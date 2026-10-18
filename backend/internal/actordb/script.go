package actordb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"qmediasync/internal/helpers"

	"gopkg.in/yaml.v3"
)

// ============ 默认脚本 ============

const DefaultScrapeScript = `name: 智能聚合刮削 (中/日/英 自动判断)
steps:
  - action: emby.getPerson
    params: { id: "{{personId}}" }
    save: person

  # 调用后端超级引擎，自动查 Wiki、JavStash 和 TPDB
  - action: metadata.smartFetch
    params: { name: "{{person.name}}" }
    save: meta

  - action: log.info
    params:
      msg: "抓取成功：{{meta.Name}} | 生日：{{meta.BirthDate}}"

  - action: emby.updatePerson
    params:
      id: "{{personId}}"
      name: "{{meta.Name}}"
      birthday: "{{meta.BirthDate}}"
      place_of_birth: "{{meta.PlaceOfBirth}}"
      provider_ids: "{{meta.ProviderIds}}"
      biography: "{{meta.Bio}}"
      taglines:
        - "【档案】身高 {{meta.Height}}cm | 三围 {{meta.Waist}}-{{meta.Hip}} | {{meta.CupSize}}罩杯"
      tags:
        - "别名:{{meta.AliasesString}}"

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

// ============ Stash-box 客户端 ============

const DefaultStashDBEndpoint = "https://stashdb.org/graphql"
const DefaultJavStashEndpoint = "https://javstash.org/graphql"
const DefaultTPDBEndpoint = "https://theporndb.net/graphql"

type StashBoxClient struct {
	Endpoint string
	APIKey   string
	http     *http.Client
}

func NewStashBoxClient(endpoint, apiKey string) *StashBoxClient {
	return &StashBoxClient{
		Endpoint: endpoint,
		APIKey:   apiKey,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

type StashBoxPerformer struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Aliases      []string `json:"aliases"`
	BirthDate    string   `json:"birth_date"`
	Country      string   `json:"country"`
	Ethnicity    string   `json:"ethnicity"`
	Height       int      `json:"height"`
	Gender       string   `json:"gender"`
	Bio          string   `json:"bio"`
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
	id name aliases birth_date country ethnicity height gender
	measurements { cup_size waist hip }
	images { url }
	urls { url }
`

func (c *StashBoxClient) SearchPerformer(ctx context.Context, term string) (*StashBoxPerformer, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("API Key 未配置")
	}
	q := fmt.Sprintf(`query Search($term: String!) { searchPerformer(term: $term) { %s } }`, stashGraphQLFields)
	body, _ := json.Marshal(map[string]any{"query": q, "variables": map[string]any{"term": term}})
	
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	
	req, _ := http.NewRequestWithContext(reqCtx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ApiKey", c.APIKey)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("网络请求失败: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	
	var envelope struct {
		Data struct {
			SearchPerformer []StashBoxPerformer `json:"searchPerformer"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("响应解析失败: %w", err)
	}
	if len(envelope.Errors) > 0 {
		return nil, fmt.Errorf("GraphQL 错误：%s", envelope.Errors[0].Message)
	}
	if len(envelope.Data.SearchPerformer) == 0 {
		return nil, fmt.Errorf("未找到演员：%s", term)
	}
	return &envelope.Data.SearchPerformer[0], nil
}

func (c *StashBoxClient) SearchTPDB(ctx context.Context, term string) (*StashBoxPerformer, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("TPDB API Key 未配置")
	}
	q := `query Search($term: String!) { searchPerformer(term: $term) { id name bio birth_date country height measurements { cup_size waist hip } images { url } urls { url } } }`
	body, _ := json.Marshal(map[string]any{"query": q, "variables": map[string]any{"term": term}})
	
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	
	respHttp, err := c.http.Do(req)
	if err != nil { return nil, err }
	defer respHttp.Body.Close()
	
	var envelope struct {
		Data struct {
			SearchPerformer []StashBoxPerformer `json:"searchPerformer"`
		} `json:"data"`
	}
	json.NewDecoder(respHttp.Body).Decode(&envelope)
	if len(envelope.Data.SearchPerformer) == 0 {
		return nil, fmt.Errorf("TPDB 未找到演员")
	}
	return &envelope.Data.SearchPerformer[0], nil
}

// ============ 维基百科抓取 ============

func fetchWikiData(ctx context.Context, lang, title string, email string) (string, string) {
	prop := "extracts"
	if lang == "zh" {
		prop = "langlinks|extracts"
	}
	
	apiURL := fmt.Sprintf("https://%s.wikipedia.org/w/api.php?action=query&titles=%s&prop=%s&lllang=ja&exintro=1&explaintext=1&format=json", lang, url.QueryEscape(title), prop)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	
	uaEmail := email
	if uaEmail == "" { uaEmail = "icezxf555@gmail.com" }
	req.Header.Set("User-Agent", fmt.Sprintf("QMS-Actor-Scraper/1.0 (https://github.com/icezxf/QMS; %s)", uaEmail))
	
	resp, err := http.DefaultClient.Do(req)
	if err != nil { return "", "" }
	defer resp.Body.Close()
	
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	
	jaName := ""
	bio := ""
	
	if query, ok := result["query"].(map[string]any); ok {
		if pages, ok := query["pages"].(map[string]any); ok {
			for _, page := range pages {
				p := page.(map[string]any)
				if b, ok := p["extract"].(string); ok { bio = b }
				if lls, ok := p["langlinks"].([]any); ok && len(lls) > 0 {
					if ll, ok := lls[0].(map[string]any); ok {
						if star, ok := ll["*"].(string); ok { jaName = star }
					}
				}
			}
		}
	}
	return jaName, bio
}

// ============ 执行器 ============

type ScriptExecutor struct {
	Emby      *EmbyClient
	Stash     *StashBoxClient
	JavStash  *StashBoxClient
	Tpdb      *StashBoxClient
	WikiEmail string
}

func NewScriptExecutor(emby *EmbyClient, stash *StashBoxClient, javStash *StashBoxClient, tpdb *StashBoxClient) *ScriptExecutor {
	return &ScriptExecutor{Emby: emby, Stash: stash, JavStash: javStash, Tpdb: tpdb}
}

func (e *ScriptExecutor) Run(ctx context.Context, scriptText string, input map[string]any) (map[string]any, []StepResult, error) {
	var s Script
	if err := yaml.Unmarshal([]byte(scriptText), &s); err != nil {
		return nil, nil, fmt.Errorf("脚本解析失败：%w", err)
	}

	vars := map[string]any{}
	for k, v := range input { vars[k] = v }

	var results []StepResult
	for i, step := range s.Steps {
		params := map[string]any{}
		for k, v := range step.Params { params[k] = resolveTemplate(v, vars) }

		fn, ok := actionRegistry[step.Action]
		if !ok {
			results = append(results, StepResult{Index: i, Action: step.Action, OK: false, Message: fmt.Sprintf("未知 action: %s", step.Action)})
			return vars, results, fmt.Errorf("未知 action: %s", step.Action)
		}

		start := time.Now()
		out, err := fn(ctx, e, params, vars)
		dur := time.Since(start).Round(time.Millisecond)

		if err != nil {
			results = append(results, StepResult{Index: i, Action: step.Action, OK: false, Message: err.Error(), Duration: dur.String()})
			return vars, results, fmt.Errorf("步骤 %d (%s) 失败：%w", i+1, step.Action, err)
		}

		if step.Save != "" { vars[step.Save] = out }
		results = append(results, StepResult{Index: i, Action: step.Action, OK: true, Message: summarize(out), Duration: dur.String()})
	}
	return vars, results, nil
}

// ============ 模板解析与数据转换 ============

var tmplRe = regexp.MustCompile(`\{\{\s*([^}]+?)\s*\}\}`)

func resolveTemplate(v any, vars map[string]any) any {
	if s, ok := v.(string); ok {
		if m := tmplRe.FindStringSubmatch(s); m != nil && m[0] == s {
			if val, ok := getPath(vars, strings.TrimSpace(m[1])); ok { return val }
			return nil
		}
		return tmplRe.ReplaceAllStringFunc(s, func(mm string) string {
			key := strings.TrimSpace(mm[2 : len(mm)-2])
			val, ok := getPath(vars, key)
			if !ok { return "" }
			return fmt.Sprint(val)
		})
	}
	if arr, ok := v.([]any); ok {
		res := make([]any, len(arr))
		for i, item := range arr { res[i] = resolveTemplate(item, vars) }
		return res
	}
	if m, ok := v.(map[string]any); ok {
		res := make(map[string]any)
		for k, item := range m { res[k] = resolveTemplate(item, vars) }
		return res
	}
	return v
}

func getPath(obj any, path string) (any, bool) {
	parts := strings.Split(path, ".")
	cur := obj
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok { return nil, false }
		cur, ok = m[p]
		if !ok { return nil, false }
	}
	return cur, true
}

func summarize(v any) string {
	if v == nil { return "ok" }
	b, _ := json.Marshal(v)
	s := string(b)
	if len(s) > 200 { s = s[:200] + "..." }
	return s
}

// ============ 核心智能 Action ============

type actionFn func(ctx context.Context, exec *ScriptExecutor, params, vars map[string]any) (any, error)

var actionRegistry = map[string]actionFn{
	"emby.getPerson":           actionEmbyGetPerson,
	"emby.updatePerson":        actionEmbyUpdatePerson,
	"emby.uploadAvatar":        actionEmbyUploadAvatar,
	"http.get":                 actionHTTPGet,
	"stashdb.searchPerformer":  actionStashDBSearch,
	"javstash.searchPerformer": actionJavStashSearch,
	"tpdb.searchPerformer":     actionTPDBSearch,
	"metadata.smartFetch":      actionSmartFetch,
	"log.info":                 actionLogInfo,
}

func detectLanguage(text string) string {
	if matched, _ := regexp.MatchString(`^[a-zA-Z\s\-\.]+$`, text); matched { return "en" }
	if matched, _ := regexp.MatchString(`[\p{Han}]`, text); matched { return "zh" }
	if matched, _ := regexp.MatchString(`[\p{Hiragana}\p{Katakana}]`, text); matched { return "ja" }
	return "unknown"
}

func extractProviderIds(urls []struct {
	URL string `json:"url"`
}) map[string]string {
	ids := map[string]string{}
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
		if strings.Contains(urlStr, "stashdb.org/performers/") {
			parts := strings.Split(urlStr, "/")
			ids["StashDB"] = parts[len(parts)-1]
		}
	}
	return ids
}

func actionSmartFetch(ctx context.Context, exec *ScriptExecutor, params, vars map[string]any) (any, error) {
	originalName := toString(params["name"])
	if originalName == "" { return nil, fmt.Errorf("缺少 name") }

	lang := detectLanguage(originalName)
	
	result := map[string]any{
		"Name": originalName, "BirthDate": "", "PlaceOfBirth": "",
		"ImageURL": "", "ProviderIds": map[string]string{}, "Bio": "",
		"CupSize": "", "Waist": 0, "Hip": 0, "Height": 0, "AliasesString": "",
	}

	var tpdbSearchName string

	switch lang {
	case "en":
		if p, err := exec.Tpdb.SearchTPDB(ctx, originalName); err == nil {
			result["Name"] = p.Name
			result["BirthDate"] = p.BirthDate
			result["Height"] = p.Height
			result["CupSize"] = p.Measurements.CupSize
			result["Waist"] = p.Measurements.Waist
			result["Hip"] = p.Measurements.Hip
			result["Bio"] = p.Bio
			if len(p.Images) > 0 { result["ImageURL"] = p.Images[0].URL }
			result["ProviderIds"] = extractProviderIds(p.Urls)
		}
	
	case "zh", "ja":
		searchName := originalName
		if lang == "zh" {
			jaName, zhBio := fetchWikiData(ctx, "zh", originalName, exec.WikiEmail)
			if zhBio != "" { result["Bio"] = zhBio }
			if jaName != "" { searchName = jaName }
		}
		
		if p, err := exec.JavStash.SearchPerformer(ctx, searchName); err == nil {
			result["Name"] = p.Name
			result["BirthDate"] = p.BirthDate
			result["PlaceOfBirth"] = "日本"
			result["AliasesString"] = strings.Join(p.Aliases, ", ")
			if len(p.Images) > 0 { result["ImageURL"] = p.Images[0].URL }
			
			mergedIds := extractProviderIds(p.Urls)
			
			var jaWikiTitle string
			for _, u := range p.Urls {
				if strings.Contains(u.URL, "ja.wikipedia.org/wiki/") {
					jaWikiTitle = u.URL[strings.LastIndex(u.URL, "/")+1:]
				}
			}
			if result["Bio"] == "" && jaWikiTitle != "" {
				if _, jaBio := fetchWikiData(ctx, "ja", jaWikiTitle, exec.WikiEmail); jaBio != "" {
					result["Bio"] = jaBio
				}
			}

			tpdbSearchName = p.Name
			for _, alias := range p.Aliases {
				if detectLanguage(alias) == "en" {
					tpdbSearchName = alias
					break
				}
			}

			if tpdbSearchName != "" {
				if tp, err := exec.Tpdb.SearchTPDB(ctx, tpdbSearchName); err == nil {
					result["Height"] = tp.Height
					result["CupSize"] = tp.Measurements.CupSize
					result["Waist"] = tp.Measurements.Waist
					result["Hip"] = tp.Measurements.Hip
					if result["Bio"] == "" { result["Bio"] = tp.Bio }
					
					tpdbIds := extractProviderIds(tp.Urls)
					for k, v := range tpdbIds {
						if mergedIds[k] == "" { mergedIds[k] = v }
					}
				}
			}
			result["ProviderIds"] = mergedIds
		}
	}
	return result, nil
}

// ============ 基础 Actions ============

func actionLogInfo(ctx context.Context, exec *ScriptExecutor, params, vars map[string]any) (any, error) {
	msg := fmt.Sprint(params["msg"])
	helpers.AppLogger.Infof("[刮削脚本] %s", msg)
	return msg, nil
}

func actionEmbyGetPerson(ctx context.Context, exec *ScriptExecutor, params, vars map[string]any) (any, error) {
	id := toString(params["id"])
	url := fmt.Sprintf("%s/api/v1/people/%s?api_key=%s", exec.Emby.BaseURL, id, exec.Emby.APIKey)
	var resp map[string]any
	if err := httpGetJSON(ctx, url, &resp); err != nil { return nil, err }
	return resp, nil
}

func actionEmbyUpdatePerson(ctx context.Context, exec *ScriptExecutor, params, vars map[string]any) (any, error) {
	id := toString(params["id"])
	if id == "" { return nil, fmt.Errorf("缺少 id") }
	
	body := map[string]any{}
	for k, v := range params {
		if k == "id" || v == nil { continue }
		switch k {
		case "place_of_birth": body["placeOfBirth"] = v
		case "known_for_department": body["knownForDepartment"] = v
		case "provider_ids": body["providerIds"] = v
		default: body[k] = v
		}
	}
	if _, ok := body["name"]; !ok { return nil, fmt.Errorf("name 必填") }

	data, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/api/v1/people/%s?api_key=%s", exec.Emby.BaseURL, id, exec.Emby.APIKey)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil { return nil, err }
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
	if id == "" { return nil, fmt.Errorf("缺少 id") }
	if imageURL == "" { return map[string]any{"ok": true, "message": "无图片URL，跳过上传"}, nil }
	
	imgReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	imgResp, err := http.DefaultClient.Do(imgReq)
	if err != nil { return nil, fmt.Errorf("下载图片失败：%w", err) }
	defer imgResp.Body.Close()
	if imgResp.StatusCode != http.StatusOK { return nil, fmt.Errorf("下载图片失败：HTTP %d", imgResp.StatusCode) }
	
	ct := imgResp.Header.Get("Content-Type")
	if ct == "" { ct = "image/jpeg" }
	imgData, _ := io.ReadAll(imgResp.Body)
	
	url := fmt.Sprintf("%s/emby/Items/%s/Images/Primary?api_key=%s", exec.Emby.BaseURL, id, exec.Emby.APIKey)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(imgData))
	req.Header.Set("Content-Type", ct)
	resp, err := http.DefaultClient.Do(req)
	if err != nil { return nil, err }
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("上传头像 %d: %s", resp.StatusCode, string(b))
	}
	return map[string]any{"ok": true, "size": len(imgData)}, nil
}

func actionHTTPGet(ctx context.Context, exec *ScriptExecutor, params, vars map[string]any) (any, error) {
	url := toString(params["url"])
	var resp map[string]any
	if err := httpGetJSON(ctx, url, &resp); err != nil { return nil, err }
	return resp, nil
}

func actionStashDBSearch(ctx context.Context, exec *ScriptExecutor, params, vars map[string]any) (any, error) {
	if exec.Stash == nil || exec.Stash.APIKey == "" { return nil, fmt.Errorf("StashDB 未配置") }
	return exec.Stash.SearchPerformer(ctx, toString(params["name"]))
}

func actionJavStashSearch(ctx context.Context, exec *ScriptExecutor, params, vars map[string]any) (any, error) {
	if exec.JavStash == nil || exec.JavStash.APIKey == "" { return nil, fmt.Errorf("JavStash 未配置") }
	return exec.JavStash.SearchPerformer(ctx, toString(params["name"]))
}

func actionTPDBSearch(ctx context.Context, exec *ScriptExecutor, params, vars map[string]any) (any, error) {
	if exec.Tpdb == nil || exec.Tpdb.APIKey == "" { return nil, fmt.Errorf("TPDB 未配置") }
	return exec.Tpdb.SearchTPDB(ctx, toString(params["name"]))
}

func toString(v any) string {
	if v == nil { return "" }
	if s, ok := v.(string); ok { return s }
	return fmt.Sprint(v)
}

func httpGetJSON(ctx context.Context, url string, out any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil { return err }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
