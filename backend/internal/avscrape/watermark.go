package avscrape

import (
	"bytes"
	"embed"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"

	"qmediasync/internal/helpers"
)

//go:embed 4k.png 5k.png 6k.png 7k.png 8k.png 字幕.png 无码.png 流出.png 破解.png
var watermarkFS embed.FS

type WatermarkItem struct {
	PngName string
	Label   string
}

func buildWatermarks(r *ScrapeResult, cfg *Config) []WatermarkItem {
	var items []WatermarkItem

	allTags := append([]string{}, r.Genres...)
	allTags = append(allTags, r.ExtraTags...)
	joined := toLower(strings_Join(allTags, ","))

	// 分辨率水印：从高到低，只打一个
	if cfg.Watermark8K && (contains(joined, "8k") || r.Resolution == "8K") {
		items = append(items, WatermarkItem{PngName: "8k.png", Label: "8K"})
	} else if cfg.Watermark7K && (contains(joined, "7k") || r.Resolution == "7K") {
		items = append(items, WatermarkItem{PngName: "7k.png", Label: "7K"})
	} else if cfg.Watermark6K && (contains(joined, "6k") || r.Resolution == "6K") {
		items = append(items, WatermarkItem{PngName: "6k.png", Label: "6K"})
	} else if cfg.Watermark5K && (contains(joined, "5k") || r.Resolution == "5K") {
		items = append(items, WatermarkItem{PngName: "5k.png", Label: "5K"})
	} else if cfg.Watermark4K && (contains(joined, "4k") || r.Resolution == "4K") {
		items = append(items, WatermarkItem{PngName: "4k.png", Label: "4K"})
	}

	if cfg.WatermarkSubtitle && (r.HasChineseSub || contains(joined, "字幕") || contains(joined, "中字")) {
		items = append(items, WatermarkItem{PngName: "字幕.png", Label: "字幕"})
	}
	if cfg.WatermarkCrack && contains(joined, "破解") {
		items = append(items, WatermarkItem{PngName: "破解.png", Label: "破解"})
	}
	if cfg.WatermarkLeak && contains(joined, "流出") {
		items = append(items, WatermarkItem{PngName: "流出.png", Label: "流出"})
	}
	if cfg.WatermarkUncensored && (r.IsUncensored || contains(joined, "无码")) {
		items = append(items, WatermarkItem{PngName: "无码.png", Label: "无码"})
	}
	return items
}

func applyWatermark(imgData []byte, items []WatermarkItem) ([]byte, error) {
	if len(items) == 0 {
		return imgData, nil
	}

	src, _, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		return imgData, err
	}
	bounds := src.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), src, bounds.Min, draw.Src)

	padX := w * 15 / 1000
	padY := h * 15 / 1000
	curX := padX
	curY := padY

	for _, item := range items {
		pngData, err := watermarkFS.ReadFile(item.PngName)
		if err != nil {
			helpers.AppLogger.Warnf("[AV水印] 读取水印图失败: %s => %v", item.PngName, err)
			continue
		}
		wmImg, err := png.Decode(bytes.NewReader(pngData))
		if err != nil {
			helpers.AppLogger.Warnf("[AV水印] 解码水印图失败: %s => %v", item.PngName, err)
			continue
		}

		wmW := wmImg.Bounds().Dx()
		wmH := wmImg.Bounds().Dy()

		// ===== 水印宽度 = 图片宽度的 15%（原来 8%，太小）=====
		targetW := w * 15 / 100
		if targetW < 120 {
			targetW = 120
		}
		// =====================================================

		scale := float64(targetW) / float64(wmW)
		targetH := int(float64(wmH) * scale)

		scaled := resizeImage(wmImg, targetW, targetH)
		draw.Draw(dst, image.Rect(curX, curY, curX+targetW, curY+targetH), scaled, image.Point{}, draw.Over)

		curX += targetW + padX/2
		if curX+targetW > w-padX {
			curX = padX
			curY += targetH + padY/2
		}
	}

	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 92}); err != nil {
		return imgData, err
	}
	return out.Bytes(), nil
}

func resizeImage(src image.Image, newW, newH int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	srcBounds := src.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()
	for y := 0; y < newH; y++ {
		for x := 0; x < newW; x++ {
			srcX := srcBounds.Min.X + x*srcW/newW
			srcY := srcBounds.Min.Y + y*srcH/newH
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}
	return dst
}

func strings_Join(arr []string, sep string) string {
	if len(arr) == 0 {
		return ""
	}
	var b []byte
	for i, s := range arr {
		if i > 0 {
			b = append(b, sep...)
		}
		b = append(b, s...)
	}
	return string(b)
}

func toLower(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c = c + 32
		}
		out = append(out, c)
	}
	return string(out)
}

func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ApplyWatermark 给单张图片打水印，供外部包（如 aven）调用。
func ApplyWatermark(imgData []byte, items []WatermarkItem) ([]byte, error) {
	return applyWatermark(imgData, items)
}