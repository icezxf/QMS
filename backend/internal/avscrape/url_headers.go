package avscrape

import (
	"net/http"
	"sync"
)

var urlHeaderCache sync.Map // key: url, value: http.Header

func cacheURLHeader(url string, h http.Header) {
	if url == "" || h == nil {
		return
	}
	urlHeaderCache.Store(url, h)
}

func getURLHeader(url string) http.Header {
	if v, ok := urlHeaderCache.Load(url); ok {
		return v.(http.Header)
	}
	return nil
}

// GetURLHeader 返回指定 URL 缓存的请求头（含 115/OpenList 的 UA）。
// 未命中返回 nil。
func GetURLHeader(url string) http.Header {
	return getURLHeader(url)
}