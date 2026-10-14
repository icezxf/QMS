package aven

import (
	"strings"

	"qmediasync/internal/avscrape"
)

// SceneToResult 把 StashDB Scene 转成 avscrape.ScrapeResult
// 这样能直接复用日本 AV 的 GenerateNFO / prepareMetaFiles / organize 等
func SceneToResult(scene *StashScene) *avscrape.ScrapeResult {
	if scene == nil {
		return nil
	}

	// ===== 生成欧美"番号" =====
	// 优先级：
	//  1. scene.Code（StashDB 的 code 字段，有值就用）
	//  2. Studio + Date 组合（如 Blacked-20240115）
	//  3. scene.ID（UUID 兜底）
	code := ""
if scene.Code != nil {
    code = strings.TrimSpace(*scene.Code)
}
	if code == "" {
		var parts []string
		if scene.Studio != nil && scene.Studio.Name != "" {
			parts = append(parts, scene.Studio.Name)
		}
		if scene.Date != "" {
			parts = append(parts, strings.ReplaceAll(scene.Date, "-", ""))
		}
		if len(parts) > 0 {
			code = strings.Join(parts, "-")
		} else {
			code = scene.ID
		}
	}

	r := &avscrape.ScrapeResult{
		Code:          code,
		Title:         scene.Title,
		OriginalTitle: scene.Title,
		Plot:          scene.Details,
		PlotOriginal:  scene.Details,
		ReleaseDate:   scene.Date,
		Runtime:       scene.Duration / 60,
		Source:        "stashdb",
	}

	if scene.Studio != nil {
		r.Studio = scene.Studio.Name
	}

	// ===== 演员：过滤男优 =====
	for _, p := range scene.Performers {
		performer := p.Performer
		if strings.ToUpper(performer.Gender) == "MALE" {
			continue
		}

		actor := avscrape.Actor{
			Name:    performer.Name,
			Aliases: performer.Aliases,
			Gender:  performer.Gender,
		}

		if len(performer.Images) > 0 {
			actor.Image = performer.Images[0].URL
		}

		r.Actors = append(r.Actors, actor)
	}

	// ===== 标签 =====
	for _, t := range scene.Tags {
		if t.Name != "" {
			r.Genres = append(r.Genres, t.Name)
		}
	}

	// ===== 图片 =====
	for _, img := range scene.Images {
		r.ImageCandidates = append(r.ImageCandidates, img.URL)
	}

	if len(scene.Images) > 1 {
		for _, img := range scene.Images[1:] {
			r.PreviewImages = append(r.PreviewImages, img.URL)
		}
	}

	// ===== 原始链接 =====
	for _, u := range scene.Urls {
		r.Urls = append(r.Urls, u.URL)
	}

	r.Rating = 0
	r.Trailer = ""

	return r
}
