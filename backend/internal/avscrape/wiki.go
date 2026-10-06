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

const (
	wikiMaxRetries  = 3
	wikiRetryDelay  = 2 * time.Second
	wikiMinInterval = 1500 * time.Millisecond
)

const wikiUserAgent = "QMediaSync/1.0 (https://github.com/icezxf/QMS; bocsx000@hotmail.com)"

// pickChineseFromActor 从 Actor 的 Name 和 Aliases 里找中文名
// 优先 Name，其次 Aliases
func pickChineseFromActor(a Actor) string {
	if isChineseName(a.Name) {
		return a.Name
	}
	for _, alias := range a.Aliases {
		if isChineseName(alias) {
			return alias
		}
	}
	return ""
}

func (w *WikiClient) GetChineseName(japaneseName string) (string, error) {
	if japaneseName == "" {
		return "", nil
	}

	// 缓存
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
			// 429 限流直接放弃，不重试
			if strings.Contains(err.Error(), "RATE_LIMIT") {
				lastErr = err
				break
			}
			lastErr = err
			continue
		}
		zhName = zh
		lastErr = nil
		break
	}

	if lastErr != nil {
		helpers.AppLogger.Warnf("[维基] %s 查询失败: %v", japaneseName, lastErr)
		return "", lastErr
	}

	if zhName != "" {
		zhName = stripDisambiguation(zhName)
	}

	w.mu.Lock()
	w.cache[japaneseName] = &wikiCache{zh: zhName, at: time.Now()}
	w.mu.Unlock()

	if zhName != "" {
		helpers.AppLogger.Infof("[维基] %s → %s", japaneseName, zhName)
	} else {
		helpers.AppLogger.Infof("[维基] %s 无中文条目", japaneseName)
	}
	return zhName, nil
}

func (w *WikiClient) requestOnce(endpoint, japaneseName string) (string, error) {
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("User-Agent", wikiUserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := w.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 429 限流
	if resp.StatusCode == http.StatusTooManyRequests {
		return "", fmt.Errorf("RATE_LIMIT: HTTP 429")
	}

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

// TranslateActorNames 演员名翻译：优先 JavStash 中文 → 回退 wiki
func (w *WikiClient) TranslateActorNames(actors []Actor) ([]Actor, []string) {
	var warnings []string
	for i := range actors {
		oldName := actors[i].Name
		if oldName == "" {
			continue
		}

		// ===== 优先：从 JavStash 的 Name/Aliases 里找中文 =====
		if zh := pickChineseFromActor(actors[i]); zh != "" {
			if zh == oldName {
				// 原名就是中文，跳过
				continue
			}
			// 从 Aliases 里找到中文，替换
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
			helpers.AppLogger.Infof("[演员] %s → %s (JavStash)", oldName, zh)
			continue
		}
		// ====================================================

		// ===== 回退：查 wiki =====
		zh, err := w.GetChineseName(oldName)
		if err != nil {
			if strings.Contains(err.Error(), "RATE_LIMIT") {
				warnings = append(warnings, fmt.Sprintf("演员 %s 维基限流(429)，跳过翻译", oldName))
			} else {
				warnings = append(warnings, fmt.Sprintf("演员 %s 维基请求失败: %v", oldName, err))
			}
			continue
		}
		if zh == "" {
			warnings = append(warnings, fmt.Sprintf("演员 %s 维基无中文条目", oldName))
			continue
		}
		if zh == oldName {
			continue
		}
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
