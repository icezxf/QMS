package avscrape

import "net/http"

// GetURLHeader 返回指定 URL 缓存的请求头（含 115/OpenList 的 UA）。
// 未命中返回 nil。
func GetURLHeader(url string) http.Header {
	return getURLHeader(url)
}