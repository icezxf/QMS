package avscrape

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"qmediasync/internal/dictdb"
	"qmediasync/internal/helpers"
	"qmediasync/internal/models"
)

const (
	wikiMaxRetries  = 3
	wikiRetryDelay  = 2 * time.Second
	wikiMinInterval = 1500 * time.Millisecond
)

const wikiUserAgent = "QMediaSync/1.0 (https://github.com/icezxf/QMS; bocsx000@hotmail.com)"

// ============================================================
// WikiClient
// ============================================================

type WikiClient struct {
	HTTP    *http.Client
	lastReq time.Time
	mu      sync.Mutex
	cache   map[string]*wikiCache
}

type wikiCache struct {
	zh     string
	source string
	at     time.Time
}

func NewWikiClient() *WikiClient {
	return &WikiClient{
		HTTP:  &http.Client{Timeout: 15 * time.Second},
		cache: make(map[string]*wikiCache),
	}
}

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

// GetChineseName 三级兜底：字典 → 维基 → ouxiang
// 返回 (中文名, 来源, error)，来源为 "dict" / "wiki" / "ouxiang" / ""
func (w *WikiClient) GetChineseName(inputName string) (string, string, error) {
	if inputName == "" {
		return "", "", nil
	}

	searchName := toSimplified(inputName)

	// ===== 1. 优先查本地 PG 字典 =====
	if dictdb.DictDB != nil {
		var mapping models.ActorMapping

		escaped := strings.ReplaceAll(inputName, "%", "\\%")
		escaped = strings.ReplaceAll(escaped, "_", "\\_")
		kwPattern := fmt.Sprintf("%%,%s,%%", escaped)

		err := dictdb.DictDB.Where(
			"jp_name = ? OR zh_name = ? OR jp_name = ? OR keyword LIKE ? ESCAPE '\\'",
			searchName, searchName, inputName, kwPattern,
		).First(&mapping).Error
		if err == nil && mapping.ZhName != "" {
			zh := toSimplified(mapping.ZhName)
			helpers.AppLogger.Infof("[字典] %s → %s", inputName, zh)
			return zh, "dict", nil
		}
	}

	// ===== 2. 查本地内存缓存 =====
	w.mu.Lock()
	if entry, ok := w.cache[inputName]; ok {
		if time.Since(entry.at) < 24*time.Hour {
			w.mu.Unlock()
			return entry.zh, entry.source, nil
		}
	}
	w.mu.Unlock()

	// ===== 3. 查维基百科 =====
	zhName, err := w.fetchZh(inputName)
	if err != nil {
		helpers.AppLogger.Warnf("[维基] 查询 %s 失败: %v", inputName, err)
	}

	if zhName == "" && searchName != inputName {
		zhName, _ = w.fetchZh(searchName)
	}

	if zhName == "" {
		alt := inputName + " (AV女優)"
		zh2, err2 := w.fetchZh(alt)
		if err2 == nil && zh2 != "" {
			zhName = zh2
		}
	}

	if zhName != "" {
		zhName = toSimplified(zhName)
		w.mu.Lock()
		w.cache[inputName] = &wikiCache{zh: zhName, source: "wiki", at: time.Now()}
		w.mu.Unlock()
		helpers.AppLogger.Infof("[维基] %s → %s", inputName, zhName)
		return zhName, "wiki", nil
	}

	// ===== 4. ouxiangdanganku.com 兜底（维基失败后才走） =====
	if jpName, zhFromOuxiang, ok := fetchOuxiangChineseName(inputName); ok && zhFromOuxiang != "" {
		zh := toSimplified(zhFromOuxiang)
		helpers.AppLogger.Infof("[ouxiang] %s → %s (日文名: %s)", inputName, zh, jpName)
		w.mu.Lock()
		w.cache[inputName] = &wikiCache{zh: zh, source: "ouxiang", at: time.Now()}
		w.mu.Unlock()
		return zh, "ouxiang", nil
	}

	// ===== 5. 全部未命中，写空缓存 =====
	w.mu.Lock()
	w.cache[inputName] = &wikiCache{zh: "", source: "", at: time.Now()}
	w.mu.Unlock()
	return "", "", nil
}

// GetJapaneseName 反向查：中文名 -> 日文名
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

// ============================================================
// 维基百科
// ============================================================

func (w *WikiClient) fetchZh(japaneseName string) (string, error) {
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

// ============================================================
// ouxiangdanganku.com 兜底
// ============================================================

var ouxiangHTTP = &http.Client{Timeout: 15 * time.Second}

var (
	ouxiangIdolRe  = regexp.MustCompile(`href="(/idol/[^"]+)"`)
	ouxiangH1Re    = regexp.MustCompile(`<h1>([^<]+)</h1>`)
	ouxiangIntroRe = regexp.MustCompile(`<section id="intro"><p>([^<]+)`)
)

// fetchOuxiangChineseName 从 ouxiangdanganku.com 搜索演员中文名
// 返回 (日文名, 中文名, 是否命中)
// 搜不到、请求失败、解析失败都返回 ok=false，静默兜底
func fetchOuxiangChineseName(keyword string) (jpName, zhName string, ok bool) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return "", "", false
	}

	// 1. 搜索
	searchURL := "https://www.ouxiangdanganku.com/search/" + url.QueryEscape(keyword)
	body, err := ouxiangGet(searchURL)
	if err != nil {
		return "", "", false
	}

	// 提取所有 /idol/ 链接，最多取前 5 个
	matches := ouxiangIdolRe.FindAllStringSubmatch(body, 5)
	if len(matches) == 0 {
		return "", "", false
	}

	// 2. 逐个请求详情页，找到简介里含 keyword 的那个
	for _, m := range matches {
		detailPath := m[1]
		time.Sleep(150 * time.Millisecond)

		detailURL := "https://www.ouxiangdanganku.com" + detailPath
		body2, err := ouxiangGet(detailURL)
		if err != nil {
			continue
		}

		h1 := ""
		if hm := ouxiangH1Re.FindStringSubmatch(body2); len(hm) >= 2 {
			h1 = strings.TrimSpace(hm[1])
		}

		intro := ""
		if im := ouxiangIntroRe.FindStringSubmatch(body2); len(im) >= 2 {
			intro = strings.TrimSpace(im[1])
		}

		// 校验：简介里必须包含原搜索词，避免搜错人
		if intro == "" || !strings.Contains(intro, keyword) {
			continue
		}

		zh := extractChineseFromIntro(intro)
		if zh == "" {
			continue
		}

		return h1, zh, true
	}

	return "", "", false
}

// extractChineseFromIntro 从简介括号里提取中文名
func extractChineseFromIntro(intro string) string {
	start := strings.Index(intro, "（")
	end := strings.Index(intro, "）")
	if start < 0 || end <= start {
		return ""
	}
	inner := intro[start+len("（") : end]

	parts := regexp.MustCompile(`[，,／/、]`).Split(inner, -1)
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// 排除日期
		if strings.ContainsAny(p, "年月日") {
			continue
		}
		// 含汉字、不含假名、不含英文字母 → 视为中文名
		if isChineseName(p) {
			return p
		}
	}
	return ""
}

func ouxiangGet(rawURL string) (string, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")

	resp, err := ouxiangHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// ============================================================
// TranslateActorNames
// ============================================================

// TranslateActorNames 演员名翻译
// 返回 (actors, warnings, dictLogs)
// 字典/维基命中 → dictLogs；ouxiang 兜底命中 → warnings；未翻译 → warnings
func (w *WikiClient) TranslateActorNames(actors []Actor) ([]Actor, []string, []string) {
	var warnings []string
	var dictLogs []string

	for i := range actors {
		oldName := actors[i].Name
		if oldName == "" {
			continue
		}

		zh, source, err := w.GetChineseName(oldName)
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

			// ouxiang 兜底命中 → warnings；字典/维基命中 → dictLogs
			if source == "ouxiang" {
				warnings = append(warnings, fmt.Sprintf("ouxiang-演员: %s → %s", oldName, zh))
			} else {
				dictLogs = append(dictLogs, fmt.Sprintf("演员: %s → %s", oldName, zh))
			}
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
			dictLogs = append(dictLogs, fmt.Sprintf("演员(别名): %s → %s", oldName, fallbackZh))
			continue
		}

		if err != nil {
			warnings = append(warnings, fmt.Sprintf("演员 %s 翻译失败(%v)", oldName, err))
		}
	}

	// 最终检查：还有哪些演员名不是中文
	for _, a := range actors {
		if a.Name == "" {
			continue
		}
		if !isChineseName(a.Name) {
			warnings = append(warnings, fmt.Sprintf("演员名未翻译: %s", a.Name))
		}
	}

	return actors, warnings, dictLogs
}