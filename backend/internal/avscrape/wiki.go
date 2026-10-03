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

func (w *WikiClient) GetChineseName(japaneseName string) (string, error) {
	if japaneseName == "" {
		return "", nil
	}

	w.mu.Lock()
	if entry, ok := w.cache[japaneseName]; ok {
		if time.Since(entry.at) < 24*time.Hour {
			w.mu.Unlock()
			return entry.zh, nil
		}
	}
	elapsed := time.Since(w.lastReq)
	if elapsed < 500*time.Millisecond {
		w.mu.Unlock()
		time.Sleep(500*time.Millisecond - elapsed)
		w.mu.Lock()
	}
	w.lastReq = time.Now()
	w.mu.Unlock()

	endpoint := fmt.Sprintf(
		"https://ja.wikipedia.org/w/api.php?action=query&titles=%s&prop=langlinks&lllang=zh&format=json&redirects=1",
		url.QueryEscape(japaneseName),
	)
	req, _ := http.NewRequest("GET", endpoint, nil)
	req.Header.Set("User-Agent", "QMediaSync/1.0 (AV Scraper)")

	resp, err := w.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

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
		return "", err
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

	// ===== 去掉消歧义括号后缀 =====
	if zhName != "" {
		zhName = stripDisambiguation(zhName)
	}
	// =============================

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

// stripDisambiguation 去掉维基消歧义后缀
// 例：
//   "Miru (AV女優)" → "Miru"
//   "S1 (成人影片製造商)" → "S1"
//   "新有菜" → "新有菜"（无括号，原样返回）
func stripDisambiguation(s string) string {
	s = strings.TrimSpace(s)
	// 半角括号（带空格）
	if idx := strings.LastIndex(s, " ("); idx > 0 {
		s = s[:idx]
	}
	// 半角括号（无空格）
	if idx := strings.LastIndex(s, "("); idx > 0 {
		s = s[:idx]
	}
	// 全角括号
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
