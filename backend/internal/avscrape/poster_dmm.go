package avscrape

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"io"
	"net/http"
	"strings"
	"time"

	"qmediasync/internal/helpers"
)

// dmmPosterURL 根据番号构造 DMM 大竖图 URL（jp.jpg）
// 规则：字母部分小写 + 数字部分补零到 5 位 + jp.jpg
// 例：MIDV-192 → midv00192jp.jpg
//
// DMM 图片类型：
//   jp.jpg = 大竖版封面（1000x1500+），最优先
//   ps.jpg = 小缩略图（147x200），太小
//   pl.jpg = 横版拼贴图（800x538），左边是竖版封面，右边是剧照
func dmmPosterURL(code string) string {
	var sb strings.Builder
	for _, r := range code {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		}
	}
	cleaned := strings.ToUpper(sb.String())
	i := 0
	for i < len(cleaned) && cleaned[i] >= 'A' && cleaned[i] <= 'Z' {
		i++
	}
	letters := cleaned[:i]
	digits := cleaned[i:]
	if letters == "" || digits == "" || len(digits) > 5 {
		return ""
	}
	var num int
	if _, err := fmt.Sscanf(digits, "%d", &num); err != nil {
		return ""
	}
	filename := strings.ToLower(letters) + fmt.Sprintf("%05d", num)
	return "https://pics.dmm.co.jp/digital/video/" + filename + "/" + filename + "jp.jpg"
}

// dmmPlURL 构造 DMM 横版拼贴图 URL（pl.jpg），作为 jp.jpg 的兜底
func dmmPlURL(code string) string {
	var sb strings.Builder
	for _, r := range code {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		}
	}
	cleaned := strings.ToUpper(sb.String())
	i := 0
	for i < len(cleaned) && cleaned[i] >= 'A' && cleaned[i] <= 'Z' {
		i++
	}
	letters := cleaned[:i]
	digits := cleaned[i:]
	if letters == "" || digits == "" || len(digits) > 5 {
		return ""
	}
	var num int
	if _, err := fmt.Sscanf(digits, "%d", &num); err != nil {
		return ""
	}
	filename := strings.ToLower(letters) + fmt.Sprintf("%05d", num)
	return "https://pics.dmm.co.jp/digital/video/" + filename + "/" + filename + "pl.jpg"
}

// downloadDMMImage 下载 DMM 图（带 Referer 防盗链）
func downloadDMMImage(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://www.dmm.co.jp/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil || len(data) < 1000 {
		return nil, fmt.Errorf("数据无效（%d 字节）", len(data))
	}
	return data, nil
}

// cropPosterFromDMM 从 DMM 的 pl.jpg（横版拼贴图）左侧裁出竖版封面
// pl.jpg 结构：左侧是竖版封面，右侧是横版剧照拼贴
func cropPosterFromDMM(data []byte) ([]byte, bool) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, false
	}
	bounds := src.Bounds()
	imgW := bounds.Dx()
	imgH := bounds.Dy()
	if imgW < 2 || imgH < 2 {
		return nil, false
	}

	// 从左侧裁 2:3 竖图
	targetW := imgH * 2 / 3
	if targetW >= imgW {
		helpers.AppLogger.Warnf("[AV海报] DMM pl.jpg %dx%d 太窄，无法裁出竖版", imgW, imgH)
		return nil, false
	}

	cropRect := image.Rect(0, 0, targetW, imgH)
	dst := image.NewRGBA(image.Rect(0, 0, targetW, imgH))
	draw.Draw(dst, dst.Bounds(), src, cropRect.Min, draw.Src)

	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 92}); err != nil {
		return nil, false
	}
	helpers.AppLogger.Infof("[AV海报] 从 DMM pl.jpg 左侧裁出竖版: %dx%d", targetW, imgH)
	return out.Bytes(), true
}
