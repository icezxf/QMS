package avscrape

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"your-project/helpers"
	"your-project/models"
	"your-project/pkg/dictdb"
)

type WikiClient struct{}

// GetChineseName 获取演员中文名
// 优先级：本地 PG 字典（脏数据拦截） -> 其他源（保留你原有逻辑） -> Ouxiang 兜底
func (w *WikiClient) GetChineseName(inputName string) (string, string, error) {
	inputName = strings.TrimSpace(inputName)
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
			// ★ 核心拦截：只有纯中文才算命中；带假名/英文的脏数据跳过
			if isChineseName(zh) {
				helpers.AppLogger.Infof("[字典] %s → %s", inputName, zh)
				return zh, "dict", nil
			}
			helpers.AppLogger.Warnf("[字典] %s 命中了异常数据 (%s)，尝试通过其他源查找", inputName, zh)
		}
	}

	// ===== 2. 你原有的缓存 / 维基百科查询逻辑保留在这里 =====
	// if zh, source, err := w.getFromCacheOrWiki(inputName); err == nil && zh != "" {
	//     return zh, source, nil
	// }

	// ===== 3. Ouxiang 兜底 =====
	if jpName, zhName, ok := fetchOuxiangChineseName(inputName); ok {
		helpers.AppLogger.Infof("[Ouxiang] %s → %s（日文名：%s）", inputName, zhName, jpName)
		return zhName, "ouxiang", nil
	}

	// ===== 4. 全失败，保留原名 =====
	return inputName, "fallback", nil
}

// ============================================================
// Ouxiang 兜底：优先抓「中文名」字段
// ============================================================

var ouxiangHTTP = &http.Client{Timeout: 15 * time.Second}

var (
	ouxiangIdolRe = regexp.MustCompile(`href="(/idol/[^"]+)"`)
	ouxiangH1Re   = regexp.MustCompile(`<h1>([^<]+)</h1>`)
	htmlTagRe     = regexp.MustCompile(`<[^>]+>`)

	// 精准匹配页面里的「中文名」字段：
	// <tr><td>中文名</td><td>梦实香苗</td>
	cnNameRe = regexp.MustCompile(`(?s)<tr[^>]*>\s*<td[^>]*>\s*中文名\s*</td>\s*<td[^>]*>\s*([^<]+?)\s*</td>`)
)

// fetchOuxiangChineseName 从 ouxiangdanganku.com 搜索演员中文名
// 返回 (日文名/主名, 中文名, 是否命中)
func fetchOuxiangChineseName(keyword string) (jpName, zhName string, ok bool) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return "", "", false
	}

	normKeyword := normalizeStr(keyword)

	// 1. 搜索
	searchURL := "https://www.ouxiangdanganku.com/search/" + url.QueryEscape(keyword)
	body, err := ouxGet(searchURL)
	if err != nil {
		return "", "", false
	}

	matches := ouxiangIdolRe.FindAllStringSubmatch(string(body), 5)
	if len(matches) == 0 {
		return "", "", false
	}

	// 2. 遍历详情页
	for _, m := range matches {
		detailPath := m[1]
		time.Sleep(150 * time.Millisecond)

		detailURL := "https://www.ouxiangdanganku.com" + detailPath
		body2, err := ouxGet(detailURL)
		if err != nil {
			continue
		}

		htmlStr := string(body2)

		// 提取 H1 主名
		h1 := ""
		if hm := ouxiangH1Re.FindStringSubmatch(htmlStr); len(hm) >= 2 {
			h1 = strings.TrimSpace(hm[1])
		}
		if h1 == "" {
			continue
		}

		// === 暴力切碎全网页，生成候选池（用于严格匹配） ===
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

		// === 严格完全匹配，防止串台 ===
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

		// === 匹配成功：优先抓「中文名」字段 ===
		var finalZhName string

		if m := cnNameRe.FindStringSubmatch(htmlStr); len(m) >= 2 {
			candidate := strings.TrimSpace(m[1])
			candidate = htmlTagRe.ReplaceAllString(candidate, "")
			candidate = strings.TrimSpace(candidate)

			if isChineseName(candidate) &&
				!strings.ContainsAny(candidate, "ぁあぃいぅうぇえぉおかがきぎくぐけげこごさざしじすずせぜそぞただちぢっつづてでとどなにぬねのはばぱひびぴふぶぷへべぺほぼぽまみむめもゃやゅゆょよらりるれろゎわゐゑをんァアィイゥウェエォオカガキギクグケゲコゴサザシジスズセゼソゾタダチヂッツヅテデトドナニヌネノハバパヒビピフブプヘベペホボポマミムメモャヤュユョヨラリルレロヮワヰヱヲンヴヵヶ") {
				finalZhName = candidate
			}
		}

		// === 中文名字段没取到，回退到候选池里找纯中文 ===
		if finalZhName == "" {
			for _, c := range candidates {
				if isChineseName(c) &&
					!strings.ContainsAny(c, "ぁあぃいぅうぇえぉおかがきぎくぐけげこごさざしじすずせぜそぞただちぢっつづてでとどなにぬねのはばぱひびぴふぶぷへべぺほぼぽまみむめもゃやゅゆょよらりるれろゎわゐゑをんァアィイゥウェエォオカガキギクグケゲコゴサザシジスズセゼソゾタダチヂッツヅテデトドナニヌネノハバパヒビピフブプヘベペホボポマミムメモャヤュユョヨラリルレロヮワヰヱヲンヴヵヶ") {
					finalZhName = c
					break
				}
			}
		}

		// === 最终兜底 ===
		if finalZhName == "" {
			finalZhName = h1
		}

		return h1, finalZhName, true
	}

	return "", "", false
}

// ouxGet 发起 Ouxiang 请求
func ouxGet(rawURL string) ([]byte, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := ouxiangHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ouxiang status: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// normalizeStr 名字标准化：去普通空格、全角空格，转小写
func normalizeStr(s string) string {
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "　", "")
	return strings.ToLower(s)
}

// isChineseName 判断是否纯中文名：不能含日文假名和英文字母，且至少包含一个汉字
func isChineseName(s string) bool {
	hasChinese := false
	for _, r := range s {
		// 日文平假名 / 片假名
		if (r >= 0x3040 && r <= 0x309F) || (r >= 0x30A0 && r <= 0x30FF) {
			return false
		}
		// 英文字母
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			return false
		}
		// 汉字
		if r >= 0x4E00 && r <= 0x9FFF {
			hasChinese = true
		}
	}
	return hasChinese
}

// toSimplified 繁简转换。若你的 helpers 包没有 ToSimplified，请替换为项目现有的转换函数
func toSimplified(s string) string {
	return helpers.ToSimplified(s)
}
