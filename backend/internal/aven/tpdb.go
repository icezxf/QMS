package aven

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"qmediasync/internal/helpers"
)

// ============================================================
// TPDB REST API 客户端
//   - GraphQL 的 Scene 类型没有 rating / posters / background 字段
//   - REST 端点 api.theporndb.net/scenes?hash=xxx 才全
// ============================================================

type TPDBClient struct {
	BaseURL string // 默认 https://api.theporndb.net
	APIKey  string
	HTTP    *http.Client

	mu      sync.Mutex
	lastReq time.Time
}

const tpdbMinInterval = 1500 * time.Millisecond

func NewTPDBClient(endpoint, apiKey string) *TPDBClient {
	base := "https://api.theporndb.net"
	// 兼容用户传 GraphQL endpoint
	if endpoint != "" {
		if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
			base = u.Scheme + "://" + u.Host
		}
	}
	return &TPDBClient{
		BaseURL: base,
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// ============================================================
// 数据结构（对应 REST 返回）
// ============================================================

type TPDBScene struct {
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Date        string       `json:"date"`
	Duration    int          `json:"duration"`
	Rating      float64      `json:"rating"`
	Posters     TPDBImageSet `json:"posters"`
	Background  TPDBImageSet `json:"background"`
	BackgroundBack TPDBImageSet `json:"background_back"`
	Performers  []TPDBPerformer `json:"performers"`
	Site        *TPDBSite    `json:"site"`
}

type TPDBImageSet struct {
	Full   string `json:"full"`
	Large  string `json:"large"`
	Medium string `json:"medium"`
	Small  string `json:"small"`
}

type TPDBPerformer struct {
	Name string `json:"name"`
	Face string `json:"face"`
}

type TPDBSite struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// GetPlot 剧情（REST 是 description）
func (s *TPDBScene) GetPlot() string {
	return s.Description
}

// GetStudio 片商名（REST 里是 site.name）
func (s *TPDBScene) GetStudio() string {
	if s.Site != nil {
		return s.Site.Name
	}
	return ""
}

// REST 响应包裹
type tpdbRESTResp struct {
	Data []*TPDBScene `json:"data"`
}

// ============================================================
// 公开方法
// ============================================================

// FindSceneByOshash 通过 oshash 查找 TPDB scene（REST）
// 未命中返回 (nil, nil)
func (c *TPDBClient) FindSceneByOshash(oshash string) (*TPDBScene, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("TPDB API Key 未配置")
	}
	if oshash == "" {
		return nil, fmt.Errorf("空 oshash")
	}

	endpoint := fmt.Sprintf("%s/scenes?hash=%s",
		strings.TrimRight(c.BaseURL, "/"),
		url.QueryEscape(oshash))

	// 限速
	c.mu.Lock()
	elapsed := time.Since(c.lastReq)
	if elapsed < tpdbMinInterval {
		c.mu.Unlock()
		time.Sleep(tpdbMinInterval - elapsed)
		c.mu.Lock()
	}
	c.lastReq = time.Now()
	c.mu.Unlock()

	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
			helpers.AppLogger.Infof("[TPDB] 第 %d 次重试", attempt)
		}

		req, err := http.NewRequest("GET", endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("构造请求失败: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("User-Agent", "QMediaSync/1.0")

		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("请求失败: %w", err)
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf("TPDB 认证失败（HTTP 401）：请检查 API Key")
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			lastErr = fmt.Errorf("TPDB 限流（HTTP 429）")
			continue
		}
		if resp.StatusCode == http.StatusNotFound {
			helpers.AppLogger.Infof("[TPDB] oshash %s 未命中（404）", oshash)
			return nil, nil
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("TPDB HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
		}

		var parsed tpdbRESTResp
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, fmt.Errorf("解析响应失败: %w", err)
		}

		if len(parsed.Data) == 0 {
			helpers.AppLogger.Infof("[TPDB] oshash %s 未命中（空结果）", oshash)
			return nil, nil
		}

		scene := parsed.Data[0]
		helpers.AppLogger.Infof("[TPDB] oshash %s → scene %s (%s) rating=%.2f",
			oshash, scene.ID, scene.Title, scene.Rating)
		return scene, nil
	}
	return nil, fmt.Errorf("重试 %d 次均失败: %v", maxAttempts, lastErr)
}
