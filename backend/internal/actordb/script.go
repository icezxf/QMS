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

const DefaultScrapeScript = `name: 默认刮削脚本
steps:
  - action: log.info
    params:
      msg: "开始刮削：{{personId}} - {{personName}}"

  - action: emby.getPerson
    params:
      id: "{{personId}}"
    save: person

  - action: log.info
    params:
      msg: "Emby 当前简介长度：{{person.Biography}}"

  # 以后启用 StashDB 后，取消下面注释
  # - action: stashdb.searchPerformer
  #   params:
  #     name: "{{person.Name}}"
  #   save: performer
  #
  # - action: emby.updatePerson
  #   params:
  #     id: "{{personId}}"
  #     name: "{{performer.Name}}"
  #     biography: "{{performer.Bio}}"
  #     birthday: "{{performer.Birthday}}"
  #
  # - action: emby.uploadAvatar
  #   params:
  #     id: "{{personId}}"
  #     imageUrl: "{{performer.ImageURL}}"

  - action: log.info
    params:
      msg: "刮削结束"
`

// ============ 文件读写 ============

func scriptFilePath() string {
	return filepath.Join("/app/config", "emby_scrape_script.yaml")
}

// LoadScript 读用户自定义脚本；文件不存在或空则返回默认脚本
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

// ============ 执行器 ============

type ScriptExecutor struct {
	Emby *EmbyClient
}

func NewScriptExecutor(emby *EmbyClient) *ScriptExecutor {
	return &ScriptExecutor{Emby: emby}
}

// Run 执行脚本；input 变量注入模板
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
		// 模板替换
		params := map[string]any{}
		for k, v := range step.Params {
			params[k] = resolveTemplate(v, vars)
		}

		fn, ok := actionRegistry[step.Action]
		if !ok {
			results = append(results, StepResult{
				Index: i, Action: step.Action, OK: false,
				Message: fmt.Sprintf("未知 action: %s", step.Action),
			})
			return vars, results, fmt.Errorf("未知 action: %s", step.Action)
		}

		start := time.Now()
		out, err := fn(ctx, e, params, vars)
		dur := time.Since(start).Round(time.Millisecond)

		if err != nil {
			results = append(results, StepResult{
				Index: i, Action: step.Action, OK: false,
				Message: err.Error(), Duration: dur.String(),
			})
			return vars, results, fmt.Errorf("步骤 %d (%s) 失败：%w", i+1, step.Action, err)
		}

		if step.Save != "" {
			vars[step.Save] = out
		}

		results = append(results, StepResult{
			Index: i, Action: step.Action, OK: true,
			Message: summarize(out), Duration: dur.String(),
		})
	}

	return vars, results, nil
}

// ============ 模板解析 ============

var tmplRe = regexp.MustCompile(`\{\{\s*([^}]+?)\s*\}\}`)

func resolveTemplate(v any, vars map[string]any) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
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
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	s := string(b)
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

// ============ Action 注册 ============

type actionFn func(ctx context.Context, exec *ScriptExecutor, params, vars map[string]any) (any, error)

var actionRegistry = map[string]actionFn{
	"emby.getPerson":          actionEmbyGetPerson,
	"emby.updatePerson":       actionEmbyUpdatePerson,
	"emby.uploadAvatar":       actionEmbyUploadAvatar,
	"http.get":                actionHTTPGet,
	"stashdb.searchPerformer": actionStashDBSearch,
	"stashdb.getPerformer":    actionStashDBGet,
	"log.info":                actionLogInfo,
}

func actionLogInfo(ctx context.Context, exec *ScriptExecutor, params, vars map[string]any) (any, error) {
	msg := fmt.Sprint(params["msg"])
	helpers.AppLogger.Infof("[刮削脚本] %s", msg)
	return msg, nil
}

func actionEmbyGetPerson(ctx context.Context, exec *ScriptExecutor, params, vars map[string]any) (any, error) {
	id := toString(params["id"])
	if id == "" {
		return nil, fmt.Errorf("缺少 id")
	}
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
		body[k] = v
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
	if id == "" || imageURL == "" {
		return nil, fmt.Errorf("缺少 id 或 imageUrl")
	}
	imgReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	imgResp, err := http.DefaultClient.Do(imgReq)
	if err != nil {
		return nil, fmt.Errorf("下载图片失败：%w", err)
	}
	defer imgResp.Body.Close()
	if imgResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载图片失败：HTTP %d", imgResp.StatusCode)
	}
	ct := imgResp.Header.Get("Content-Type")
	if ct == "" {
		ct = "image/jpeg"
	}
	imgData, err := io.ReadAll(imgResp.Body)
	if err != nil {
		return nil, err
	}
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

func actionHTTPGet(ctx context.Context, exec *ScriptExecutor, params, vars map[string]any) (any, error) {
	url := toString(params["url"])
	if url == "" {
		return nil, fmt.Errorf("缺少 url")
	}
	var resp map[string]any
	if err := httpGetJSON(ctx, url, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func actionStashDBSearch(ctx context.Context, exec *ScriptExecutor, params, vars map[string]any) (any, error) {
	name := toString(params["name"])
	return map[string]any{
		"Name":     name,
		"Bio":      "",
		"ImageURL": "",
		"ID":       "",
		"_note":    "StashDB 接口待实现",
	}, nil
}

func actionStashDBGet(ctx context.Context, exec *ScriptExecutor, params, vars map[string]any) (any, error) {
	return nil, fmt.Errorf("StashDB getPerformer 待实现")
}

// ============ 工具 ============

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
