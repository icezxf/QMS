package avscrape

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"qmediasync/internal/helpers"
)

type WikiClient struct {
	HTTP    *http.Client
	lastReq time.Time
	mu      sync.Mutex
	cache   map[string]*wikiCache
}

type wikiCache struct {
	zh string
	at time.Time
}

func NewWikiClient() *WikiClient {
	return &WikiClient{
		HTTP:  &http.Client{Timeout: 15 * time.Second},
		cache: make(map[string]*wikiCache),
	}
}

// ===== 重试参数 =====
const (
	wikiMaxRetries = 3
	wikiRetryDelay = 2 * time.Second
	// ===== 改动 1：查询间隔从 500ms 加大到 1500ms，避免维基限流 =====
	wikiMinInterval = 1500 * time.Millisecond
	// ============================================================
)

// ===== 改动 2：User-Agent 加联系方式，符合维基 API 最佳实践 =====
const wikiUserAgent = "QMediaSync/1.0 (https://github.com/icezxf/QMS; bocsx000@hotmail.com)"
// ==============================================================

func (w *WikiClient) GetChineseName(japaneseName string) (string, error) {
	if japaneseName == "" {
		return "", nil
	}

	// 缓存命中
	w.mu.Lock()
	if entry, ok := w.cache[japaneseName]; ok {
		if time.Since(entry.at) < 24*time.Hour {
			w.mu.Unlock()
			return entry.zh, nil
		}
	}
	// 限速
	elapsed := time.Since(w.lastReq)
	if elapsed < wikiMinInterval {
		w.mu.Unlock()
		time.Sleep(wikiMinInterval - elapsed)
		w.mu.Lock()
	}
	w.lastReq = time.Now()
	w.mu.Unlock()

	endpoint := fmt.Sprintf(
		"https://ja.wikipedia.org/w/api.php?action=query&titles=%s&prop=langlinks&lllang=zh&format=json&redirects=1",
		url.QueryEscape(japaneseName),
	)

	// ===== 重试循环 =====
	var lastErr error
	var zhName string

	for attempt := 1; attempt <= wikiMaxRetries; attempt++ {
		if attempt > 1 {
			delay := time.Duration(attempt-1) * wikiRetryDelay
			helpers.AppLogger.Infof("[维基] %s 第 %d 次重试，等待 %v", japaneseName, attempt, delay)
			time.Sleep(delay)
		}

		zh, err := w.requestOnce(endpoint, japaneseName)
		if err != nil {
			lastErr = err
			continue
		}
		zhName = zh
		lastErr = nil
		break
	}

	if lastErr != nil {
		helpers.AppLogger.Warnf("[维基] %s 重试 %d 次均失败: %v", japaneseName, wikiMaxRetries, lastErr)
		return "", lastErr
	}

	// 去消歧义括号
	if zhName != "" {
		zhName = stripDisambiguation(zhName)
	}

	// 写缓存
	w.mu.Lock()
	w.cache[japaneseName] = &wikiCache{zh: zhName, at: time.Now()}
	w.mu.Unlock()

	if zhName != "" {
		helpers.AppLogger.Infof("[维基] %s → %s", japaneseName, zhName)
	} else {
		helpers.AppLogger.Infof("[维基] %s 未找到中文译名", japaneseName)
	}
	return zhName, nil
}

// requestOnce 单次请求，含 HTTP 状态码和 Body 内容校验
func (w *WikiClient) requestOnce(endpoint, japaneseName string) (string, error) {
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("构造请求失败: %w", err)
	}
	// ===== 改动 2：使用带联系方式的 User-Agent =====
	req.Header.Set("User-Agent", wikiUserAgent)
	// ==============================================
	req.Header.Set("Accept", "application/json")

	resp, err := w.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	// ===== 状态码检查 =====
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}
	if len(body) == 0 {
		return "", fmt.Errorf("响应为空")
	}

	// ===== Body 内容检查：必须以 { 或 [ 开头 =====
	trimmed := strings.TrimSpace(string(body))
	if len(trimmed) == 0 {
		return "", fmt.Errorf("响应为空")
	}
	firstChar := trimmed[0]
	if firstChar != '{' && firstChar != '[' {
		preview := trimmed
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		return "", fmt.Errorf("响应不是 JSON（前 200 字符: %s）", preview)
	}

	// ===== 解析 JSON =====
	var result struct {
		Query struct {
			Pages map[string]struct {
				Title     string `json:"title"`
				Langlinks []struct {
					Lang string `json:"lang"`
					Name string `json:"*"`
				} `json:"langlinks"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析 JSON 失败: %w", err)
	}

	var zhName string
	for _, page := range result.Query.Pages {
		for _, link := range page.Langlinks {
			if link.Lang == "zh" && link.Name != "" {
				zhName = link.Name
				break
			}
		}
	}
	return zhName, nil
}

// stripDisambiguation 去掉维基消歧义后缀
// 例：
//   "Miru (AV女優)" → "Miru"
//   "S1 (成人影片製造商)" → "S1"
//   "新有菜" → "新有菜"（无括号，原样返回）
func stripDisambiguation(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.LastIndex(s, " ("); idx > 0 {
		s = s[:idx]
	}
	if idx := strings.LastIndex(s, "("); idx > 0 {
		s = s[:idx]
	}
	if idx := strings.LastIndex(s, "（"); idx > 0 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}

// TranslateActorNames 批量查询演员中文名
// 关键：翻译成功后把原始日文名加入 aliases，供后续翻译占位符使用
// 返回: (处理后的演员列表, 警告列表)
func (w *WikiClient) TranslateActorNames(actors []Actor) ([]Actor, []string) {
	var warnings []string
	for i := range actors {
		oldName := actors[i].Name
		if oldName == "" {
			continue
		}
		zh, err := w.GetChineseName(oldName)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("演员 %s 维基查询失败: %v", oldName, err))
			continue
		}
		if zh == "" {
			warnings = append(warnings, fmt.Sprintf("演员 %s 未找到中文译名", oldName))
			continue
		}
		if zh == oldName {
			continue
		}
		// 命中，翻译
		exists := false
		for _, a := range actors[i].Aliases {
			if a == oldName {
				exists = true
				break
			}
		}
		if !exists {
			actors[i].Aliases = append(actors[i].Aliases, oldName)
		}
		actors[i].Name = zh
	}
	return actors, warnings
}
