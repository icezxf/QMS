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

	"qmediasync/internal/dictdb"
	"qmediasync/internal/helpers"
	"qmediasync/internal/models"
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

// GetChineseName 优先查本地 PG 字典，没命中再去查维基百科
func (w *WikiClient) GetChineseName(japaneseName string) (string, error) {
	if japaneseName == "" {
		return "", nil
	}

	// 1. ★ 最高优先级：查本地 PostgreSQL 数据库（毫秒级）
	if dictdb.DictDB != nil {
		var mapping models.ActorMapping
		err := dictdb.DictDB.Where("jp_name = ?", japaneseName).First(&mapping).Error
		if err == nil && mapping.ZhName != "" {
			return mapping.ZhName, nil
		}
	}

	// 2. 查本地内存缓存
	w.mu.Lock()
	if entry, ok := w.cache[japaneseName]; ok {
		if time.Since(entry.at) < 24*time.Hour {
			w.mu.Unlock()
			return entry.zh, nil
		}
	}
	w.mu.Unlock()

	// 3. 查维基百科
	zhName, err := w.fetchZh(japaneseName)

	// 4. 尝试加 "(AV女優)" 后缀查维基百科
	if zhName == "" {
		alt := japaneseName + " (AV女優)"
		zh2, err2 := w.fetchZh(alt)
		if err2 == nil && zh2 != "" {
			zhName = zh2
			helpers.AppLogger.Infof("[维基] %s 命中 (AV女優) 变体", japaneseName)
		}
	}

	// 5. 写缓存
	w.mu.Lock()
	w.cache[japaneseName] = &wikiCache{zh: zhName, at: time.Now()}
	w.mu.Unlock()

	if zhName != "" {
		helpers.AppLogger.Infof("[维基] %s → %s", japaneseName, zhName)
	}
	return zhName, nil
}

// GetJapaneseName 反向查：中文名 -> 日文名 (方便用中文名去刮削)
func (w *WikiClient) GetJapaneseName(chineseName string) string {
	if dictdb.DictDB != nil {
		var mapping models.ActorMapping
		err := dictdb.DictDB.Where("zh_name = ?", chineseName).First(&mapping).Error
		if err == nil && mapping.JpName != "" {
			return mapping.JpName
		}
	}
	return ""
}

func (w *WikiClient) fetchZh(japaneseName string) (string, error) {
	// 限速
	w.mu.Lock()
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
			time.Sleep(time.Duration(attempt-1) * wikiRetryDelay)
		}
		zh, err := w.requestOnce(endpoint, japaneseName)
		if err != nil {
			if strings.Contains(err.Error(), "RATE_LIMIT") {
				return "", err
			}
			lastErr = err
			continue
		}
		zhName = zh
		lastErr = nil
		break
	}

	if lastErr != nil {
		return "", lastErr
	}
	if zhName != "" {
		zhName = stripDisambiguation(zhName)
	}
	return zhName, nil
}

func (w *WikiClient) requestOnce(endpoint, japaneseName string) (string, error) {
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", wikiUserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := w.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return "", fmt.Errorf("RATE_LIMIT: HTTP 429")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var result struct {
		Query struct {
			Pages map[string]struct {
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
	return zhName, nil
}

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

func (w *WikiClient) TranslateActorNames(actors []Actor) ([]Actor, []string) {
	var warnings []string
	for i := range actors {
		oldName := actors[i].Name
		if oldName == "" {
			continue
		}

		zh, err := w.GetChineseName(oldName)
		if err == nil && zh != "" && zh != oldName {
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
			continue
		}

		if fallbackZh := pickChineseFromActor(actors[i]); fallbackZh != "" {
			if fallbackZh == oldName {
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
			actors[i].Name = fallbackZh
			helpers.AppLogger.Infof("[演员] %s → %s (JavStash 兜底)", oldName, fallbackZh)
			continue
		}

		if err != nil {
			warnings = append(warnings, fmt.Sprintf("演员 %s 翻译失败(%v)", oldName, err))
		}
	}
	return actors, warnings
}