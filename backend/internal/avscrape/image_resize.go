package avscrape

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math"

	"qmediasync/internal/helpers"
)

// resizePosterToMin 如果 poster 小于 minW x minH，用双线性插值放大到至少 minW x minH
// 返回放大后的数据；没放大返回 (nil, false)
func resizePosterToMin(imgData []byte, minW, minH int) ([]byte, bool) {
	src, _, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		return nil, false
	}
	bounds := src.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if w >= minW && h >= minH {
		return nil, false
	}

	// 等比缩放（取大的比例，保证两个维度都不小于目标）
	scaleW := float64(minW) / float64(w)
	scaleH := float64(minH) / float64(h)
	scale := math.Max(scaleW, scaleH)
	newW := int(float64(w) * scale)
	newH := int(float64(h) * scale)

	// 双线性插值
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	for y := 0; y < newH; y++ {
		for x := 0; x < newW; x++ {
			srcX := float64(x) * float64(w) / float64(newW)
			srcY := float64(y) * float64(h) / float64(newH)

			x0 := int(srcX)
			y0 := int(srcY)
			x1 := x0 + 1
			y1 := y0 + 1
			if x1 >= w {
				x1 = w - 1
			}
			if y1 >= h {
				y1 = h - 1
			}

			fx := srcX - float64(x0)
			fy := srcY - float64(y0)

			c00 := toRGBA(src.At(bounds.Min.X+x0, bounds.Min.Y+y0))
			c01 := toRGBA(src.At(bounds.Min.X+x1, bounds.Min.Y+y0))
			c10 := toRGBA(src.At(bounds.Min.X+x0, bounds.Min.Y+y1))
			c11 := toRGBA(src.At(bounds.Min.X+x1, bounds.Min.Y+y1))

			r := bilinear(c00.R, c01.R, c10.R, c11.R, fx, fy)
			g := bilinear(c00.G, c01.G, c10.G, c11.G, fx, fy)
			b := bilinear(c00.B, c01.B, c10.B, c11.B, fx, fy)
			a := bilinear(c00.A, c01.A, c10.A, c11.A, fx, fy)

			dst.Set(x, y, color.RGBA{R: r, G: g, B: b, A: a})
		}
	}

	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 92}); err != nil {
		return nil, false
	}
	helpers.AppLogger.Infof("[AV海报] poster 放大: %dx%d → %dx%d", w, h, newW, newH)
	return out.Bytes(), true
}

func toRGBA(c color.Color) color.RGBA {
	r, g, b, a := c.RGBA()
	return color.RGBA{
		R: uint8(r >> 8),
		G: uint8(g >> 8),
		B: uint8(b >> 8),
		A: uint8(a >> 8),
	}
}

func bilinear(c00, c01, c10, c11 uint8, fx, fy float64) uint8 {
	v0 := float64(c00)*(1-fx) + float64(c01)*fx
	v1 := float64(c10)*(1-fx) + float64(c11)*fx
	v := v0*(1-fy) + v1*fy
	if v < 0 {
		v = 0
	}
	if v > 255 {
		v = 255
	}
	return uint8(v)
}
