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
	zh string
	at time.Time
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

// GetChineseName 只做：字典 → 缓存 → 维基
// ouxiang 不在这里，放到 TranslateActorNames 最后兜底
func (w *WikiClient) GetChineseName(inputName string) (string, error) {
	if inputName == "" {
		return "", nil
	}

	searchName := toSimplified(inputName)

	// ===== 1. 字典 =====
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
			// ★ 只有纯中文才算命中；半中半日脏数据跳过，继续走维基
			if isChineseName(zh) {
				helpers.AppLogger.Infof("[字典] %s → %s", inputName, zh)
				return zh, nil
			}
			helpers.AppLogger.Warnf("[字典] %s 命中了异常数据 (%s)，尝试通过其他源查找", inputName, zh)
		}
	}

	// ===== 2. 缓存 =====
	w.mu.Lock()
	if entry, ok := w.cache[inputName]; ok {
		if time.Since(entry.at) < 24*time.Hour {
			w.mu.Unlock()
			return entry.zh, nil
		}
	}
	w.mu.Unlock()

	// ===== 3. 维基 =====
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
	}

	// ===== 4. 写缓存（空结果也写，避免重复请求维基） =====
	w.mu.Lock()
	w.cache[inputName] = &wikiCache{zh: zhName, at: time.Now()}
	w.mu.Unlock()

	if zhName != "" {
		helpers.AppLogger.Infof("[维基] %s → %s", inputName, zhName)
	}
	return zhName, nil
}

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
// ouxiangdanganku.com（最后兜底）
// ============================================================

var ouxiangHTTP = &http.Client{Timeout: 15 * time.Second}

var (
	ouxiangIdolRe   = regexp.MustCompile(`href="(/idol/[^"]+)"`)
	ouxiangH1Re     = regexp.MustCompile(`<h1>([^<]+)</h1>`)
	ouxiangCnNameRe = regexp.MustCompile(`(?s)<tr[^>]*>\s*<td[^>]*>\s*中文名\s*</td>\s*<td[^>]*>\s*([^<]+?)\s*</td>`)
	htmlTagRe       = regexp.MustCompile(`<[^>]+>`)
)

func fetchOuxiangChineseName(keyword string) (jpName, zhName string, ok bool) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return "", "", false
	}

	normKeyword := normalizeStr(keyword)

	searchURL := "https://www.ouxiangdanganku.com/search/" + url.QueryEscape(keyword)
	body, err := ouxiangGet(searchURL)
	if err != nil {
		return "", "", false
	}

	matches := ouxiangIdolRe.FindAllStringSubmatch(body, 5)
	if len(matches) == 0 {
		return "", "", false
	}

	for _, m := range matches {
		detailPath := m[1]
		time.Sleep(150 * time.Millisecond)

		detailURL := "https://www.ouxiangdanganku.com" + detailPath
		htmlStr, err := ouxiangGet(detailURL)
		if err != nil {
			continue
		}

		h1 := ""
		if hm := ouxiangH1Re.FindStringSubmatch(htmlStr); len(hm) >= 2 {
			h1 = strings.TrimSpace(hm[1])
		}
		if h1 == "" {
			continue
		}

		// 暴力切碎候选池
		cleanHtml := strings.ReplaceAll(htmlStr, "<br>", ",")
		cleanHtml = strings.ReplaceAll(cleanHtml, "<br/>", ",")
		cleanHtml = strings.ReplaceAll(cleanHtml, "<br />", ",")
		cleanHtml = strings.ReplaceAll(cleanHtml, "</td>", ",")
		cleanHtml = htmlTagRe.ReplaceAllString(cleanHtml, " ")

		candidates := []string{h1}
		parts := regexp.MustCompile(`[，,\n\r]`).Split(cleanHtml, -1)
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if len(p) >= 2 && len(p) <= 20 &&
				!strings.ContainsAny(p, "0123456789年月日") &&
				!strings.Contains(p, "：") && !strings.Contains(p, ":") {
				candidates = append(candidates, p)
			}
		}

		// 严格完全匹配，防串台
		matched := false
		for _, c := range candidates {
			if normalizeStr(c) == normKeyword {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}

		// 优先抓「中文名」字段
		var finalZhName string
		if m := ouxiangCnNameRe.FindStringSubmatch(htmlStr); len(m) >= 2 {
			candidate := strings.TrimSpace(m[1])
			candidate = htmlTagRe.ReplaceAllString(candidate, "")
			candidate = strings.TrimSpace(candidate)
			if isChineseName(candidate) {
				finalZhName = candidate
			}
		}

		// 回退候选池找纯中文
		if finalZhName == "" {
			for _, c := range candidates {
				if isChineseName(c) && !strings.ContainsAny(c, "ぁあぃいぅうぇえぉおかがきぎくぐけげこごさざしじすずせぜそぞただちぢっつづてでとどなにぬねのはばぱひびぴふぶぷへべぺほぼぽまみむめもゃやゅゆょよらりるれろゎわゐゑをんァアィイゥウェエォオカガキギクグケゲコゴサザシジスズセゼソゾタダチヂッツヅテデトドナニヌネノハバパヒビピフブプヘベペホボポマミムメモャヤュユョヨラリルレロヮワヰヱヲンヴヵヶ") {
					finalZhName = c
					break
				}
			}
		}

		if finalZhName == "" {
			finalZhName = h1
		}

		return h1, finalZhName, true
	}

	return "", "", false
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

func normalizeStr(s string) string {
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "　", "")
	return strings.ToLower(s)
}

// ============================================================
// TranslateActorNames
// 返回 (actors, warnings, dictLogs)
// 顺序：字典/缓存/维基 → JavStash 别名（纯中文）→ ouxiang 最后兜底
// ============================================================

func (w *WikiClient) TranslateActorNames(actors []Actor) ([]Actor, []string, []string) {
	var warnings []string
	var dictLogs []string

	for i := range actors {
		oldName := actors[i].Name
		if oldName == "" {
			continue
		}

		// ===== 1. 字典 → 缓存 → 维基 =====
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
			dictLogs = append(dictLogs, fmt.Sprintf("演员: %s → %s", oldName, zh))
			continue
		}

		// ===== 2. JavStash 别名兜底（只接受纯中文） =====
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
			helpers.AppLogger.Infof("[演员] %s → %s (JavStash 别名兜底)", oldName, fallbackZh)

			// 写缓存，避免下次重复走维基/ouxiang
			w.mu.Lock()
			w.cache[oldName] = &wikiCache{zh: fallbackZh, at: time.Now()}
			w.mu.Unlock()
			continue
		}

		// ===== 3. ouxiang 最后的最后兜底 =====
		if jpName, zhFromOuxiang, ok := fetchOuxiangChineseName(oldName); ok && zhFromOuxiang != "" {
			zh = toSimplified(zhFromOuxiang)
			if zh != "" && zh != oldName {
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
				warnings = append(warnings, fmt.Sprintf("ouxiang-演员: %s → %s", oldName, zh))
				helpers.AppLogger.Infof("[ouxiang] %s → %s (日文名: %s)", oldName, zh, jpName)

				// 写缓存，避免下次重复走维基/ouxiang
				w.mu.Lock()
				w.cache[oldName] = &wikiCache{zh: zh, at: time.Now()}
				w.mu.Unlock()
				continue
			}
		}

		// ===== 4. 全部失败 =====
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
