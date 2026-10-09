package aven

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"qmediasync/internal/avscrape"
	"qmediasync/internal/helpers"
	"qmediasync/internal/models"
)

// ============================================================
// 图片下载
// ============================================================

var httpClientForImage = &http.Client{Timeout: 30 * time.Second}

func downloadImage(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "image/*")

	resp, err := httpClientForImage.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func redactURL(raw string) string {
	if len(raw) > 200 {
		return raw[:200] + "..."
	}
	return raw
}

// ============================================================
// 图片裁剪：从横版图裁出 2:3 竖版（仅作 poster 兜底）
// ============================================================

// cropToPoster 从横版图（或任意图）裁出 2:3 竖版
// 策略：从左侧裁（欧美截图通常是左侧主体）
func cropToPoster(src []byte) ([]byte, bool) {
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, false
	}
	bounds := img.Bounds()
	imgW := bounds.Dx()
	imgH := bounds.Dy()
	if imgW < 2 || imgH < 2 {
		return nil, false
	}

	// 目标：2:3 竖版，高不变，宽 = 高 * 2/3
	targetW := imgH * 2 / 3
	if targetW > imgW {
		// 图比 2:3 还窄，反过来用高度裁
		targetH := imgW * 3 / 2
		if targetH > imgH {
			return src, false
		}
		dst := image.NewRGBA(image.Rect(0, 0, imgW, targetH))
		draw.Draw(dst, dst.Bounds(), img, image.Point{0, 0}, draw.Src)
		var out bytes.Buffer
		if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 92}); err != nil {
			return nil, false
		}
		return out.Bytes(), true
	}

	// 从左侧裁 targetW 宽
	dst := image.NewRGBA(image.Rect(0, 0, targetW, imgH))
	draw.Draw(dst, dst.Bounds(), img, image.Point{0, 0}, draw.Src)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 92}); err != nil {
		return nil, false
	}
	return out.Bytes(), true
}

// resizeToMin 如果小于 minW x minH，等比放大
func resizeToMin(src []byte, minW, minH int) ([]byte, bool) {
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, false
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w >= minW && h >= minH {
		return nil, false
	}
	scaleW := float64(minW) / float64(w)
	scaleH := float64(minH) / float64(h)
	scale := scaleW
	if scaleH > scale {
		scale = scaleH
	}
	newW := int(float64(w) * scale)
	newH := int(float64(h) * scale)

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	for y := 0; y < newH; y++ {
		for x := 0; x < newW; x++ {
			srcX := x * w / newW
			srcY := y * h / newH
			dst.Set(x, y, img.At(bounds.Min.X+srcX, bounds.Min.Y+srcY))
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 92}); err != nil {
		return nil, false
	}
	return out.Bytes(), true
}

// ============================================================
// 生成元数据文件
// ============================================================

// PrepareMetaFilesN 生成欧美元数据文件（NFO + poster + fanart + 剧照）
//
// 图片来源（由 service_en.go 提前填好）：
//   poster → r.Poster，来自 TPDB posters.full（800x1200 竖版，直接下）
//   fanart → r.ImageCandidates（StashDB images），挑 width > height 的第一张
//
// 兜底策略：
//   poster 为空 → 从候选图里裁 2:3
//   fanart 为空 → 用第一张候选图（并打 warning）
func PrepareMetaFilesN(baseName string, r *avscrape.ScrapeResult, cfg *Config) ([]avscrape.LocalFile, []string, error) {
	var warnings []string
	if r == nil {
		return nil, warnings, nil
	}

	tmpDir := filepath.Join(helpers.ConfigDir, "tmp", "aven", baseName)
	os.RemoveAll(tmpDir)
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return nil, warnings, fmt.Errorf("创建临时目录失败: %w", err)
	}

	files := []avscrape.LocalFile{}
	var posterData, fanartData []byte

	// ===== 1. poster：TPDB posters.full（已是 800x1200 竖版）=====
	if r.Poster != "" {
		data, err := downloadImage(r.Poster)
		if err != nil {
			helpers.AppLogger.Warnf("[欧美元数据] poster 下载失败: %s => %v", redactURL(r.Poster), err)
			warnings = append(warnings, fmt.Sprintf("poster 下载失败: %v", err))
		} else {
			imgCfg, _, _ := image.DecodeConfig(bytes.NewReader(data))
			helpers.AppLogger.Infof("[欧美元数据] poster 来自 TPDB: %dx%d (%s)",
				imgCfg.Width, imgCfg.Height, redactURL(r.Poster))
			posterData = data
		}
	} else {
		helpers.AppLogger.Infof("[欧美元数据] r.Poster 为空，poster 走兜底")
	}

	// ===== 2. fanart：从 StashDB 候选图里挑横版（width > height）=====
	candidates := r.ImageCandidates
	helpers.AppLogger.Infof("[欧美元数据] fanart 候选共 %d 张", len(candidates))
	for i, url := range candidates {
		data, err := downloadImage(url)
		if err != nil {
			helpers.AppLogger.Warnf("[欧美元数据]   [%d] 下载失败: %s => %v", i, redactURL(url), err)
			continue
		}
		imgCfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			continue
		}
		helpers.AppLogger.Infof("[欧美元数据]   [%d] %dx%d (%s)",
			i, imgCfg.Width, imgCfg.Height, redactURL(url))
		if imgCfg.Width > imgCfg.Height {
			fanartData = data
			r.Fanart = url
			helpers.AppLogger.Infof("[欧美元数据]   → 选为 fanart（横版 %dx%d）", imgCfg.Width, imgCfg.Height)
			break
		}
	}

	// ===== 3. poster 兜底：从第一张候选裁出 2:3 =====
	if posterData == nil && len(candidates) > 0 {
		if data, err := downloadImage(candidates[0]); err == nil {
			if cropped, ok := cropToPoster(data); ok {
				posterData = cropped
				helpers.AppLogger.Infof("[欧美元数据] poster 兜底：从候选图裁出")
			}
		}
	}

	// ===== 4. fanart 兜底：用第一张候选 =====
	if fanartData == nil && len(candidates) > 0 {
		if data, err := downloadImage(candidates[0]); err == nil {
			fanartData = data
			r.Fanart = candidates[0]
			helpers.AppLogger.Infof("[欧美元数据] fanart 兜底：用第一张候选")
			warnings = append(warnings, "fanart 未找到横版图，使用兜底")
		}
	}

	// 放大 poster 到 500x750
	if posterData != nil {
		if resized, ok := resizeToMin(posterData, 500, 750); ok {
			posterData = resized
		}
	}

	// ===== 打水印 =====
	wmItems := buildWatermarksN(r, cfg)
	if len(wmItems) > 0 {
		helpers.AppLogger.Infof("[欧美水印] 需要打水印 %d 个", len(wmItems))
		if posterData != nil {
			if wm, err := avscrape.ApplyWatermark(posterData, wmItems); err == nil {
				posterData = wm
				helpers.AppLogger.Infof("[欧美水印] poster 水印完成")
			} else {
				warnings = append(warnings, fmt.Sprintf("poster 水印失败: %v", err))
			}
		}
		if fanartData != nil {
			if wm, err := avscrape.ApplyWatermark(fanartData, wmItems); err == nil {
				fanartData = wm
				helpers.AppLogger.Infof("[欧美水印] fanart 水印完成")
			} else {
				warnings = append(warnings, fmt.Sprintf("fanart 水印失败: %v", err))
			}
		}
	}

	// ===== 写 poster =====
	if posterData != nil {
		p := filepath.Join(tmpDir, "poster.jpg")
		if err := os.WriteFile(p, posterData, 0644); err == nil {
			files = append(files, avscrape.LocalFile{LocalPath: p, RemoteName: "poster.jpg"})
			r.Poster = "poster.jpg"
		}
	} else {
		r.Poster = ""
		warnings = append(warnings, "poster 未生成")
	}

	// ===== 写 fanart + thumb =====
	if fanartData != nil {
		p := filepath.Join(tmpDir, "fanart.jpg")
		if err := os.WriteFile(p, fanartData, 0644); err == nil {
			files = append(files, avscrape.LocalFile{LocalPath: p, RemoteName: "fanart.jpg"})
			r.Fanart = "fanart.jpg"
		}
		p2 := filepath.Join(tmpDir, "thumb.jpg")
		if err := os.WriteFile(p2, fanartData, 0644); err == nil {
			files = append(files, avscrape.LocalFile{LocalPath: p2, RemoteName: "thumb.jpg"})
		}
	} else {
		r.Fanart = ""
		warnings = append(warnings, "fanart 未生成")
	}

	// ===== 剧照 =====
	successCount := 0
	failCount := 0
	for i, url := range r.PreviewImages {
		remoteName := fmt.Sprintf("extrafanart/fanart%d.jpg", i+1)
		localPath := filepath.Join(tmpDir, fmt.Sprintf("fanart%d.jpg", i+1))
		if err := helpers.DownloadFile(url, localPath, ""); err == nil {
			files = append(files, avscrape.LocalFile{LocalPath: localPath, RemoteName: remoteName})
			successCount++
		} else {
			failCount++
		}
	}
	if failCount > 0 {
		warnings = append(warnings, fmt.Sprintf("剧照部分下载失败: 成功 %d, 失败 %d", successCount, failCount))
	}

	// ===== 预告片（如果 urls 里有 mp4 链接）=====
	if r.Trailer != "" {
		p := filepath.Join(tmpDir, "trailer.strm")
		if err := os.WriteFile(p, []byte(r.Trailer), 0644); err == nil {
			files = append(files, avscrape.LocalFile{LocalPath: p, RemoteName: "trailers/trailer.strm"})
		}
	}

	// ===== NFO =====
	nfoPath := filepath.Join(tmpDir, baseName+".nfo")
	if err := os.WriteFile(nfoPath, []byte(GenerateNFON(r)), 0644); err != nil {
		return nil, warnings, fmt.Errorf("写 NFO 失败: %w", err)
	}
	files = append(files, avscrape.LocalFile{LocalPath: nfoPath, RemoteName: baseName + ".nfo"})

	return files, warnings, nil
}

// ============================================================
// 整理（移动视频文件）
// ============================================================

// OrganizeN 移动视频到目标目录
func OrganizeN(fs avscrape.FileSystem, path *models.AVENPath, media *models.AVENMedia, videoPaths []string, r *avscrape.ScrapeResult) (string, error) {
	relDir := renderTemplateN(path.NameTemplate, media)
	if relDir == "" {
		relDir = media.Title
	}
	targetDir := strings.TrimRight(path.TargetPath, "/") + "/" + relDir
	if err := fs.MkdirAll(targetDir); err != nil {
		return "", err
	}
	helpers.AppLogger.Infof("[欧美整理] 目标目录: %s", targetDir)

	resSuffix := ""
	if r != nil {
		resSuffix = resolutionSuffix(r.Resolution)
	}

	for _, srcPath := range videoPaths {
		originalName := filepath.Base(srcPath)
		ext := filepath.Ext(originalName)
		newName := media.Title + resSuffix + ext
		newPath := targetDir + "/" + newName

		if fs.Exists(newPath) {
			helpers.AppLogger.Infof("[欧美整理] 目标已存在，跳过: %s", newName)
			continue
		}

		switch path.MoveMethod {
		case "copy":
			if err := fs.Copy(srcPath, targetDir); err != nil {
				helpers.AppLogger.Warnf("[欧美整理] 复制失败 %s: %v", srcPath, err)
				continue
			}
			if originalName != newName {
				oldPath := targetDir + "/" + originalName
				if err := fs.Rename(oldPath, newName); err != nil {
					helpers.AppLogger.Warnf("[欧美整理] 重命名失败 %s: %v", oldPath, err)
				}
			}
		default:
			if err := fs.Move(srcPath, targetDir, newName); err != nil {
				helpers.AppLogger.Warnf("[欧美整理] 移动失败 %s: %v", srcPath, err)
				continue
			}
		}
		helpers.AppLogger.Infof("[欧美整理] %s → %s", originalName, newName)
	}

	return targetDir, nil
}

// ============================================================
// 模板渲染（欧美：{actor} / {studio} / {title} / {year} / {code}）
// ============================================================

func renderTemplateN(tpl string, media *models.AVENMedia) string {
	if tpl == "" {
		return defaultNameTemplateN(media)
	}

	var actors []avscrape.Actor
	if media.Actors != "" {
		_ = json.Unmarshal([]byte(media.Actors), &actors)
	}

	names := make([]string, 0, len(actors))
	for _, a := range actors {
		if a.Name != "" {
			names = append(names, a.Name)
		}
	}

	actorDir := ""
	switch {
	case len(names) == 0:
		actorDir = "Unknown"
	case len(names) <= 3:
		actorDir = strings.Join(names, ",")
	default:
		actorDir = "多人作品"
	}
	allActors := strings.Join(names, ", ")
	allActorsPath := sanitizePathN(allActors)
	if len(names) >= 2 {
		allActorsPath = "多人作品/" + sanitizePathN(allActors)
	}

	replacer := strings.NewReplacer(
		"{actor}", sanitizePathN(actorDir),
		"{actors}", allActorsPath,
		"{title}", sanitizePathN(media.Title),
		"{year}", extractYearN(media.ReleaseDate),
		"{studio}", sanitizePathN(media.Studio),
		"{series}", sanitizePathN(media.Series),
		"{code}", sanitizePathN(media.StashID),
	)

	result := replacer.Replace(tpl)
	result = strings.Trim(result, "/")
	for strings.Contains(result, "//") {
		result = strings.ReplaceAll(result, "//", "/")
	}
	parts := strings.Split(result, "/")
	cleaned := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	return strings.Join(cleaned, "/")
}

func defaultNameTemplateN(media *models.AVENMedia) string {
	var actors []avscrape.Actor
	if media.Actors != "" {
		_ = json.Unmarshal([]byte(media.Actors), &actors)
	}
	names := make([]string, 0, len(actors))
	for _, a := range actors {
		if a.Name != "" {
			names = append(names, a.Name)
		}
	}
	actorDir := "Unknown"
	if len(names) == 1 {
		actorDir = names[0]
	} else if len(names) >= 2 && len(names) <= 3 {
		actorDir = strings.Join(names, ",")
	} else if len(names) > 3 {
		actorDir = "多人作品"
	}
	return sanitizePathN(actorDir) + "/" + sanitizePathN(media.Title)
}

func sanitizePathN(s string) string {
	if s == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "：", "*", "_",
		"?", "？", "\"", "'", "<", "《", ">", "》", "|", "_",
	)
	return strings.TrimSpace(replacer.Replace(s))
}

func extractYearN(date string) string {
	if len(date) >= 4 {
		return date[:4]
	}
	return ""
}

func resolutionSuffix(res string) string {
	switch res {
	case "8K":
		return "-8K"
	case "7K":
		return "-7K"
	case "6K":
		return "-6K"
	case "5K":
		return "-5K"
	case "4K":
		return "-4K"
	}
	return ""
}

// ============================================================
// 水印判定（欧美版，复用 avscrape.ApplyWatermark）
// ============================================================

// buildWatermarksN 欧美水印判定（逻辑与日本 AV 一致，但用 aven.Config）
func buildWatermarksN(r *avscrape.ScrapeResult, cfg *Config) []avscrape.WatermarkItem {
	var items []avscrape.WatermarkItem

	allTags := append([]string{}, r.Genres...)
	allTags = append(allTags, r.ExtraTags...)
	joined := strings.ToLower(strings.Join(allTags, ","))

	if cfg.Watermark8K && (strings.Contains(joined, "8k") || r.Resolution == "8K") {
		items = append(items, avscrape.WatermarkItem{PngName: "8k.png", Label: "8K"})
	} else if cfg.Watermark7K && (strings.Contains(joined, "7k") || r.Resolution == "7K") {
		items = append(items, avscrape.WatermarkItem{PngName: "7k.png", Label: "7K"})
	} else if cfg.Watermark6K && (strings.Contains(joined, "6k") || r.Resolution == "6K") {
		items = append(items, avscrape.WatermarkItem{PngName: "6k.png", Label: "6K"})
	} else if cfg.Watermark5K && (strings.Contains(joined, "5k") || r.Resolution == "5K") {
		items = append(items, avscrape.WatermarkItem{PngName: "5k.png", Label: "5K"})
	} else if cfg.Watermark4K && (strings.Contains(joined, "4k") || r.Resolution == "4K") {
		items = append(items, avscrape.WatermarkItem{PngName: "4k.png", Label: "4K"})
	}

	if cfg.WatermarkSubtitle && (r.HasChineseSub || strings.Contains(joined, "字幕") || strings.Contains(joined, "中字")) {
		items = append(items, avscrape.WatermarkItem{PngName: "字幕.png", Label: "字幕"})
	}
	if cfg.WatermarkCrack && strings.Contains(joined, "破解") {
		items = append(items, avscrape.WatermarkItem{PngName: "破解.png", Label: "破解"})
	}
	if cfg.WatermarkLeak && strings.Contains(joined, "流出") {
		items = append(items, avscrape.WatermarkItem{PngName: "流出.png", Label: "流出"})
	}
	if cfg.WatermarkUncensored && (r.IsUncensored || strings.Contains(joined, "无码")) {
		items = append(items, avscrape.WatermarkItem{PngName: "无码.png", Label: "无码"})
	}
	return items
}