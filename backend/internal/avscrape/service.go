package avscrape

import (
	"fmt"
	"sort"
	"strings"

	"qmediasync/internal/helpers"
	"qmediasync/internal/models"

	"gorm.io/gorm"
)

type Service struct {
	DB *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	return &Service{DB: db}
}

func (s *Service) Scrape(code string, oshash string) (*ScrapeResult, error) {
	cfg, err := LoadConfig(s.DB)
	if err != nil {
		return nil, err
	}
	var allResults []*ScrapeResult

	// ===== oshash 优先匹配 =====
	if oshash != "" && cfg.EnableOshashMatch && cfg.EnableJavStash {
		js := NewJavStashClient(cfg.JavStashEndpoint, cfg.JavStashAPIKey)
		if r, err := js.SearchByOshash(oshash); err == nil {
			helpers.AppLogger.Infof("[AV刮削] oshash 优先命中: %s => %s", oshash, r.Code)
			allResults = append(allResults, r)
		} else {
			helpers.AppLogger.Infof("[AV刮削] oshash 未命中，回退到番号搜索: %v", err)
		}
	}

	// ===== MetaTube（用 ProviderID 拉详情） =====
	if cfg.EnableMetaTube && cfg.MetaTubeServer != "" {
		mt := NewMetaTubeClient(cfg.MetaTubeServer)
		hits, err := mt.Search(code)
		if err == nil {
			for _, h := range hits {
				providerID := h.ProviderID
				if providerID == "" {
					providerID = extractProviderID(h.Source, h.Code)
				}
				if providerID == "" {
					continue
				}
				detail, err := mt.Detail(code, providerID)
				if err == nil {
					helpers.AppLogger.Infof("[MetaTube] 详情成功: %s => %d 张剧照",
						providerID, len(detail.PreviewImages))
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
	if cfg.EnableJavStash {
		js := NewJavStashClient(cfg.JavStashEndpoint, cfg.JavStashAPIKey)
		hits, err := js.Search(code)
		if err == nil {
			allResults = append(allResults, hits...)
		}
	}

	if len(allResults) == 0 {
		return nil, fmt.Errorf("no result for %s", code)
	}

	// ===== 用 JavStash 的 aliases 归一化演员名 =====
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

	// ===== 合并多源 =====
	best := mergeResults(allResults, cfg)

	// ===== 维基百科补全演员中文名 =====
	wikiClient := NewWikiClient()
	best.Actors = wikiClient.TranslateActorNames(best.Actors)

	// ===== 维基百科补全片商中文名 =====
	if best.Studio != "" {
		// 先试全名
		if zh, err := wikiClient.GetChineseName(best.Studio); err == nil && zh != "" && zh != best.Studio {
			helpers.AppLogger.Infof("[维基] 片商 %s → %s", best.Studio, zh)
			best.Studio = zh
		} else {
			// ===== 改动：用 strings.Fields 按全角/半角空格分词，取第一个词再试 =====
			fields := strings.Fields(best.Studio)
			if len(fields) > 1 {
				shortName := fields[0]
				if zh, err := wikiClient.GetChineseName(shortName); err == nil && zh != "" && zh != shortName {
					helpers.AppLogger.Infof("[维基] 片商 %s → %s (截取 %s)", best.Studio, zh, shortName)
					best.Studio = zh
				} else {
					helpers.AppLogger.Infof("[维基] 片商 %s 未找到中文译名", best.Studio)
				}
			} else {
				helpers.AppLogger.Infof("[维基] 片商 %s 未找到中文译名", best.Studio)
			}
			// ======================================================================
		}
	}

	// ===== JavDB 评分 =====
	if cfg.EnableJavDBRating && cfg.JavDBCookie != "" {
		client := NewJavDBClient(cfg.JavDBCookie)
		if rating, votes, err := client.GetRating(code); err == nil && rating > 0 {
			best.Rating = rating
			best.Votes = votes
			helpers.AppLogger.Infof("[AV刮削] JavDB 评分: %.2f (%d人)", rating, votes)
		} else if err != nil {
			helpers.AppLogger.Warnf("[AV刮削] JavDB 评分获取失败: %v", err)
		}
	}

	// ===== 翻译 =====
	if cfg.EnableTranslate {
		tr := NewTranslator(cfg.TranslateEngine, cfg.TranslateTarget)
		tr.DeepLKey = cfg.TranslateDeepLKey
		tr.BingKey = cfg.TranslateBingKey
		tr.BingRegion = cfg.TranslateBingRegion
		tr.GeminiKey = cfg.TranslateGeminiKey
		tr.GeminiModel = cfg.TranslateGeminiModel
		// ===== Google Cloud Translation =====
		tr.GoogleAPIKey = cfg.GoogleTranslateAPIKey
		tr.GoogleTranslateForTags = cfg.GoogleTranslateForTags
		tr.GoogleTranslateForAll = cfg.GoogleTranslateForAll
		// ====================================
		helpers.AppLogger.Infof("[AV刮削] 开始翻译 %s (engine=%s)", code, cfg.TranslateEngine)
		tr.TranslateResult(best)
	}

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

// buildJavStashAliasMap 从 JavStash 的结果里构建"别名 → 主名"映射
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
