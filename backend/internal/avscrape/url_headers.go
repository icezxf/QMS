package avscrape

import (
	"net/http"
	"sync"
)

var urlHeaderCache sync.Map // key: url, value: http.Header

// cacheURLHeader 缓存 OpenList 返回的 header
func cacheURLHeader(url string, h http.Header) {
	if url == "" || h == nil {
		return
	}
	urlHeaderCache.Store(url, h)
}

// getURLHeader 取回 header
func getURLHeader(url string) http.Header {
	if v, ok := urlHeaderCache.Load(url); ok {
		return v.(http.Header)
	}
	return nil
}
