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
// 图片裁剪：从横版图裁出 2:3 竖版（仅作兜底）
// ============================================================

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

	targetW := imgH * 2 / 3
	if targetW > imgW {
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

	dst := image.NewRGBA(image.Rect(0, 0, targetW, imgH))
	draw.Draw(dst, dst.Bounds(), img, image.Point{0, 0}, draw.Src)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 92}); err != nil {
		return nil, false
	}
	return out.Bytes(), true
}

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
// 图片来源：
//   poster → r.Poster（由 service_en.go 从 TPDB posters.full 填充，800x1200 竖版）
//   fanart → r.ImageCandidates 里挑横版（由 service_en.go 从 StashDB images 填充）
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

	// ===== 1. poster：优先用 TPDB posters.full（已是 800x1200 竖版）=====
	var posterData, fanartData []byte

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
		helpers.AppLogger.Infof("[欧美元数据] r.Poster 为空，poster 将走兜底")
	}

	// ===== 2. fanart：从 StashDB images 里挑横版（width > height）=====
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
		helpers.AppLogger.Infof("[欧美元数据]   [%d] %dx%d (%s)", i, imgCfg.Width, imgCfg.Height, redactURL(url))
		if imgCfg.Width > imgCfg.Height {
			fanartData = data
			r.Fanart = url
			helpers.AppLogger.Infof("[欧美元数据]   → 选为 fanart（横版 %dx%d）", imgCfg.Width, imgCfg.Height)
			break
		}
	}

	// ===== 3. poster 兜底：从候选图里裁 =====
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