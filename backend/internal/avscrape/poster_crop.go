package avscrape

import (
	"bytes"
	"image"
	"image/draw"
	"image/jpeg"

	"Q115-STRM/internal/helpers"
)

// cropPosterFromFanart 从横版图右侧裁剪出 2:3 的竖版海报
// 大部分 JAV 源的横版图上，封面位于右侧
func cropPosterFromFanart(fanartData []byte) ([]byte, bool) {
	src, _, err := image.Decode(bytes.NewReader(fanartData))
	if err != nil {
		helpers.AppLogger.Warnf("[AV海报] 解码 fanart 失败: %v", err)
		return nil, false
	}

	bounds := src.Bounds()
	imgW := bounds.Dx()
	imgH := bounds.Dy()

	// 期望竖版宽高比 2:3
	targetW := imgH * 2 / 3
	if targetW >= imgW {
		helpers.AppLogger.Warnf("[AV海报] fanart %dx%d 太窄，无法裁出竖版", imgW, imgH)
		return nil, false
	}

	// 从右侧裁剪
	x0 := imgW - targetW
	y0 := 0

	cropRect := image.Rect(x0, y0, x0+targetW, y0+imgH)
	dst := image.NewRGBA(image.Rect(0, 0, targetW, imgH))
	draw.Draw(dst, dst.Bounds(), src, cropRect.Min, draw.Src)

	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 92}); err != nil {
		return nil, false
	}
	helpers.AppLogger.Infof("[AV海报] 从 fanart 右侧裁剪出竖版: %dx%d", targetW, imgH)
	return out.Bytes(), true
}
