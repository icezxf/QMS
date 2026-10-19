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

	"github.com/liuzl/gocc"
)

const (
	wikiMaxRetries  = 3
	wikiRetryDelay  = 2 * time.Second
	wikiMinInterval = 1500 * time.Millisecond
)

const wikiUserAgent = "QMediaSync/1.0 (https://github.com/icezxf/QMS; bocsx000@hotmail.com)"

// ===== 全局繁简转换器 =====
var (
	t2sConverter *gocc.OpenCC
)

func init() {
	// s2t 是简转繁，t2s 是繁转简
	t2sConverter, _ = gocc.New("t2s")
}

// toSimplified 繁转简
func toSimplified(s string) string {
	if s == "" || t2sConverter == nil {
		return s
	}
	res, err := t2sConverter.Convert(s)
	if err == nil {
		return res
	}
	return s
}

// ============================================================

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

// pickChineseFromActor 从 Actor 的 Name 和 Aliases 里找中文名
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
func (w *WikiClient) GetChineseName(inputName string) (string, error) {
	if inputName == "" {
		return "", nil
	}

	// 1. 把输入的名字统一洗成简体，用于查库
	searchName := toSimplified(inputName)

	// 2. 优先查本地 PG 字典
	if dictdb.DictDB != nil {
		var mapping models.ActorMapping
		// 用 简体名 或者 原始名 去查（扩大命中率）
		err := dictdb.DictDB.Where("jp_name = ? OR zh_name = ? OR jp_name = ?", searchName, searchName, inputName).First(&mapping).Error
		if err == nil && mapping.ZhName != "" {
			return toSimplified(mapping.ZhName), nil
		}
	}

	// 3. 查本地内存缓存
	w.mu.Lock()
	if entry, ok := w.cache[inputName]; ok {
		if time.Since(entry.at) < 24*time.Hour {
			w.mu.Unlock()
			return entry.zh, nil
		}
	}
	w.mu.Unlock()

	// 4. ★ 核心：去查维基百科时，用【原始输入】去查！
	zhName, err := w.fetchZh(inputName)
	if err != nil {
		helpers.AppLogger.Warnf("[维基] 查询 %s 失败: %v", inputName, err)
	}

	// 5. 如果原始输入没查到，再用【简体】试一次（防某些奇葩页面）
	if zhName == "" && searchName != inputName {
		zhName, _ = w.fetchZh(searchName)
	}

	// 6. 尝试加 "(AV女優)" 后缀查
	if zhName == "" {
		alt := inputName + " (AV女優)"
		zh2, err2 := w.fetchZh(alt)
		if err2 == nil && zh2 != "" {
			zhName = zh2
		}
	}

	// 7. 最终结果统一转简体
	if zhName != "" {
		zhName = toSimplified(zhName)
	}

	// 8. 写缓存
	w.mu.Lock()
	w.cache[inputName] = &wikiCache{zh: zhName, at: time.Now()}
	w.mu.Unlock()

	if zhName != "" {
		helpers.AppLogger.Infof("[维基] %s → %s", inputName, zhName)
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

// fetchZh 单次查询维基百科
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
			delay := time.Duration(attempt-1) * wikiRetryDelay
			helpers.AppLogger.Infof("[维基] %s 第 %d 次重试，等待 %v", japaneseName, attempt, delay)
			time.Sleep(delay)
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
		return "", fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("User-Agent", wikiUserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := w.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %w", err)
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
		return "", fmt.Errorf("读取响应失败: %w", err)
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

// TranslateActorNames 演员名翻译：优先 PG字典 -> 维基 -> JavStash 中文兜底
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