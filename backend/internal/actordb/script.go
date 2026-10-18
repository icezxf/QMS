package actordb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"qmediasync/internal/helpers"

	"gopkg.in/yaml.v3"
)

// ============ 默认脚本 ============

const DefaultScrapeScript = `name: JavStash 默认刮削
steps:
  - action: log.info
    params:
      msg: "开始刮削演员：{{personName}} ({{personId}})"

  - action: emby.getPerson
    params:
      id: "{{personId}}"
    save: person

  - action: javstash.searchPerformer
    params:
      name: "{{person.Name}}"
    save: js

  - action: log.info
    params:
      msg: "JavStash 匹配：{{js.Name}} | 生日={{js.BirthDate}}"

  - action: emby.updatePerson
    params:
      id: "{{personId}}"
      name: "{{js.Name}}"
      birthday: "{{js.BirthDate}}"
      place_of_birth: "{{js.PlaceOfBirth}}"
      provider_ids: "{{js.ProviderIds}}"
      tags:
        - "身高:{{js.Height}}cm"
        - "罩杯:{{js.CupSize}}"
        - "三围:{{js.Waist}}-{{js.Hip}}"

  - action: emby.uploadAvatar
    params:
      id: "{{personId}}"
      imageUrl: "{{js.ImageURL}}"

  - action: log.info
    params:
      msg: "刮削完成：{{js.Name}}"
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

// ============ Stash-box 客户端 (StashDB / JavStash) ============

const DefaultStashDBEndpoint = "https://stashdb.org/graphql"
const DefaultJavStashEndpoint = "https://javstash.org/graphql"

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

// 正确的嵌套结构体
type StashBoxPerformer struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Aliases      []string `json:"aliases"`
	BirthDate    string   `json:"birth_date"`
	Country      string   `json:"country"`
	Ethnicity    string   `json:"ethnicity"`
	Height       int      `json:"height"`
	Gender       string   `json:"gender"`
	CareerStart  int      `json:"career_start_year"`
	CareerEnd    int      `json:"career_end_year"`
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
	id name aliases birth_date country ethnicity height gender career_start_year career_end_year
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
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

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

// ============ TPDB 客户端 ============

func (c *StashBoxClient) SearchTPDB(ctx context.Context, term string) (*StashBoxPerformer, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("TPDB API Key 未配置")
	}
	q := `query Search($term: String!) { searchPerformer(term: $term) { id name measurements { cup_size waist hip } height } }`
	body, _ := json.Marshal(map[string]any{"query": q, "variables": map[string]any{"term": term}})
	
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://theporndb.net/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey) // TPDB 专属认证
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

// ============ 执行器 ============

type ScriptExecutor struct {
	Emby     *EmbyClient
	Stash    *StashBoxClient
	JavStash *StashBoxClient
	Tpdb     *StashBoxClient
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

		if step.Save != "" {
			vars[step.Save] = out
		}

		results = append(results, StepResult{Index: i, Action: step.Action, OK: true, Message: summarize(out), Duration: dur.String()})
	}
	return vars, results, nil
}

// ============ 模板与数据转换 ============

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

// 将底层抓取的数据格式化为脚本可用的变量，提取映射和外部ID
func formatStashResult(p *StashBoxPerformer) map[string]any {
	imageURL := ""
	if len(p.Images) > 0 {
		imageURL = p.Images[0].URL
	}

	countryMap := map[string]string{
		"JP": "日本", "US": "美国", "KR": "韩国", "CN": "中国", "TW": "中国台湾", "HK": "中国香港",
	}
	placeOfBirth := countryMap[p.Country]
	if placeOfBirth == "" { placeOfBirth = p.Country }

	providerIds := map[string]string{}
	for _, u := range p.Urls {
		if strings.Contains(u.URL, "wikidata.org/wiki/") {
			parts := strings.Split(u.URL, "/")
			providerIds["Wikidata"] = parts[len(parts)-1]
		}
		if strings.Contains(u.URL, "stashdb.org/performers/") {
			parts := strings.Split(u.URL, "/")
			providerIds["StashDB"] = parts[len(parts)-1]
		}
	}

	return map[string]any{
		"ID":           p.ID,
		"Name":         p.Name,
		"Aliases":      p.Aliases,
		"BirthDate":    p.BirthDate,
		"Country":      p.Country,
		"PlaceOfBirth": placeOfBirth,
		"Ethnicity":    p.Ethnicity,
		"Height":       p.Height,
		"CupSize":      p.Measurements.CupSize,
		"Waist":        p.Measurements.Waist,
		"Hip":          p.Measurements.Hip,
		"Gender":       p.Gender,
		"CareerStart":  p.CareerStart,
		"CareerEnd":    p.CareerEnd,
		"ImageURL":     imageURL,
		"ProviderIds":  providerIds,
	}
}

// ============ Action 注册表 ============

type actionFn func(ctx context.Context, exec *ScriptExecutor, params, vars map[string]any) (any, error)

var actionRegistry = map[string]actionFn{
	"emby.getPerson":           actionEmbyGetPerson,
	"emby.updatePerson":        actionEmbyUpdatePerson,
	"emby.uploadAvatar":        actionEmbyUploadAvatar,
	"http.get":                 actionHTTPGet,
	"stashdb.searchPerformer":  actionStashDBSearch,
	"javstash.searchPerformer": actionJavStashSearch,
	"tpdb.searchPerformer":     actionTPDBSearch,
	"log.info":                 actionLogInfo,
}

func actionLogInfo(ctx context.Context, exec *ScriptExecutor, params, vars map[string]any) (any, error) {
	msg := fmt.Sprint(params["msg"])
	helpers.AppLogger.Infof("[刮削脚本] %s", msg)
	return msg, nil
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
	if id == "" || imageURL == "" { return nil, fmt.Errorf("缺少 id 或 imageUrl") }
	
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
	p, err := exec.Stash.SearchPerformer(ctx, toString(params["name"]))
	if err != nil { return nil, err }
	return formatStashResult(p), nil
}

func actionJavStashSearch(ctx context.Context, exec *ScriptExecutor, params, vars map[string]any) (any, error) {
	if exec.JavStash == nil || exec.JavStash.APIKey == "" { return nil, fmt.Errorf("JavStash 未配置") }
	p, err := exec.JavStash.SearchPerformer(ctx, toString(params["name"]))
	if err != nil { return nil, err }
	return formatStashResult(p), nil
}

func actionTPDBSearch(ctx context.Context, exec *ScriptExecutor, params, vars map[string]any) (any, error) {
	if exec.Tpdb == nil || exec.Tpdb.APIKey == "" { return nil, fmt.Errorf("TPDB 未配置") }
	p, err := exec.Tpdb.SearchTPDB(ctx, toString(params["name"]))
	if err != nil { return nil, err }
	
	return map[string]any{
		"CupSize": p.Measurements.CupSize,
		"Waist":   p.Measurements.Waist,
		"Hip":     p.Measurements.Hip,
		"Height":  p.Height,
	}, nil
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