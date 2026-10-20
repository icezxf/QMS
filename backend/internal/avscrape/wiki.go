package actordb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"qmediasync/internal/dictdb"
	"qmediasync/internal/helpers"
	"qmediasync/internal/models"

	"gopkg.in/yaml.v3"
)

// ============ 默认脚本 ============

const DefaultScrapeScript = `name: 智能聚合刮削
steps:
  - action: emby.getPerson
    params: { id: "{{personId}}" }
    save: person

  - action: metadata.smartFetch
    params: { name: "{{person.name}}" }
    save: meta

  - action: emby.updatePerson
    params:
      id: "{{personId}}"
      name: "{{meta.Name}}"
      character: "Actor"
      birthday: "{{meta.BirthDate}}"
      place_of_birth: "{{meta.PlaceOfBirth}}"
      production_locations: "{{meta.ProductionLocations}}"
      premiere_date: "{{meta.PremiereDate}}"
      production_year: "{{meta.ProductionYear}}"
      known_for_department: "{{meta.KnownForDepartment}}"
      
      provider_ids: "{{meta.ProviderIds}}"
      
      taglines: "{{meta.Taglines}}"
      biography: "{{meta.Bio}}"
      tags: []

  - action: emby.uploadAvatar
    params:
      id: "{{personId}}"
      imageUrl: "{{meta.ImageURL}}"
`

// ============ 文件读写 ============

func scriptFilePath() string {
	return filepath.Join("/app/config", "emby_scrape_script.yaml")
}

func LoadScript() (string, bool, error) {
	data, err := os.ReadFile(scriptFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultScrapeScript, true, nil
		}
		return DefaultScrapeScript, true, err
	}
	s := string(data)
	if strings.TrimSpace(s) == "" {
		return DefaultScrapeScript, true, nil
	}
	return s, false, nil
}

func SaveScript(text string) error {
	return os.WriteFile(scriptFilePath(), []byte(text), 0644)
}

func ResetScript() error {
	err := os.Remove(scriptFilePath())
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ============ 脚本结构 ============

type Script struct {
	Name  string `yaml:"name"`
	Steps []Step `yaml:"steps"`
}

type Step struct {
	Action string         `yaml:"action"`
	Params map[string]any `yaml:"params"`
	Save   string         `yaml:"save"`
}

type StepResult struct {
	Index    int    `json:"index"`
	Action   string `json:"action"`
	OK       bool   `json:"ok"`
	Message  string `json:"message"`
	Duration string `json:"duration"`
}

// ============ API 客户端 ============

const DefaultStashDBEndpoint = "https://stashdb.org/graphql"
const DefaultJavStashEndpoint = "https://javstash.org/graphql"
const DefaultTPDBEndpoint = "https://api.theporndb.net/performers"

type StashBoxClient struct {
	Endpoint string
	APIKey   string
	http     *http.Client
}

func NewStashBoxClient(endpoint, apiKey string) *StashBoxClient {
	return &StashBoxClient{Endpoint: endpoint, APIKey: apiKey, http: &http.Client{Timeout: 30 * time.Second}}
}

type StashBoxPerformer struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Aliases      []string `json:"aliases"`
	BirthDate    string   `json:"birth_date"`
	Country      string   `json:"country"`
	Ethnicity    string   `json:"ethnicity"`
	Height       int      `json:"height"`
	Weight       int      `json:"weight"`
	Gender       string   `json:"gender"`
	Bio          string   `json:"bio"`
	CareerStart  int      `json:"career_start_year"`
	Measurements struct {
		CupSize string `json:"cup_size"`
		Waist   int    `json:"waist"`
		Hip     int    `json:"hip"`
	} `json:"measurements"`
	Images []struct {
		URL string `json:"url"`
	} `json:"images"`
	Urls []struct {
		URL string `json:"url"`
	} `json:"urls"`
}

const stashGraphQLFields = `
	id name aliases birth_date country ethnicity height gender career_start_year
	measurements { cup_size waist hip }
	images { url }
	urls { url }
`

func (c *StashBoxClient) SearchPerformer(ctx context.Context, term string) (*StashBoxPerformer, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("API Key 未配置")
	}

	isUUID := len(term) == 36 && strings.Count(term, "-") == 4
	var q string
	var vars map[string]any

	if isUUID {
		q = fmt.Sprintf(`query Find($id: ID!) { findPerformer(id: $id) { %s } }`, stashGraphQLFields)
		vars = map[string]any{"id": term}
	} else {
		q = fmt.Sprintf(`query Search($term: String!) { searchPerformer(term: $term) { %s } }`, stashGraphQLFields)
		vars = map[string]any{"term": term}
	}

	body, _ := json.Marshal(map[string]any{"query": q, "variables": vars})
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(reqCtx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ApiKey", c.APIKey)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if isUUID {
		var envelope struct {
			Data struct {
				FindPerformer *StashBoxPerformer `json:"findPerformer"`
			} `json:"data"`
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		json.NewDecoder(resp.Body).Decode(&envelope)
		if len(envelope.Errors) > 0 {
			return nil, fmt.Errorf("接口报错: %s", envelope.Errors[0].Message)
		}
		if envelope.Data.FindPerformer == nil {
			return nil, fmt.Errorf("未找到该ID的演员")
		}
		return envelope.Data.FindPerformer, nil
	}

	var envelope struct {
		Data struct {
			SearchPerformer []StashBoxPerformer `json:"searchPerformer"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	json.NewDecoder(resp.Body).Decode(&envelope)
	if len(envelope.Errors) > 0 {
		return nil, fmt.Errorf("接口报错: %s", envelope.Errors[0].Message)
	}
	if len(envelope.Data.SearchPerformer) == 0 {
		return nil, fmt.Errorf("未找到演员")
	}

	// ★ 严格校验：遍历全部结果，只有名字或别名严格包含搜索词才放行
	sTerm := strings.ToLower(strings.TrimSpace(term))
	for _, p := range envelope.Data.SearchPerformer {
		tName := strings.ToLower(p.Name)
		matched := false

		if strings.Contains(tName, sTerm) || strings.Contains(sTerm, tName) {
			matched = true
		} else {
			for _, alias := range p.Aliases {
				aName := strings.ToLower(alias)
				if strings.Contains(aName, sTerm) || strings.Contains(sTerm, aName) {
					matched = true
					break
				}
			}
		}

		if matched {
			return &p, nil
		}
	}

	return nil, fmt.Errorf("未找到匹配的演员 (JavStash 返回了无关数据)")
}

func (c *StashBoxClient) SearchTPDB(ctx context.Context, term string) (*StashBoxPerformer, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("TPDB API Key 未配置")
	}

	isUUID := len(term) == 36 && strings.Count(term, "-") == 4
	bestID := ""

	if isUUID {
		bestID = term
	} else {
		searchURL := fmt.Sprintf("%s?q=%s", c.Endpoint, url.QueryEscape(term))
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("User-Agent", "Mozilla/5.0")
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		var listResp struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		json.NewDecoder(resp.Body).Decode(&listResp)
		if len(listResp.Data) == 0 {
			return nil, fmt.Errorf("TPDB 未找到演员")
		}
		bestID = listResp.Data[0].ID
	}

	detailURL := fmt.Sprintf("%s/%s", c.Endpoint, bestID)
	req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, detailURL, nil)
	req2.Header.Set("Authorization", "Bearer "+c.APIKey)
	req2.Header.Set("User-Agent", "Mozilla/5.0")
	resp2, err := c.http.Do(req2)
	if err != nil {
		return nil, err
	}
	defer resp2.Body.Close()

	var detailResp struct {
		Data struct {
			ID      string   `json:"id"`
			Name    string   `json:"name"`
			Bio     string   `json:"bio"`
			Image   string   `json:"image"`
			Aliases []string `json:"aliases"`
			Extras  struct {
				Birthday       string `json:"birthday"`
				BirthplaceCode string `json:"birthplace_code"`
				Height         string `json:"height"`
				Weight         string `json:"weight"`
				CupSize        string `json:"cupsize"`
				Waist          string `json:"waist"`
				Hips           string `json:"hips"`
				CareerStart    int    `json:"career_start_year"`
			} `json:"extras"`
			Links  map[string]string `json:"links"`
			Images []struct {
				URL string `json:"url"`
			} `json:"images"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&detailResp); err != nil {
		return nil, err
	}

	d := detailResp.Data
	p := &StashBoxPerformer{
		ID:          d.ID,
		Name:        d.Name,
		Bio:         d.Bio,
		BirthDate:   d.Extras.Birthday,
		Country:     d.Extras.BirthplaceCode,
		CareerStart: d.Extras.CareerStart,
		Aliases:     d.Aliases,
	}

	p.Height = parseHeight(d.Extras.Height)
	p.Weight = parseWeight(d.Extras.Weight)

	p.Measurements.CupSize = d.Extras.CupSize
	p.Measurements.Waist, _ = strconv.Atoi(d.Extras.Waist)
	p.Measurements.Hip, _ = strconv.Atoi(d.Extras.Hips)

	if d.Image != "" {
		p.Images = append(p.Images, struct {
			URL string `json:"url"`
		}{URL: d.Image})
	}
	for _, img := range d.Images {
		p.Images = append(p.Images, struct {
			URL string `json:"url"`
		}{URL: img.URL})
	}

	if d.Links != nil {
		for key, link := range d.Links {
			if key == "IMDb" || key == "TMDB" || key == "StashDB" {
				p.Urls = append(p.Urls, struct {
					URL string `json:"url"`
				}{URL: link})
			}
		}
	}

	p.Urls = append(p.Urls, struct {
		URL string `json:"url"`
	}{URL: fmt.Sprintf("https://theporndb.net/performers/%s", p.ID)})

	// ★ 严格校验：TPDB 名字校验
	if !isUUID {
		sTerm := strings.ToLower(strings.TrimSpace(term))
		tName := strings.ToLower(p.Name)
		matched := false

		if strings.Contains(tName, sTerm) || strings.Contains(sTerm, tName) {
			matched = true
		} else {
			for _, alias := range p.Aliases {
				aName := strings.ToLower(alias)
				if strings.Contains(aName, sTerm) || strings.Contains(sTerm, aName) {
					matched = true
					break
				}
			}
		}

		if !matched {
			return nil, fmt.Errorf("未找到匹配的演员 (TPDB 返回了无关数据: %s)", p.Name)
		}
	}

	return p, nil
}

func parseHeight(s string) int {
	s = strings.ToLower(s)
	if strings.Contains(s, "cm") {
		n, _ := strconv.Atoi(strings.TrimSpace(strings.ReplaceAll(s, "cm", "")))
		return n
	}
	if strings.Contains(s, "'") {
		parts := strings.Split(s, "'")
		feet, _ := strconv.Atoi(parts[0])
		inches := 0
		if len(parts) > 1 {
			inches, _ = strconv.Atoi(strings.Trim(parts[1], "\""))
		}
		return int(math.Round(float64(feet*12+inches) * 2.54))
	}
	return 0
}

func parseWeight(s string) int {
	s = strings.ToLower(s)
	if strings.Contains(s, "kg") {
		n, _ := strconv.Atoi(strings.TrimSpace(strings.ReplaceAll(s, "kg", "")))
		return n
	}
	if strings.Contains(s, "lb") {
		n, _ := strconv.Atoi(strings.TrimSpace(strings.ReplaceAll(s, "lbs", "")))
		return int(math.Round(float64(n) * 0.453592))
	}
	return 0
}

// ============ 维基百科 ============

func fetchWikiData(ctx context.Context, lang, title, targetLang string, email string) (string, string, string) {
	prop := "extracts"
	if targetLang != "" {
		prop = "langlinks|extracts"
	}

	apiURL := fmt.Sprintf(
		"https://%s.wikipedia.org/w/api.php?action=query&titles=%s&prop=%s&exintro=1&explaintext=1&format=json&redirects=1&formatversion=2",
		lang, url.QueryEscape(title), prop,
	)
	if targetLang != "" {
		apiURL += "&lllang=" + targetLang
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	uaEmail := email
	if uaEmail == "" {
		uaEmail = "icezxf555@gmail.com"
	}
	req.Header.Set("User-Agent", fmt.Sprintf("QMS-Actor-Scraper/1.0 (https://github.com/icezxf/QMS; %s)", uaEmail))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", ""
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	langTitle, actualTitle, bio := "", "", ""
	if query, ok := result["query"].(map[string]any); ok {
		if pages, ok := query["pages"].([]any); ok && len(pages) > 0 {
			p := pages[0].(map[string]any)
			if t, ok := p["title"].(string); ok {
				actualTitle = t
			}
			if b, ok := p["extract"].(string); ok {
				bio = b
			}
			if lls, ok := p["langlinks"].([]any); ok && len(lls) > 0 {
				if ll, ok := lls[0].(map[string]any); ok {
					if t, ok := ll["title"].(string); ok {
						re := regexp.MustCompile(`\s*\(.*?\)`)
						langTitle = re.ReplaceAllString(t, "")
						langTitle = strings.TrimSpace(langTitle)
					}
				}
			}
		}
	}
	return langTitle, actualTitle, bio
}

// ============ Ouxiang 数据源 ============

type OuxiangData struct {
	JpName string
	ZhName string
	Bio    string
	Weight int
	Url    string
}

var ouxiangHTTP = &http.Client{Timeout: 15 * time.Second}

var (
	ouxiangIdolRe   = regexp.MustCompile(`href="(/idol/[^"]+)"`)
	ouxiangH1Re     = regexp.MustCompile(`<h1>([^<]+)</h1>`)
	ouxiangIntroRe  = regexp.MustCompile(`(?s)<section id="intro"><p>(.*?)</p>`)
	ouxiangCnNameRe = regexp.MustCompile(`(?s)<tr[^>]*>\s*<td[^>]*>\s*中文名\s*</td>\s*<td[^>]*>\s*([^<]+?)\s*</td>`)
	htmlTagRe       = regexp.MustCompile(`<[^>]+>`)
)

func fetchOuxiangData(ctx context.Context, keyword string) (*OuxiangData, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, fmt.Errorf("空关键词")
	}

	normKeyword := normalizeStr(keyword)

	searchURL := "https://www.ouxiangdanganku.com/search/" + url.QueryEscape(keyword)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	resp, err := ouxiangHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

	matches := ouxiangIdolRe.FindAllStringSubmatch(string(body), 5)
	if len(matches) == 0 {
		return nil, fmt.Errorf("未搜到结果")
	}

	for _, m := range matches {
		detailPath := m[1]
		time.Sleep(150 * time.Millisecond)

		detailURL := "https://www.ouxiangdanganku.com" + detailPath
		req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, detailURL, nil)
		req2.Header.Set("User-Agent", "Mozilla/5.0")
		resp2, err := ouxiangHTTP.Do(req2)
		if err != nil {
			continue
		}
		body2, _ := io.ReadAll(io.LimitReader(resp2.Body, 4<<20))
		resp2.Body.Close()

		htmlStr := string(body2)

		h1 := ""
		if hm := ouxiangH1Re.FindStringSubmatch(htmlStr); len(hm) >= 2 {
			h1 = strings.TrimSpace(hm[1])
		}
		if h1 == "" {
			continue
		}

		// === 暴力碎块提取法 ===
		cleanHtml := strings.ReplaceAll(htmlStr, "<br>", ",")
		cleanHtml = strings.ReplaceAll(cleanHtml, "<br/>", ",")
		cleanHtml = strings.ReplaceAll(cleanHtml, "<br />", ",")
		cleanHtml = strings.ReplaceAll(cleanHtml, "</td>", ",")
		cleanHtml = htmlTagRe.ReplaceAllString(cleanHtml, " ")

		candidates := []string{h1}
		parts := regexp.MustCompile(`[，,\n\r]`).Split(cleanHtml, -1)
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if len(p) >= 2 && len(p) <= 20 &&
				!strings.ContainsAny(p, "0123456789年月日") &&
				!strings.Contains(p, "：") && !strings.Contains(p, ":") {
				candidates = append(candidates, p)
			}
		}

		// === 严格完全匹配 ===
		matched := false
		for _, c := range candidates {
			if normalizeStr(c) == normKeyword {
				matched = true
				break
			}
		}

		if !matched {
			continue
		}

		data := &OuxiangData{
			JpName: h1,
			Url:    detailURL,
		}

		if im := ouxiangIntroRe.FindStringSubmatch(htmlStr); len(im) >= 2 {
			intro := htmlTagRe.ReplaceAllString(im[1], "")
			data.Bio = strings.TrimSpace(intro)
		}

		// === 优先抓页面「中文名」字段：<tr><td>中文名</td><td>小凑四叶</td> ===
		if m := ouxiangCnNameRe.FindStringSubmatch(htmlStr); len(m) >= 2 {
			candidate := strings.TrimSpace(m[1])
			candidate = htmlTagRe.ReplaceAllString(candidate, "")
			candidate = strings.TrimSpace(candidate)
			if isChineseName(candidate) {
				data.ZhName = candidate
			}
		}

		// === 中文名字段没取到，回退候选池找纯中文 ===
		if data.ZhName == "" {
			for _, c := range candidates {
				if isChineseName(c) && !strings.ContainsAny(c, "ぁあぃいぅうぇえぉおかがきぎくぐけげこごさざしじすずせぜそぞただちぢっつづてでとどなにぬねのはばぱひびぴふぶぷへべぺほぼぽまみむめもゃやゅゆょよらりるれろゎわゐゑをんァアィイゥウェエォオカガキギクグケゲコゴサザシジスズセゼソゾタダチヂッツヅテデトドナニヌネノハバパヒビピフブプヘベペホボポマミムメモャヤュユョヨラリルレロヮワヰヱヲンヴヵヶ") {
					data.ZhName = c
					break
				}
			}
		}

		// === 最终兜底 ===
		if data.ZhName == "" {
			data.ZhName = h1
		}

		cleanHtmlForWeight := htmlTagRe.ReplaceAllString(htmlStr, " ")
		weightRe := regexp.MustCompile(`体重[^\d]*(\d+)`)
		if wm := weightRe.FindStringSubmatch(cleanHtmlForWeight); len(wm) >= 2 {
			w, _ := strconv.Atoi(wm[1])
			data.Weight = w
		}

		return data, nil
	}

	return nil, fmt.Errorf("未找到完全匹配的详情页")
}

func normalizeStr(s string) string {
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "　", "")
	return strings.ToLower(s)
}

func isChineseName(s string) bool {
	hasChinese := false
	for _, r := range s {
		if (r >= 0x3040 && r <= 0x309F) || (r >= 0x30A0 && r <= 0x30FF) {
			return false
		}
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			return false
		}
		if r >= 0x4E00 && r <= 0x9FFF {
			hasChinese = true
		}
	}
	return hasChinese
}

// ============ 本地字典 ============

func getJpNameFromDict(chineseName string) string {
	if dictdb.DictDB == nil || chineseName == "" {
		return ""
	}
	var mapping models.ActorMapping
	err := dictdb.DictDB.Where("zh_name = ?", chineseName).First(&mapping).Error
	if err == nil && mapping.JpName != "" {
		return mapping.JpName
	}

	escaped := strings.ReplaceAll(chineseName, "%", "\\%")
	escaped = strings.ReplaceAll(escaped, "_", "\\_")
	kwPattern := fmt.Sprintf("%%,%s,%%", escaped)
	err = dictdb.DictDB.Where("keyword LIKE ? ESCAPE '\\'", kwPattern).First(&mapping).Error
	if err == nil && mapping.JpName != "" {
		return mapping.JpName
	}
	return ""
}

// ============ DeepL 翻译 ============

func translateWithDeepL(ctx context.Context, text, apiKey string) (string, error) {
	endpoint := "https://api.deepl.com/v2/translate"
	if strings.HasSuffix(apiKey, ":fx") {
		endpoint = "https://api-free.deepl.com/v2/translate"
	}

	form := url.Values{}
	form.Set("text", text)
	form.Set("source_lang", "EN")
	form.Set("target_lang", "ZH")

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "DeepL-Auth-Key "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return text, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return text, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}

	var out struct {
		Translations []struct {
			Text string `json:"text"`
		} `json:"translations"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return text, err
	}
	if len(out.Translations) > 0 {
		return out.Translations[0].Text, nil
	}
	return text, fmt.Errorf("empty translation")
}

// ============ 执行器 ============

type ScriptExecutor struct {
	Config    *EmbyActorConfig // ★ 带上前端配置（里面有 DeepL Key）
	Emby      *EmbyClient
	Stash     *StashBoxClient
	JavStash  *StashBoxClient
	Tpdb      *StashBoxClient
	WikiEmail string
}

func NewScriptExecutor(config *EmbyActorConfig, emby *EmbyClient, stash *StashBoxClient, javStash *StashBoxClient, tpdb *StashBoxClient) *ScriptExecutor {
	return &ScriptExecutor{
		Config: config, Emby: emby, Stash: stash, JavStash: javStash, Tpdb: tpdb,
	}
}

func (e *ScriptExecutor) Run(ctx context.Context, scriptText string, input map[string]any) (map[string]any, []StepResult, error) {
	var s Script
	if err := yaml.Unmarshal([]byte(scriptText), &s); err != nil {
		return nil, nil, err
	}
	vars := map[string]any{}
	for k, v := range input {
		vars[k] = v
	}
	var results []StepResult
	for i, step := range s.Steps {
		params := map[string]any{}
		for k, v := range step.Params {
			params[k] = resolveTemplate(v, vars)
		}
		fn, ok := actionRegistry[step.Action]
		if !ok {
			return vars, results, fmt.Errorf("未知 action: %s", step.Action)
		}
		start := time.Now()
		out, err := fn(ctx, e, params, vars)
		dur := time.Since(start).Round(time.Millisecond)
		if err != nil {
			results = append(results, StepResult{Index: i, Action: step.Action, OK: false, Message: err.Error(), Duration: dur.String()})
			return vars, results, fmt.Errorf("步骤 %d (%s) 失败：%w", i+1, step.Action, err)
		}
		if step.Save != "" {
			vars[step.Save] = out
		}
		results = append(results, StepResult{Index: i, Action: step.Action, OK: true, Message: summarize(out), Duration: dur.String()})
	}
	return vars, results, nil
}

var tmplRe = regexp.MustCompile(`\{\{\s*([^}]+?)\s*\}\}`)

func resolveTemplate(v any, vars map[string]any) any {
	if s, ok := v.(string); ok {
		if m := tmplRe.FindStringSubmatch(s); m != nil && m[0] == s {
			if val, ok := getPath(vars, strings.TrimSpace(m[1])); ok {
				return val
			}
			return nil
		}
		return tmplRe.ReplaceAllStringFunc(s, func(mm string) string {
			key := strings.TrimSpace(mm[2 : len(mm)-2])
			val, ok := getPath(vars, key)
			if !ok {
				return ""
			}
			return fmt.Sprint(val)
		})
	}
	if arr, ok := v.([]any); ok {
		res := make([]any, len(arr))
		for i, item := range arr {
			res[i] = resolveTemplate(item, vars)
		}
		return res
	}
	if m, ok := v.(map[string]any); ok {
		res := make(map[string]any)
		for k, item := range m {
			res[k] = resolveTemplate(item, vars)
		}
		return res
	}
	return v
}

func getPath(obj any, path string) (any, bool) {
	parts := strings.Split(path, ".")
	cur := obj
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func summarize(v any) string {
	if v == nil {
		return "ok"
	}
	b, _ := json.Marshal(v)
	s := string(b)
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

func detectLanguage(text string) string {
	if matched, _ := regexp.MatchString(`^[a-zA-Z\s\-\.0-9]+$`, text); matched {
		return "en"
	}
	if matched, _ := regexp.MatchString(`[\p{Han}]`, text); matched {
		return "zh"
	}
	if matched, _ := regexp.MatchString(`[\p{Hiragana}\p{Katakana}]`, text); matched {
		return "ja"
	}
	return "unknown"
}

func extractProviderIds(urls []struct {
	URL string `json:"url"`
}) map[string]any {
	ids := map[string]any{}
	for _, u := range urls {
		urlStr := u.URL
		if strings.Contains(urlStr, "themoviedb.org/person/") {
			parts := strings.Split(urlStr, "/person/")
			if len(parts) > 1 {
				ids["Tmdb"] = strings.Split(parts[1], "-")[0]
			}
		}
		if strings.Contains(urlStr, "imdb.com/name/") {
			parts := strings.Split(urlStr, "/name/")
			if len(parts) > 1 {
				ids["Imdb"] = strings.Trim(parts[1], "/")
			}
		}
		if strings.Contains(urlStr, "wikidata.org/wiki/") {
			parts := strings.Split(urlStr, "/")
			ids["Wikidata"] = parts[len(parts)-1]
		}
		if strings.Contains(urlStr, "wikipedia.org/wiki/") {
			parts := strings.Split(urlStr, "//")
			if len(parts) > 1 {
				ids["Wikipedia"] = parts[1]
			}
		}
		if strings.Contains(urlStr, "stashdb.org/performers/") {
			parts := strings.Split(urlStr, "/")
			ids["StashDB"] = parts[len(parts)-1]
		}
		if strings.Contains(urlStr, "javstash.org/performers/") {
			parts := strings.Split(urlStr, "/")
			ids["JavStash"] = parts[len(parts)-1]
		}
		if strings.Contains(urlStr, "theporndb.net/performers/") {
			parts := strings.Split(urlStr, "/")
			ids["Porndb"] = parts[len(parts)-1]
		}
	}
	return ids
}

type actionFn func(ctx context.Context, exec *ScriptExecutor, params, vars map[string]any) (any, error)

var actionRegistry = map[string]actionFn{
	"emby.getPerson":      actionEmbyGetPerson,
	"emby.updatePerson":   actionEmbyUpdatePerson,
	"emby.uploadAvatar":   actionEmbyUploadAvatar,
	"metadata.smartFetch": actionSmartFetch,
	"log.info":            actionLogInfo,
}

func actionSmartFetch(ctx context.Context, exec *ScriptExecutor, params, vars map[string]any) (any, error) {
	originalName := toString(params["name"])
	if originalName == "" {
		return nil, fmt.Errorf("缺少 name")
	}

	lang := detectLanguage(originalName)
	isUUID := len(originalName) == 36 && strings.Count(originalName, "-") == 4

	result := map[string]any{
		"Name": originalName, "BirthDate": "", "PlaceOfBirth": "",
		"ImageURL": "", "ProviderIds": map[string]any{}, "Bio": "", "ExternalLinks": "",
		"CupSize": "", "Waist": 0, "Hip": 0, "Height": 0, "Weight": 0, "AliasesString": "",
		"Images": []string{},
		"PremiereDate": "", "ProductionYear": 0, "KnownForDepartment": "Acting", "ProductionLocations": []string{},
	}

	countryMap := map[string]string{
		"JP": "日本", "US": "美国", "KR": "韩国", "CN": "中国",
		"TW": "中国台湾", "HK": "中国香港", "CA": "加拿大", "UK": "英国",
		"GB": "英国", "AU": "澳大利亚", "FR": "法国", "DE": "德国",
		"RU": "俄罗斯", "IT": "意大利", "ES": "西班牙", "BR": "巴西",
	}

	var tpdbSearchName string
	var js *StashBoxPerformer

	// ===== 0. Ouxiang 预查 =====
	var ouxData *OuxiangData
	if !isUUID { // 只要是正常名字，都去碰碰运气
		if data, err := fetchOuxiangData(ctx, originalName); err == nil {
			ouxData = data
			helpers.AppLogger.Infof("[Ouxiang] %s 命中: 中文名=%s, 日文名=%s", originalName, data.ZhName, data.JpName)

			if data.Bio != "" {
				result["Bio"] = data.Bio
			}
			if data.Weight > 0 {
				result["Weight"] = data.Weight
			}

			ids := result["ProviderIds"].(map[string]any)
			ids["Ouxiang"] = data.Url
		}
	}

	// ===== 1. 先尝试查 TPDB =====
	if exec.Tpdb != nil && exec.Tpdb.APIKey != "" {
		tp, err := exec.Tpdb.SearchTPDB(ctx, originalName)
		if err == nil {
			// 只有当输入是 UUID 的时候，才允许 TPDB 的名字作为回退。正常名字绝不覆盖。
			if isUUID && toString(result["Name"]) == originalName {
				result["Name"] = tp.Name
			}
			result["BirthDate"] = tp.BirthDate
			result["Height"] = tp.Height
			if tp.Weight > 0 {
				result["Weight"] = tp.Weight
			}
			result["CupSize"] = tp.Measurements.CupSize
			result["Waist"] = tp.Measurements.Waist
			result["Hip"] = tp.Measurements.Hip

			if toString(result["Bio"]) == "" {
				result["Bio"] = tp.Bio
			}

			if tp.CareerStart > 0 {
				result["PremiereDate"] = fmt.Sprintf("%d-01-01", tp.CareerStart)
				result["ProductionYear"] = tp.CareerStart
			}

			if cName, ok := countryMap[tp.Country]; ok {
				result["PlaceOfBirth"] = cName
				result["ProductionLocations"] = []string{cName}
			} else {
				result["PlaceOfBirth"] = tp.Country
				result["ProductionLocations"] = []string{tp.Country}
			}

			tpIds := extractProviderIds(tp.Urls)
			currentIds := result["ProviderIds"].(map[string]any)
			for k, v := range tpIds {
				currentIds[k] = v
			}

			tpImgs := []string{}
			for _, img := range tp.Images {
				tpImgs = append(tpImgs, img.URL)
			}
			if len(tpImgs) > 0 {
				result["ImageURL"] = tpImgs[0]
			}
			result["Images"] = tpImgs
		}
	}

	// ===== 2. 核心：查维基 + JavStash =====
	searchCandidates := []string{originalName}

	// 把 ouxiang 查到的日文名加入搜索候选（尽量拿日文名去搜）
	if ouxData != nil && ouxData.JpName != "" {
		searchCandidates = append([]string{ouxData.JpName}, searchCandidates...)
	}

	if lang == "zh" {
		jpNameFromDict := getJpNameFromDict(originalName)
		if jpNameFromDict != "" && jpNameFromDict != originalName {
			helpers.AppLogger.Infof("[演员字典] %s → %s", originalName, jpNameFromDict)
			searchCandidates = append([]string{jpNameFromDict}, searchCandidates...)

			zhTitleFromJa, _, jaBio := fetchWikiData(ctx, "ja", jpNameFromDict, "zh", exec.WikiEmail)
			if jaBio != "" && toString(result["Bio"]) == "" {
				result["Bio"] = jaBio
			}
			if zhTitleFromJa != "" {
				_, _, zhBio := fetchWikiData(ctx, "zh", zhTitleFromJa, "", exec.WikiEmail)
				if zhBio != "" && toString(result["Bio"]) == "" {
					result["Bio"] = zhBio
				}
				if zhTitleFromJa != originalName && zhTitleFromJa != jpNameFromDict {
					searchCandidates = append(searchCandidates, zhTitleFromJa)
				}
			}
		}

		if jpNameFromDict == "" && (ouxData == nil || ouxData.JpName == "") {
			jaName, actualTitle, zhBio := fetchWikiData(ctx, "zh", originalName, "ja", exec.WikiEmail)
			if zhBio != "" && toString(result["Bio"]) == "" {
				result["Bio"] = zhBio
			}
			if actualTitle != "" && actualTitle != originalName {
				searchCandidates = append(searchCandidates, actualTitle)
			}
			if jaName != "" && jaName != originalName && jaName != actualTitle {
				searchCandidates = append(searchCandidates, jaName)
			}
		}
	} else if lang == "ja" {
		zhTitleFromJa, _, jaBio := fetchWikiData(ctx, "ja", originalName, "zh", exec.WikiEmail)
		if jaBio != "" && toString(result["Bio"]) == "" {
			result["Bio"] = jaBio
		}
		if zhTitleFromJa != "" {
			_, _, zhBio := fetchWikiData(ctx, "zh", zhTitleFromJa, "", exec.WikiEmail)
			if zhBio != "" && toString(result["Bio"]) == "" {
				result["Bio"] = zhBio
			}
			if zhTitleFromJa != originalName {
				searchCandidates = append(searchCandidates, zhTitleFromJa)
			}
		}
	}

	if exec.JavStash != nil && exec.JavStash.APIKey != "" {
		for _, candidate := range searchCandidates {
			var jsErr error
			js, jsErr = exec.JavStash.SearchPerformer(ctx, candidate)
			if jsErr == nil && js != nil {
				break
			}
		}
	}

	if js != nil {
		// 只有 UUID 才允许 JavStash 改名，正常输入（中/日/英）一律保留原名。
		if isUUID && toString(result["Name"]) == originalName {
			result["Name"] = js.Name
		}

		result["BirthDate"] = js.BirthDate
		result["PlaceOfBirth"] = "日本"
		result["ProductionLocations"] = []string{"日本"}
		result["AliasesString"] = strings.Join(js.Aliases, ", ")

		if js.Height > 0 {
			result["Height"] = js.Height
		}
		if js.CareerStart > 0 {
			result["PremiereDate"] = fmt.Sprintf("%d-01-01", js.CareerStart)
			result["ProductionYear"] = js.CareerStart
		}

		mergedIds := result["ProviderIds"].(map[string]any)
		jsIds := extractProviderIds(js.Urls)
		if js.ID != "" {
			jsIds["JavStash"] = js.ID
		}
		for k, v := range jsIds {
			mergedIds[k] = v
		}

		jsImgs := []string{}
		for _, img := range js.Images {
			jsImgs = append(jsImgs, img.URL)
		}
		if len(jsImgs) > 0 {
			result["ImageURL"] = jsImgs[0]
		}
		result["Images"] = jsImgs

		if toString(result["Bio"]) == "" {
			var jaWikiTitle string
			for _, u := range js.Urls {
				if strings.Contains(u.URL, "ja.wikipedia.org/wiki/") {
					jaWikiTitle = u.URL[strings.LastIndex(u.URL, "/")+1:]
				}
			}
			if jaWikiTitle != "" {
				zhTitle, _, jaBio := fetchWikiData(ctx, "ja", jaWikiTitle, "zh", exec.WikiEmail)
				if jaBio != "" {
					result["Bio"] = jaBio
				}
				if zhTitle != "" {
					_, _, zhBio := fetchWikiData(ctx, "zh", zhTitle, "", exec.WikiEmail)
					if zhBio != "" {
						result["Bio"] = zhBio
					}
				}
			}
		}

		tpdbSearchName = js.Name
		for _, alias := range js.Aliases {
			if detectLanguage(alias) == "en" {
				tpdbSearchName = alias
				break
			}
		}
	}

	// ===== 3. TPDB 补三围/体重 =====
	needMeasurements := result["Height"] == 0 || result["Waist"] == 0 || result["Hip"] == 0 || result["CupSize"] == ""
	if needMeasurements && tpdbSearchName != "" && exec.Tpdb != nil && exec.Tpdb.APIKey != "" {
		if tp, err := exec.Tpdb.SearchTPDB(ctx, tpdbSearchName); err == nil {
			if tp.Height > 0 {
				result["Height"] = tp.Height
			}
			if tp.Weight > 0 {
				result["Weight"] = tp.Weight
			}
			if tp.Measurements.CupSize != "" {
				result["CupSize"] = tp.Measurements.CupSize
			}
			if tp.Measurements.Waist > 0 {
				result["Waist"] = tp.Measurements.Waist
			}
			if tp.Measurements.Hip > 0 {
				result["Hip"] = tp.Measurements.Hip
			}

			if toString(result["Bio"]) == "" {
				result["Bio"] = tp.Bio
			}

			merged := result["ProviderIds"].(map[string]any)
			for k, v := range extractProviderIds(tp.Urls) {
				if merged[k] == nil || merged[k] == "" {
					merged[k] = v
				}
			}
			tpImgs := result["Images"].([]string)
			for _, img := range tp.Images {
				isNew := true
				for _, existing := range tpImgs {
					if existing == img.URL {
						isNew = false
						break
					}
				}
				if isNew {
					tpImgs = append(tpImgs, img.URL)
				}
			}
			result["Images"] = tpImgs
		}
	}

	// ===== 4. 体重最终兜底 =====
	if result["Weight"] == 0 && ouxData != nil && ouxData.Weight > 0 {
		result["Weight"] = ouxData.Weight
	}

	// ===== 5. 英文简介翻译（强制使用 DeepL） =====
	bioText := strings.TrimSpace(toString(result["Bio"]))
	isEnglish := false
	if bioText != "" {
		hasJa := false
		hasEn := false
		for _, r := range bioText {
			if (r >= 0x3040 && r <= 0x309F) || (r >= 0x30A0 && r <= 0x30FF) {
				hasJa = true
				break
			}
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				hasEn = true
			}
		}
		if !hasJa && hasEn {
			isEnglish = true
		}
	}

	// 直接读取前端传过来的配置
	if isEnglish && exec.Config != nil && exec.Config.DeepLApiKey != "" {
		helpers.AppLogger.Infof("[演员翻译] 准备用 DeepL 翻译英文简介: %s", originalName)
		translated, err := translateWithDeepL(ctx, bioText, exec.Config.DeepLApiKey)
		if err == nil && translated != "" {
			result["Bio"] = translated
			helpers.AppLogger.Infof("[演员翻译] 翻译成功")
		} else {
			helpers.AppLogger.Warnf("[演员翻译] 翻译失败: %v", err)
		}
	}

	// 动态生成外部链接
	var links []string
	ids := result["ProviderIds"].(map[string]any)
	if val := ids["Wikipedia"]; val != nil && val != "" {
		links = append(links, fmt.Sprintf("Wikipedia: https://%s", val))
	}
	if val := ids["StashDB"]; val != nil && val != "" {
		links = append(links, fmt.Sprintf("StashDB: https://stashdb.org/performers/%s", val))
	}
	if val := ids["JavStash"]; val != nil && val != "" {
		links = append(links, fmt.Sprintf("JavStash: https://javstash.org/performers/%s", val))
	}
	if val := ids["Ouxiang"]; val != nil && val != "" {
		links = append(links, fmt.Sprintf("Ouxiang: %s", val))
	}
	if val := ids["Imdb"]; val != nil && val != "" {
		links = append(links, fmt.Sprintf("IMDb: https://www.imdb.com/name/%s/", val))
	}
	if val := ids["Tmdb"]; val != nil && val != "" {
		links = append(links, fmt.Sprintf("TMDB: https://www.themoviedb.org/person/%s", val))
	}
	if val := ids["Porndb"]; val != nil && val != "" {
		links = append(links, fmt.Sprintf("Porndb: https://theporndb.net/performers/%s", val))
	}

	externalLinks := ""
	if len(links) > 0 {
		externalLinks = "【外部链接】\n" + strings.Join(links, "\n")
	}

	bioText = strings.TrimSpace(toString(result["Bio"]))
	if externalLinks != "" {
		if bioText != "" {
			result["Bio"] = bioText + "\n\n" + externalLinks
		} else {
			result["Bio"] = externalLinks
		}
	} else {
		result["Bio"] = bioText
	}

	// 档案
	var taglines []string
	doc := fmt.Sprintf("【档案】身高 %dcm | 体重 %dkg | 三围 %d-%d | %s罩杯", result["Height"], result["Weight"], result["Waist"], result["Hip"], result["CupSize"])
	taglines = append(taglines, doc)

	if aliases := toString(result["AliasesString"]); aliases != "" {
		taglines = append(taglines, "【别名】："+aliases)
	}
	result["Taglines"] = taglines

	return result, nil
}

func actionLogInfo(ctx context.Context, exec *ScriptExecutor, params, vars map[string]any) (any, error) {
	return fmt.Sprint(params["msg"]), nil
}

func actionEmbyGetPerson(ctx context.Context, exec *ScriptExecutor, params, vars map[string]any) (any, error) {
	id := toString(params["id"])
	url := fmt.Sprintf("%s/api/v1/people/%s?api_key=%s", exec.Emby.BaseURL, id, exec.Emby.APIKey)
	var resp map[string]any
	if err := httpGetJSON(ctx, url, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func actionEmbyUpdatePerson(ctx context.Context, exec *ScriptExecutor, params, vars map[string]any) (any, error) {
	id := toString(params["id"])
	if id == "" {
		return nil, fmt.Errorf("缺少 id")
	}
	body := map[string]any{}
	for k, v := range params {
		if k == "id" || v == nil {
			continue
		}
		switch k {
		case "place_of_birth":
			body["placeOfBirth"] = v
		case "known_for_department":
			body["knownForDepartment"] = v
		case "provider_ids":
			body["providerIds"] = v
		case "premiere_date":
			body["premiereDate"] = v
		case "production_locations":
			body["productionLocations"] = v
		case "production_year":
			body["productionYear"] = v
		case "character":
			body["character"] = v
		default:
			body[k] = v
		}
	}
	if _, ok := body["name"]; !ok {
		return nil, fmt.Errorf("name 必填")
	}

	data, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/api/v1/people/%s?api_key=%s", exec.Emby.BaseURL, id, exec.Emby.APIKey)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("PATCH %d: %s", resp.StatusCode, string(b))
	}
	return map[string]any{"ok": true}, nil
}

func actionEmbyUploadAvatar(ctx context.Context, exec *ScriptExecutor, params, vars map[string]any) (any, error) {
	id := toString(params["id"])
	imageURL := toString(params["imageUrl"])
	if id == "" {
		return nil, fmt.Errorf("缺少 id")
	}
	if imageURL == "" {
		return map[string]any{"ok": true, "message": "无图片URL，跳过"}, nil
	}

	imgReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	imgReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	imgResp, err := http.DefaultClient.Do(imgReq)
	if err != nil {
		return nil, err
	}
	defer imgResp.Body.Close()
	if imgResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载失败：HTTP %d", imgResp.StatusCode)
	}

	ct := imgResp.Header.Get("Content-Type")
	if ct == "" {
		ct = "image/jpeg"
	}
	imgData, _ := io.ReadAll(imgResp.Body)

	url := fmt.Sprintf("%s/emby/Items/%s/Images/Primary?api_key=%s", exec.Emby.BaseURL, id, exec.Emby.APIKey)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(imgData))
	req.Header.Set("Content-Type", ct)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("上传头像 %d: %s", resp.StatusCode, string(b))
	}
	return map[string]any{"ok": true, "size": len(imgData)}, nil
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func httpGetJSON(ctx context.Context, url string, out any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
