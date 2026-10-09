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

	r := &avscrape.ScrapeResult{
		Code:          scene.ID, // StashDB scene UUID 作为 code
		Title:         scene.Title,
		OriginalTitle: scene.Title,
		Plot:          scene.Details,
		PlotOriginal:  scene.Details, // 保存原文
		ReleaseDate:   scene.Date,
		Runtime:       scene.Duration / 60,
		Source:        "stashdb",
	}

	if scene.Studio != nil {
		r.Studio = scene.Studio.Name
		// 片商 logo 作为 fanart 候选
		if len(scene.Studio.Images) > 0 {
			r.ImageCandidates = append(r.ImageCandidates, scene.Studio.Images[0].URL)
		}
	}

	// ===== 演员：过滤男优 =====
	for _, p := range scene.Performers {
		performer := p.Performer
		// 跳过男优
		if strings.ToUpper(performer.Gender) == "MALE" {
			continue
		}

		actor := avscrape.Actor{
			Name:    performer.Name,
			Aliases: performer.Aliases,
			Gender:  performer.Gender,
		}

		// 演员头像：取第一张图
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

	// ===== 图片：Scene.images 作为 ImageCandidates =====
	// StashDB 的 scene.images 通常是横版截图（剧照/封面）
	for _, img := range scene.Images {
		r.ImageCandidates = append(r.ImageCandidates, img.URL)
	}

	// PreviewImages（剧照）：从 images[1:] 取
	if len(scene.Images) > 1 {
		for _, img := range scene.Images[1:] {
			r.PreviewImages = append(r.PreviewImages, img.URL)
		}
	}

	// ===== 原始链接 =====
	for _, u := range scene.Urls {
		r.Urls = append(r.Urls, u.URL)
	}

	// ===== 评分：StashDB Scene 层没有 rating，留空 =====
	r.Rating = 0

	// ===== 预告片：StashDB 没有独立字段，留空 =====
	r.Trailer = ""

	return r
}
