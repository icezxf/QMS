package avscrape

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"qmediasync/internal/dictdb"
	"qmediasync/internal/helpers"
	"qmediasync/internal/models"

	"github.com/liuzl/gocc"
	"gorm.io/gorm"
)

type Service struct {
	DB *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	return &Service{DB: db}
}

// ===== 全局繁简转换器（统一放在 service.go） =====
var t2sConverter *gocc.OpenCC

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

// ===== 片商硬编码映射（mapping_info.xml 不含片商翻译） =====
var studioOverrides = map[string]string{
	"エスワン ナンバーワンスタイル": "S1",
	"エスワン":              "S1",
	"ムーディーズ":            "Moodyz",
	"プレステージ":            "Prestige",
	"アイデアポケット":          "IP社",
	// 按需补充
}

func translateStudio(studio string) string {
	if zh, ok := studioOverrides[studio]; ok {
		return zh
	}
	return studio
}

// getInfoMapping 基于 keyword 字段做模糊匹配
// 返回值：
//   zhName != "" 且 != "删除" -> 命中翻译
//   zhName == "删除"          -> 命中删除规则
//   hit == false              -> 未命中
func getInfoMapping(jpName string) (zhName string, hit bool) {
	if dictdb.DictDB == nil || jpName == "" {
		return "", false
	}

	// 转义 LIKE 通配符，防止标签名本身包含 % 或 _
	escaped := strings.ReplaceAll(jpName, "%", "\\%")
	escaped = strings.ReplaceAll(escaped, "_", "\\_")
	pattern := fmt.Sprintf("%%,%s,%%", escaped)

	var mapping models.InfoMapping
	err := dictdb.DictDB.Where("keyword LIKE ? ESCAPE '\\'", pattern).First(&mapping).Error
	if err != nil {
		return "", false
	}

	if mapping.ZhName == "删除" {
		return "删除", true
	}
	if mapping.ZhName != "" {
		return mapping.ZhName, true
	}
	return "", false
}

// ===== 全局 WikiClient 单例 =====
var (
	globalWikiClient *WikiClient
)

func GetWikiClient() *WikiClient {
	if globalWikiClient == nil {
		globalWikiClient = NewWikiClient()
	}
	return globalWikiClient
}

// ============================================================

func (s *Service) Scrape(code string, oshash string) (*ScrapeResult, error) {
	cfg, err := LoadConfig(s.DB)
	if err != nil {
		return nil, err
	}
	var allResults []*ScrapeResult
	effectiveCode := code

	// ===== oshash 优先匹配 JavStash =====
	oshashHit := false
	if oshash != "" && cfg.EnableOshashMatch && cfg.EnableJavStash {
		js := NewJavStashClient(cfg.JavStashEndpoint, cfg.JavStashAPIKey)
		if r, err := js.SearchByOshash(oshash); err == nil && r != nil {
			helpers.AppLogger.Infof("[AV刮削] oshash 优先命中: %s => %s", oshash, r.Code)
			allResults = append(allResults, r)
			oshashHit = true
			if r.Code != "" {
				effectiveCode = r.Code
			}
		} else if err != nil {
			if strings.Contains(err.Error(), "Cannot query field") ||
				strings.Contains(err.Error(), "Unknown argument") {
				helpers.AppLogger.Infof("[AV刮削] JavStash schema 不支持 oshash 查询，跳过")
			} else {
				helpers.AppLogger.Infof("[AV刮削] oshash 未命中，回退到番号搜索: %v", err)
			}
		}
	}

	// ===== MetaTube =====
	if cfg.EnableMetaTube && cfg.MetaTubeServer != "" {
		mt := NewMetaTubeClient(cfg.MetaTubeServer)
		hits, err := mt.Search(effectiveCode)
		if err == nil {
			var previewChosen bool
			var previewSource string

			for _, h := range hits {
				providerID := h.ProviderID
				if providerID == "" {
					providerID = extractProviderID(h.Source, h.Code)
				}
				if providerID == "" {
					continue
				}
				detail, err := mt.Detail(effectiveCode, providerID)
				if err == nil {
					helpers.AppLogger.Infof("[MetaTube] 详情成功: %s => %d 张剧照",
						providerID, len(detail.PreviewImages))

					if previewChosen {
						if len(detail.PreviewImages) > 0 {
							helpers.AppLogger.Infof("[MetaTube] 源 %s 也提供 %d 张剧照，已忽略（已选 %s）",
								providerID, len(detail.PreviewImages), previewSource)
						}
						detail.PreviewImages = nil
					} else if len(detail.PreviewImages) > 0 {
						previewChosen = true
						previewSource = providerID
						helpers.AppLogger.Infof("[MetaTube] 剧照采用源 %s（%d 张），后续源忽略",
							providerID, len(detail.PreviewImages))
					}

					allResults = append(allResults, detail)
				} else {
					helpers.AppLogger.Warnf("[MetaTube] 详情失败: %s => %v", providerID, err)
					allResults = append(allResults, h)
				}
			}
		} else {
			helpers.AppLogger.Warnf("[MetaTube] 搜索失败: %v", err)
		}
	}

	// ===== JavStash 番号搜索 =====
	if cfg.EnableJavStash && !oshashHit {
		js := NewJavStashClient(cfg.JavStashEndpoint, cfg.JavStashAPIKey)

		hits, err := js.Search(effectiveCode)
		if err != nil {
			helpers.AppLogger.Warnf("[JavStash] 搜索失败 %s: %v", effectiveCode, err)
		} else {
			helpers.AppLogger.Infof("[JavStash] 搜索 %s 返回 %d 条", effectiveCode, len(hits))
			allResults = append(allResults, hits...)
		}

		if len(hits) == 0 {
			altCode := stripNumberLeadingZeros(effectiveCode)
			if altCode != "" && altCode != effectiveCode {
				helpers.AppLogger.Infof("[JavStash] 备用番号 %s", altCode)
				if altHits, err2 := js.Search(altCode); err2 == nil {
					helpers.AppLogger.Infof("[JavStash] 备用 %s 返回 %d 条", altCode, len(altHits))
					allResults = append(allResults, altHits...)
				} else {
					helpers.AppLogger.Warnf("[JavStash] 备用 %s 搜索失败: %v", altCode, err2)
				}
			}
		}
	}

	if len(allResults) == 0 {
		return nil, fmt.Errorf("no result for %s", code)
	}

	// ===== 演员名归一化 =====
	aliasMap := buildJavStashAliasMap(allResults)
	if len(aliasMap) > 0 {
		helpers.AppLogger.Infof("[演员归一化] JavStash 提供了 %d 条别名映射", len(aliasMap))
		for _, r := range allResults {
			for i := range r.Actors {
				if master, ok := aliasMap[r.Actors[i].Name]; ok && master != r.Actors[i].Name {
					helpers.AppLogger.Infof("[演员归一化] %s → %s (来源=%s)", r.Actors[i].Name, master, r.Source)
					r.Actors[i].Name = master
				}
			}
		}
	}

	// ===== 括号清洗 =====
	for _, r := range allResults {
		r.Actors = normalizeParenActorNames(r.Actors)
	}

	// ===== 合并多源 =====
	best := mergeResults(allResults, cfg)

	// ===== 从 r18.dev 链接提取 content_id =====
	seenCID := map[string]bool{}
	for _, u := range best.Urls {
		if strings.Contains(u, "r18.dev") {
			if cid := extractR18ContentID(u); cid != "" && !seenCID[cid] {
				best.DMMContentIDs = append(best.DMMContentIDs, cid)
				seenCID[cid] = true
				helpers.AppLogger.Infof("[AV刮削] 从 r18.dev 提取到 content_id: %s", cid)
			}
		}
	}

	// ===== 收集警告和字典日志 =====
	var warnings []string
	var dictLogs []string

	// ===== 1. 演员维基翻译（走 PG 字典 + 维基） =====
	wikiClient := GetWikiClient()
	translatedActors, actorWarnings, actorDictLogs := wikiClient.TranslateActorNames(best.Actors)
	best.Actors = translatedActors
	warnings = append(warnings, actorWarnings...)
	dictLogs = append(dictLogs, actorDictLogs...)

	// ===== 2. 番号标题优先查本地 PG 字典 =====
	if dictdb.DictDB != nil {
		var cNumMapping models.CNumberMapping
		if err := dictdb.DictDB.Where("number = ?", best.Code).First(&cNumMapping).Error; err == nil && cNumMapping.Title != "" {
			helpers.AppLogger.Infof("[AV刮削] 命中标题字典: %s => %s", best.Code, cNumMapping.Title)
			best.Title = toSimplified(cNumMapping.Title)
			dictLogs = append(dictLogs, fmt.Sprintf("标题: %s => %s", best.Code, best.Title))
		} else if altCode := stripNumberLeadingZeros(best.Code); altCode != best.Code {
			if err := dictdb.DictDB.Where("number = ?", altCode).First(&cNumMapping).Error; err == nil && cNumMapping.Title != "" {
				helpers.AppLogger.Infof("[AV刮削] 命中标题字典(备用): %s => %s", altCode, cNumMapping.Title)
				best.Title = toSimplified(cNumMapping.Title)
				dictLogs = append(dictLogs, fmt.Sprintf("标题: %s => %s", altCode, best.Title))
			}
		}
	}

	// ===== 3. 标签 / 系列 / 导演 / 片商 优先查本地 PG 字典 =====
	if dictdb.DictDB != nil {
		// ---- 标签：命中删除规则则丢弃，命中翻译则替换，未命中保留原值 ----
		var newGenres []string
		for _, g := range best.Genres {
			zh, hit := getInfoMapping(g)
			if hit {
				if zh == "删除" {
					helpers.AppLogger.Infof("[字典-标签] 丢弃: %s", g)
					dictLogs = append(dictLogs, fmt.Sprintf("标签: 丢弃 %s", g))
					continue
				}
				newGenres = append(newGenres, toSimplified(zh))
				helpers.AppLogger.Infof("[字典-标签] %s → %s", g, zh)
				dictLogs = append(dictLogs, fmt.Sprintf("标签: %s → %s", g, zh))
				continue
			}
			newGenres = append(newGenres, g)
		}
		best.Genres = newGenres

		// ---- 系列 ----
		if best.Series != "" {
			if zh, hit := getInfoMapping(best.Series); hit {
				if zh == "删除" {
					helpers.AppLogger.Infof("[字典-系列] 丢弃: %s", best.Series)
					dictLogs = append(dictLogs, fmt.Sprintf("系列: 丢弃 %s", best.Series))
					best.Series = ""
				} else {
					helpers.AppLogger.Infof("[字典-系列] %s → %s", best.Series, zh)
					dictLogs = append(dictLogs, fmt.Sprintf("系列: %s → %s", best.Series, zh))
					best.Series = toSimplified(zh)
				}
			}
		}

		// ---- 导演 ----
		if best.Director != "" {
			if zh, hit := getInfoMapping(best.Director); hit {
				if zh == "删除" {
					helpers.AppLogger.Infof("[字典-导演] 丢弃: %s", best.Director)
					dictLogs = append(dictLogs, fmt.Sprintf("导演: 丢弃 %s", best.Director))
					best.Director = ""
				} else {
					helpers.AppLogger.Infof("[字典-导演] %s → %s", best.Director, zh)
					dictLogs = append(dictLogs, fmt.Sprintf("导演: %s → %s", best.Director, zh))
					best.Director = toSimplified(zh)
				}
			}
		}

		// ---- 片商：先走硬编码映射，再尝试字典兜底 ----
		if best.Studio != "" {
			original := best.Studio
			best.Studio = translateStudio(best.Studio)
			if zh, hit := getInfoMapping(best.Studio); hit && zh != "删除" {
				best.Studio = toSimplified(zh)
			}
			if best.Studio != original {
				helpers.AppLogger.Infof("[片商] %s → %s", original, best.Studio)
				dictLogs = append(dictLogs, fmt.Sprintf("片商: %s → %s", original, best.Studio))
			} else {
				helpers.AppLogger.Infof("[片商] 最终使用值: %s", best.Studio)
			}
		}
	} else {
		// 无字典时也走硬编码兜底
		if best.Studio != "" {
			original := best.Studio
			best.Studio = translateStudio(best.Studio)
			if best.Studio != original {
				helpers.AppLogger.Infof("[片商] %s → %s", original, best.Studio)
			} else {
				helpers.AppLogger.Infof("[片商] 最终使用值: %s", best.Studio)
			}
		}
	}

	// ===== 3.5 jav8 兜底（字典之后、AI 翻译之前） =====
	// 标题：保持原逻辑，仅当日文时请求
	// 剧情：为空或日文时请求（空剧情也要补）
	needTitle := best.Title != "" && isJapanese(best.Title)
	needPlot := best.Plot == "" || isJapanese(best.Plot)

	if needTitle || needPlot {
		jav8Title, jav8Plot := FetchJav8(best.Code)

		if needTitle && jav8Title != "" {
			best.Title = toSimplified(jav8Title)
			warnings = append(warnings, fmt.Sprintf("jav8-标题: %s => %s", best.Code, best.Title))
			helpers.AppLogger.Infof("[jav8] %s 标题兜底: %s", best.Code, best.Title)
		}

		if needPlot && jav8Plot != "" {
			best.Plot = toSimplified(jav8Plot)
			warnings = append(warnings, fmt.Sprintf("jav8-剧情: %s 命中中文简介（%d 字）", best.Code, len(jav8Plot)))
			helpers.AppLogger.Infof("[jav8] %s 剧情兜底: %d 字", best.Code, len(jav8Plot))
		}
	}

	// ===== JavDB 评分 =====
	if cfg.EnableJavDBRating {
		client := NewJavDBClient(cfg.JavDBCookie)
		candidates := javdbCodeCandidates(code)
		got := false
		for _, c := range candidates {
			rating, votes, err := client.GetRating(c)
			if err != nil {
				helpers.AppLogger.Warnf("[AV刮削] JavDB 评分获取失败 [查询番号=%s]: %v", c, err)
				continue
			}
			if rating > 0 {
				best.Rating = rating
				best.Votes = votes
				helpers.AppLogger.Infof("[AV刮削] JavDB 评分: %.2f (%d人) [查询番号=%s]", rating, votes, c)
				got = true
				break
			}
			helpers.AppLogger.Infof("[AV刮削] JavDB 未找到 [查询番号=%s]，尝试下一个", c)
		}
		if !got {
			warnings = append(warnings, fmt.Sprintf("JavDB 评分获取失败: 所有候选番号均未找到 (%s)",
				strings.Join(candidates, ", ")))
		}
	}

	// ===== 4. 批量翻译（内部会自动过滤掉已经是纯中文的标题/标签） =====
	if cfg.EnableTranslate {
		tr := NewTranslator(cfg.TranslateEngine, cfg.TranslateTarget)
		tr.DeepLKey = cfg.TranslateDeepLKey
		tr.BingKey = cfg.TranslateBingKey
		tr.BingRegion = cfg.TranslateBingRegion
		tr.GeminiKey = cfg.TranslateGeminiKey
		tr.GeminiModel = cfg.TranslateGeminiModel
		tr.GoogleAPIKey = cfg.GoogleTranslateAPIKey
		tr.GoogleTranslateForTags = cfg.GoogleTranslateForTags
		tr.GoogleTranslateForAll = cfg.GoogleTranslateForAll
		helpers.AppLogger.Infof("[AV刮削] 开始批量翻译 %s (engine=%s)", code, cfg.TranslateEngine)
		transWarnings := tr.TranslateResultBatch(best)
		warnings = append(warnings, transWarnings...)
	}

	best.Warnings = warnings
	best.DictLogs = dictLogs
	best.Oshash = oshash

	media := MediaFromResult(best)

	var existing models.AVMedia
	err = s.DB.Where("code = ?", media.Code).First(&existing).Error
	if err == nil {
		media.ID = existing.ID
		media.CreatedAt = existing.CreatedAt
		if err := s.DB.Save(media).Error; err != nil {
			return nil, err
		}
	} else {
		if err := s.DB.Create(media).Error; err != nil {
			return nil, err
		}
	}
	return best, nil
}

// ============================================================
// 以下为辅助函数（保持原有逻辑不变）
// ============================================================

func extractR18ContentID(url string) string {
	re := regexp.MustCompile(`(?:id|combined)=([^/?]+)`)
	m := re.FindStringSubmatch(url)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func stripLeadingDigits(s string) string {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return s[i:]
}

func stripNumberLeadingZeros(s string) string {
	sep := -1
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if c == '-' || c == '_' || c == ' ' {
			sep = i
			break
		}
	}
	if sep < 0 || sep == len(s)-1 {
		return s
	}
	prefix := s[:sep+1]
	numPart := s[sep+1:]
	for i := 0; i < len(numPart); i++ {
		if numPart[i] < '0' || numPart[i] > '9' {
			return s
		}
	}
	j := 0
	for j < len(numPart)-1 && numPart[j] == '0' {
		j++
	}
	if j == 0 {
		return s
	}
	return prefix + numPart[j:]
}

func javdbCodeCandidates(code string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}

	base := stripLeadingDigits(code)
	if base == "" {
		base = code
	}
	add(base)
	add(stripNumberLeadingZeros(base))
	if base != code {
		add(code)
	}
	return out
}

func buildJavStashAliasMap(results []*ScrapeResult) map[string]string {
	m := make(map[string]string)
	for _, r := range results {
		if !strings.HasPrefix(r.Source, "javstash") {
			continue
		}
		for _, a := range r.Actors {
			master := a.Name
			if master == "" {
				continue
			}
			m[master] = master
			for _, alias := range a.Aliases {
				if alias == "" {
					continue
				}
				m[alias] = master
			}
		}
	}
	return m
}

// ============================================================
// 合并多源
// ============================================================

func mergeResults(results []*ScrapeResult, cfg *Config) *ScrapeResult {
	sorted := sortByChinese(results, cfg.PreferChineseSource)
	best := *sorted[0]
	best.ImageCandidates = []string{}
	best.Warnings = nil

	type candidate struct {
		url      string
		priority int
	}
	var imageList []candidate
	for _, r := range sorted {
		p := sourceImagePriority(r.Source)
		if r.Poster != "" {
			imageList = append(imageList, candidate{r.Poster, p})
		}
		if r.Fanart != "" {
			imageList = append(imageList, candidate{r.Fanart, p})
		}
	}

	sort.SliceStable(imageList, func(i, j int) bool {
		return imageList[i].priority < imageList[j].priority
	})

	seen := map[string]bool{}
	for _, c := range imageList {
		if !seen[c.url] {
			best.ImageCandidates = append(best.ImageCandidates, c.url)
			seen[c.url] = true
		}
	}

	for _, r := range sorted[1:] {
		if best.Plot == "" && r.Plot != "" {
			best.Plot = r.Plot
		}
		if best.OriginalTitle == "" && r.OriginalTitle != "" {
			best.OriginalTitle = r.OriginalTitle
		}
		if best.Fanart == "" && r.Fanart != "" {
			best.Fanart = r.Fanart
		}
		if best.Director == "" && r.Director != "" {
			best.Director = r.Director
		}
		if best.Studio == "" && r.Studio != "" {
			best.Studio = r.Studio
		}
		if best.Label == "" && r.Label != "" {
			best.Label = r.Label
		}
		if best.Series == "" && r.Series != "" {
			best.Series = r.Series
		}
		if best.Rating == 0 && r.Rating > 0 {
			best.Rating = r.Rating
		}
		if best.Votes == 0 && r.Votes > 0 {
			best.Votes = r.Votes
		}
		if best.Runtime == 0 && r.Runtime > 0 {
			best.Runtime = r.Runtime
		}
		if best.ReleaseDate == "" && r.ReleaseDate != "" {
			best.ReleaseDate = r.ReleaseDate
		}
		if best.Trailer == "" && r.Trailer != "" {
			best.Trailer = r.Trailer
		}

		for _, img := range r.PreviewImages {
			exists := false
			for _, bi := range best.PreviewImages {
				if bi == img {
					exists = true
					break
				}
			}
			if !exists {
				best.PreviewImages = append(best.PreviewImages, img)
			}
		}

		for _, a := range r.Actors {
			found := false
			for i := range best.Actors {
				if aliasMatch(best.Actors[i], a) {
					if best.Actors[i].Image == "" && a.Image != "" {
						best.Actors[i].Image = a.Image
					}
					if best.Actors[i].Birthday == "" && a.Birthday != "" {
						best.Actors[i].Birthday = a.Birthday
					}
					if best.Actors[i].Country == "" && a.Country != "" {
						best.Actors[i].Country = a.Country
					}
					if best.Actors[i].Height == 0 && a.Height > 0 {
						best.Actors[i].Height = a.Height
					}
					for _, al := range a.Aliases {
						exists := false
						for _, bal := range best.Actors[i].Aliases {
							if bal == al {
								exists = true
								break
							}
						}
						if !exists {
							best.Actors[i].Aliases = append(best.Actors[i].Aliases, al)
						}
					}
					found = true
					break
				}
			}
			if !found {
				best.Actors = append(best.Actors, a)
			}
		}

		for _, g := range r.Genres {
			exists := false
			for _, bg := range best.Genres {
				if normalizeTag(bg) == normalizeTag(g) {
					exists = true
					break
				}
			}
			if !exists {
				best.Genres = append(best.Genres, g)
			}
		}

		for _, u := range r.Urls {
			exists := false
			for _, bu := range best.Urls {
				if bu == u {
					exists = true
					break
				}
			}
			if !exists {
				best.Urls = append(best.Urls, u)
			}
		}
	}

	if len(best.Actors) >= 2 {
		hasEnsemble := false
		for _, g := range best.Genres {
			if g == "共演" {
				hasEnsemble = true
				break
			}
		}
		if !hasEnsemble {
			best.Genres = append(best.Genres, "共演")
		}
	}

	return &best
}

func aliasMatch(a, b Actor) bool {
	an := normalizeName(a.Name)
	bn := normalizeName(b.Name)

	if an == bn {
		return true
	}
	for _, alias := range a.Aliases {
		if normalizeName(alias) == bn {
			return true
		}
	}
	for _, alias := range b.Aliases {
		if normalizeName(alias) == an {
			return true
		}
	}
	if len(a.Aliases) > 0 && len(b.Aliases) > 0 {
		for _, al := range a.Aliases {
			for _, bl := range b.Aliases {
				if normalizeName(al) == normalizeName(bl) {
					return true
				}
			}
		}
	}
	return false
}

func normalizeName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "　", "")
	s = strings.ReplaceAll(s, " ", "")
	return strings.ToLower(s)
}

func normalizeTag(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "・", "、")
	s = strings.ReplaceAll(s, "·", "、")
	s = strings.ReplaceAll(s, "　", "")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "fourk", "4k")
	return s
}

func sourceImagePriority(source string) int {
	switch {
	case strings.HasPrefix(source, "javstash"):
		return 1
	case strings.Contains(source, "DMM"):
		return 2
	case strings.Contains(source, "FANZA"):
		return 2
	case strings.Contains(source, "JAV321"):
		return 3
	case strings.Contains(source, "JavBus"):
		return 10
	default:
		return 5
	}
}

func sortByChinese(results []*ScrapeResult, preferChinese bool) []*ScrapeResult {
	sorted := make([]*ScrapeResult, len(results))
	copy(sorted, results)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			si := completeness(sorted[i])
			sj := completeness(sorted[j])
			if preferChinese {
				ci := boolToInt(sorted[i].HasChinese)
				cj := boolToInt(sorted[j].HasChinese)
				if cj > ci || (cj == ci && sj > si) {
					sorted[i], sorted[j] = sorted[j], sorted[i]
				}
			} else {
				if sj > si {
					sorted[i], sorted[j] = sorted[j], sorted[i]
				}
			}
		}
	}
	return sorted
}

func completeness(r *ScrapeResult) int {
	s := 0
	if r.Plot != "" {
		s += 10
	}
	if r.Poster != "" {
		s += 3
	}
	if r.Fanart != "" {
		s += 3
	}
	if len(r.PreviewImages) > 0 {
		s += 5
	}
	if r.Director != "" {
		s += 2
	}
	if r.Studio != "" {
		s += 2
	}
	if r.Label != "" {
		s += 1
	}
	if r.Series != "" {
		s += 1
	}
	if r.Rating > 0 {
		s += 2
	}
	if r.Trailer != "" {
		s += 2
	}
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func extractProviderID(source, code string) string {
	if len(source) <= len("metatube:") {
		return ""
	}
	return source[len("metatube:"):] + "/" + code
}

func (s *Service) ListMedia(page, pageSize int) ([]models.AVMedia, int64, error) {
	var list []models.AVMedia
	var total int64
	s.DB.Model(&models.AVMedia{}).Count(&total)
	err := s.DB.Order("created_at desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error
	return list, total, err
}

func (s *Service) GetMedia(id uint) (*models.AVMedia, error) {
	var m models.AVMedia
	err := s.DB.First(&m, id).Error
	return &m, err
}

func (s *Service) ReleaseMedia(id uint) error {
	media := models.GetAVMediaByID(id)
	if media == nil {
		return fmt.Errorf("媒体记录不存在")
	}
	if media.Status != "paused" {
		return fmt.Errorf("当前状态为 %s，不是暂停状态，无法放行", media.Status)
	}
	if err := s.DB.Model(media).Updates(map[string]any{
		"status":       "released",
		"pause_reason": "",
	}).Error; err != nil {
		return err
	}
	helpers.AppLogger.Infof("[AV放行] %s 已放行，下次扫描将强制走完", media.Code)
	return nil
}

func (s *Service) RestartMedia(id uint) error {
	media := models.GetAVMediaByID(id)
	if media == nil {
		return fmt.Errorf("媒体记录不存在")
	}
	tmpDir := filepath.Join(helpers.ConfigDir, "tmp", "avscrape", media.Code)
	if err := os.RemoveAll(tmpDir); err != nil {
		helpers.AppLogger.Warnf("[AV重启] 清理 tmp 失败 %s: %v", tmpDir, err)
	}
	imgDir := filepath.Join(helpers.ConfigDir, "avscrape_images", media.Code)
	_ = os.RemoveAll(imgDir)
	if err := s.DB.Delete(&models.AVMedia{}, id).Error; err != nil {
		return err
	}
	s.DB.Where("media_id = ?", id).Delete(&models.AVTask{})
	helpers.AppLogger.Infof("[AV重启] %s 已重启", media.Code)
	return nil
}

func (s *Service) CancelMedia(id uint) error {
	media := models.GetAVMediaByID(id)
	if media == nil {
		return fmt.Errorf("媒体记录不存在")
	}
	tmpDir := filepath.Join(helpers.ConfigDir, "tmp", "avscrape", media.Code)
	if err := os.RemoveAll(tmpDir); err != nil {
		helpers.AppLogger.Warnf("[AV取消] 清理 tmp 失败 %s: %v", tmpDir, err)
	}
	imgDir := filepath.Join(helpers.ConfigDir, "avscrape_images", media.Code)
	_ = os.RemoveAll(imgDir)
	if err := s.DB.Delete(&models.AVMedia{}, id).Error; err != nil {
		return err
	}
	s.DB.Where("media_id = ?", id).Delete(&models.AVTask{})
	helpers.AppLogger.Infof("[AV取消] %s 已取消", media.Code)
	return nil
}

func (s *Service) BatchDeleteMedia(ids []uint) (int, error) {
	var mediaList []models.AVMedia
	query := s.DB.Model(&models.AVMedia{})
	if len(ids) > 0 {
		query = query.Where("id IN ?", ids)
	}
	if err := query.Find(&mediaList).Error; err != nil {
		return 0, err
	}
	if len(mediaList) == 0 {
		return 0, nil
	}

	codes := make([]string, 0, len(mediaList))
	mediaIDs := make([]uint, 0, len(mediaList))
	for _, m := range mediaList {
		codes = append(codes, m.Code)
		mediaIDs = append(mediaIDs, m.ID)
	}

	for _, code := range codes {
		tmpDir := filepath.Join(helpers.ConfigDir, "tmp", "avscrape", code)
		if err := os.RemoveAll(tmpDir); err != nil {
			helpers.AppLogger.Warnf("[AV批量删除] 清理 tmp 失败 %s: %v", tmpDir, err)
		}
		imgDir := filepath.Join(helpers.ConfigDir, "avscrape_images", code)
		if err := os.RemoveAll(imgDir); err != nil {
			helpers.AppLogger.Warnf("[AV批量删除] 清理图片失败 %s: %v", imgDir, err)
		}
	}

	if err := s.DB.Where("id IN ?", mediaIDs).Delete(&models.AVMedia{}).Error; err != nil {
		return 0, err
	}

	s.DB.Where("media_id IN ?", mediaIDs).Delete(&models.AVTask{})

	helpers.AppLogger.Infof("[AV批量删除] 已删除 %d 条媒体记录", len(mediaIDs))
	return len(mediaIDs), nil
}

func dedupParenActors(actors []Actor) []Actor {
	if len(actors) <= 1 {
		return actors
	}

	nameSet := make(map[string]bool)
	for _, a := range actors {
		if a.Name != "" {
			nameSet[normalizeName(a.Name)] = true
		}
		for _, al := range a.Aliases {
			if al != "" {
				nameSet[normalizeName(al)] = true
			}
		}
	}

	out := make([]Actor, 0, len(actors))
	for _, a := range actors {
		if inner := extractParenInner(a.Name); inner != "" {
			if nameSet[normalizeName(inner)] {
				helpers.AppLogger.Infof("[演员去重] 丢弃重复演员: %s（括号内 %s 已存在）", a.Name, inner)
				continue
			}
		}
		out = append(out, a)
	}
	return out
}

func extractParenInner(s string) string {
	if i := strings.Index(s, "（"); i >= 0 {
		if j := strings.LastIndex(s, "）"); j > i {
			return strings.TrimSpace(s[i+len("（"):j])
		}
	}
	if i := strings.Index(s, "("); i >= 0 {
		if j := strings.LastIndex(s, ")"); j > i {
			return strings.TrimSpace(s[i+1 : j])
		}
	}
	return ""
}

func normalizeParenActorNames(actors []Actor) []Actor {
	for i := range actors {
		name := actors[i].Name
		inner := extractParenInner(name)
		if inner == "" {
			continue
		}
		outer := extractParenOuter(name)
		if outer == "" || outer == name {
			continue
		}

		actors[i].Name = outer

		exists := false
		for _, al := range actors[i].Aliases {
			if al == inner {
				exists = true
				break
			}
		}
		if !exists {
			actors[i].Aliases = append(actors[i].Aliases, inner)
		}

		helpers.AppLogger.Infof("[演员名清洗] %s → name=%q, alias+=%q", name, outer, inner)
	}
	return actors
}

func extractParenOuter(s string) string {
	if i := strings.Index(s, "（"); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	if i := strings.Index(s, "("); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}