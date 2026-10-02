package avscrape

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

// downloadImage 下载图片，处理防盗链
func downloadImage(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	// 防盗链 Referer
	switch {
	case strings.Contains(url, "javbus.com"):
		req.Header.Set("Referer", "https://www.javbus.com/")
	case strings.Contains(url, "dmm.co.jp") || strings.Contains(url, "dmm.com"):
		req.Header.Set("Referer", "https://www.dmm.co.jp/")
	case strings.Contains(url, "javstash.org"):
		req.Header.Set("Referer", "https://javstash.org/")
	case strings.Contains(url, "jav321.com"):
		req.Header.Set("Referer", "https://www.jav321.com/")
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36")
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func now() time.Time { return time.Now() }
