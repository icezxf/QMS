package avscrape

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"qmediasync/internal/helpers"
	"qmediasync/internal/models"

	"gorm.io/gorm"
)

type Service struct {
	DB *gorm.DB
}

var studioSeparatorRe = regexp.MustCompile(`[^\p{Han}\p{Hiragana}\p{Katakana}\p{Latin}\p{N}]+`)

func NewService(db *gorm.DB) *Service {
	return &Service{DB: db}
}

func (s *Service) Scrape(code string, oshash string) (*ScrapeResult, error) {
	cfg, err := LoadConfig(s.DB)
	if err != nil {
		return nil, err
	}
	var allResults []*ScrapeResult

	if oshash != "" && cfg.EnableOshashMatch && cfg.EnableJavStash {
		js := NewJavStashClient(cfg.JavStashEndpoint, cfg.JavStashAPIKey)
		if r, err := js.SearchByOshash(oshash); err == nil {
			helpers.AppLogger.Infof("[AV刮削] oshash 优先命中: %s => %s", oshash, r.Code)
			allResults = append(allResults, r)
		} else {
			helpers.AppLogger.Infof("[AV刮削] oshash 未命中，回退到番号搜索: %v", err)
		}
	}

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

	// 演员名归一化
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

	// ===== 收集警告 =====
	var warnings []string

	// 演员维基翻译
	wikiClient := NewWikiClient()
	translatedActors, actorWarnings := wikiClient.TranslateActorNames(best.Actors)
	best.Actors = translatedActors
	warnings = append(warnings, actorWarnings...)

	// 片商维基翻译
	if best.Studio != "" {
		if zh, err := wikiClient.GetChineseName(best.Studio); err == nil && zh != "" && zh != best.Studio {
			helpers.AppLogger.Infof("[维基] 片商 %s → %s", best.Studio, zh)
			best.Studio = zh
		} else {
			parts := studioSeparatorRe.Split(best.Studio, -1)
			var shortName string
			for _, p := range parts {
				if strings.TrimSpace(p) != "" {
					shortName = strings.TrimSpace(p)
					break
				}
			}
			if shortName != "" && shortName != best.Studio {
				if zh, err := wikiClient.GetChineseName(shortName); err == nil && zh != "" && zh != shortName {
					helpers.AppLogger.Infof("[维基] 片商 %s → %s (截取 %s)", best.Studio, zh, shortName)
					best.Studio = zh
				} else {
					msg := fmt.Sprintf("片商 %s 未找到中文译名", best.Studio)
					helpers.AppLogger.Infof("[维基] %s", msg)
					warnings = append(warnings, msg)
				}
			} else {
				msg := fmt.Sprintf("片商 %s 未找到中文译名", best.Studio)
				helpers.AppLogger.Infof("[维基] %s", msg)
				warnings = append(warnings, msg)
			}
		}
	}

	// JavDB 评分
	if cfg.EnableJavDBRating && cfg.JavDBCookie != "" {
		client := NewJavDBClient(cfg.JavDBCookie)
		if rating, votes, err := client.GetRating(code); err == nil && rating > 0 {
			best.Rating = rating
			best.Votes = votes
			helpers.AppLogger.Infof("[AV刮削] JavDB 评分: %.2f (%d人)", rating, votes)
		} else if err != nil {
			helpers.AppLogger.Warnf("[AV刮削] JavDB 评分获取失败: %v", err)
			warnings = append(warnings, fmt.Sprintf("JavDB 评分获取失败: %v", err))
		}
	}

	// 翻译
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
		helpers.AppLogger.Infof("[AV刮削] 开始翻译 %s (engine=%s)", code, cfg.TranslateEngine)
		transWarnings := tr.TranslateResult(best)
		warnings = append(warnings, transWarnings...)
	}

	best.Warnings = warnings

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

// ===== 暂停操作 =====

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
	if err := s.DB.Delete(&models.AVMedia{}, id).Error; err != nil {
		return err
	}
	s.DB.Where("media_id = ?", id).Delete(&models.AVTask{})
	helpers.AppLogger.Infof("[AV取消] %s 已取消", media.Code)
	return nil
}

// ===== 批量删除媒体 =====

// BatchDeleteMedia 批量删除媒体记录
// ids 为空时删除全部
// 注意：只删数据库记录和本地 tmp，不动源视频、不动已上传的元数据文件
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

	// 清理本地 tmp（每个番号一个目录）
	for _, code := range codes {
		tmpDir := filepath.Join(helpers.ConfigDir, "tmp", "avscrape", code)
		if err := os.RemoveAll(tmpDir); err != nil {
			helpers.AppLogger.Warnf("[AV批量删除] 清理 tmp 失败 %s: %v", tmpDir, err)
		}
	}

	// 删 AVMedia
	if err := s.DB.Where("id IN ?", mediaIDs).Delete(&models.AVMedia{}).Error; err != nil {
		return 0, err
	}

	// 删关联 AVTask
	s.DB.Where("media_id IN ?", mediaIDs).Delete(&models.AVTask{})

	helpers.AppLogger.Infof("[AV批量删除] 已删除 %d 条媒体记录", len(mediaIDs))
	return len(mediaIDs), nil
}
