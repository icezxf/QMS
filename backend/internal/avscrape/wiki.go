package avscrape

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"Q115-STRM/internal/helpers"
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

// TranslateActorNames 批量查询演员中文名
// 关键：翻译成功后把原始日文名加入 aliases，供后续翻译占位符使用
func (w *WikiClient) TranslateActorNames(actors []Actor) []Actor {
	for i := range actors {
		oldName := actors[i].Name
		if zh, err := w.GetChineseName(oldName); err == nil && zh != "" && zh != oldName {
			// 把原始日文名加进 aliases
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
	}
	return actors
}
