package aven

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"qmediasync/internal/helpers"
)

// ============================================================
// StashDB GraphQL 客户端
// ============================================================

type StashDBClient struct {
	Endpoint string
	APIKey   string
	HTTP     *http.Client

	mu      sync.Mutex
	lastReq time.Time
}

// 限速间隔（StashDB 官方建议约 1 请求/秒，避免被限流）
const stashDBMinInterval = 1200 * time.Millisecond

func NewStashDBClient(endpoint, apiKey string) *StashDBClient {
	if endpoint == "" {
		endpoint = "https://stashdb.org/graphql"
	}
	return &StashDBClient{
		Endpoint: endpoint,
		APIKey:   apiKey,
		HTTP:     &http.Client{Timeout: 30 * time.Second},
	}
}

// ============================================================
// StashDB 数据结构
// ============================================================

type StashScene struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Details  string  `json:"details"`
	Date     string  `json:"date"`
	Duration int     `json:"duration"` // 秒
	Rating   float64 `json:"rating"`   // 1-100
	Trailer  string  `json:"trailer"`

	Studio *StashStudio `json:"studio"`

	Performers []StashPerformerRef `json:"performers"`
	Tags       []StashTag          `json:"tags"`
	Images     []StashImage        `json:"images"`
	Paths      *StashPaths         `json:"paths"`
	Urls       []StashURL          `json:"urls"`
}

type StashStudio struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Image string `json:"image"`
}

type StashPerformerRef struct {
	Performer StashPerformer `json:"performer"`
}

type StashPerformer struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Image   string   `json:"image"`   // 可能是相对路径或完整 URL
	Gender  string   `json:"gender"`
	Aliases []string `json:"aliases"`
}

type StashTag struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type StashImage struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

type StashPaths struct {
	Screenshot string `json:"screenshot"`
}

type StashURL struct {
	URL string `json:"url"`
}

// ============================================================
// GraphQL 请求
// ============================================================

type gqlRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
		Path    []any  `json:"path,omitempty"`
	} `json:"errors"`
}

// doGraphQL 通用 GraphQL 请求，带重试 + 限速
func (c *StashDBClient) doGraphQL(query string, variables map[string]interface{}, out interface{}) error {
	if c.APIKey == "" {
		return fmt.Errorf("StashDB API Key 未配置")
	}

	// 限速
	c.mu.Lock()
	elapsed := time.Since(c.lastReq)
	if elapsed < stashDBMinInterval {
		c.mu.Unlock()
		time.Sleep(stashDBMinInterval - elapsed)
		c.mu.Lock()
	}
	c.lastReq = time.Now()
	c.mu.Unlock()

	payload, _ := json.Marshal(gqlRequest{Query: query, Variables: variables})

	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
			helpers.AppLogger.Infof("[StashDB] 第 %d 次重试", attempt)
		}

		req, err := http.NewRequest("POST", c.Endpoint, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("构造请求失败: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		// StashDB 认证：ApiKey header
		req.Header.Set("ApiKey", c.APIKey)
		req.Header.Set("User-Agent", "QMediaSync/1.0")

		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("请求失败: %w", err)
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("StashDB 认证失败（HTTP 401）：请检查 API Key")
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			lastErr = fmt.Errorf("StashDB 限流（HTTP 429）")
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("StashDB HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
		}

		var gr gqlResponse
		if err := json.Unmarshal(body, &gr); err != nil {
			return fmt.Errorf("解析响应失败: %w", err)
		}
		if len(gr.Errors) > 0 {
			return fmt.Errorf("StashDB GraphQL 错误: %s", gr.Errors[0].Message)
		}
		if err := json.Unmarshal(gr.Data, out); err != nil {
			return fmt.Errorf("解析 data 失败: %w", err)
		}
		return nil
	}
	return fmt.Errorf("重试 %d 次均失败: %v", maxAttempts, lastErr)
}

// ============================================================
// 公开方法
// ============================================================

// FindSceneByOshash 用 osHash 精确查找场景
// 未命中返回 (nil, nil)
func (c *StashDBClient) FindSceneByOshash(oshash string) (*StashScene, error) {
	if oshash == "" {
		return nil, fmt.Errorf("空 oshash")
	}

	query := `query FindSceneByHash($oshash: String!) {
		findSceneByHash(input: { oshash: $oshash }) {
			id
			title
			details
			date
			duration
			rating
			trailer
			studio { id name image }
			performers {
				performer { id name image gender aliases }
			}
			tags { id name }
			images { id url }
			paths { screenshot }
			urls { url }
		}
	}`

	var result struct {
		FindSceneByHash *StashScene `json:"findSceneByHash"`
	}

	err := c.doGraphQL(query, map[string]interface{}{"oshash": oshash}, &result)
	if err != nil {
		return nil, err
	}
	if result.FindSceneByHash == nil {
		helpers.AppLogger.Infof("[StashDB] oshash %s 未命中", oshash)
		return nil, nil
	}

	helpers.AppLogger.Infof("[StashDB] oshash %s → scene %s (%s)",
		oshash, result.FindSceneByHash.ID, result.FindSceneByHash.Title)
	return result.FindSceneByHash, nil
}

// SearchScene 模糊搜索（osHash 未命中时的兜底）
// 一般用文件名或标题作为 term
func (c *StashDBClient) SearchScene(term string) ([]*StashScene, error) {
	if strings.TrimSpace(term) == "" {
		return nil, fmt.Errorf("空搜索词")
	}

	query := `query SearchScene($term: String!) {
		searchScene(term: $term) {
			id
			title
			details
			date
			duration
			rating
			trailer
			studio { id name image }
			performers {
				performer { id name image gender aliases }
			}
			tags { id name }
			images { id url }
			paths { screenshot }
			urls { url }
		}
	}`

	var result struct {
		SearchScene []*StashScene `json:"searchScene"`
	}

	err := c.doGraphQL(query, map[string]interface{}{"term": term}, &result)
	if err != nil {
		return nil, err
	}

	helpers.AppLogger.Infof("[StashDB] 搜索 %q 命中 %d 个场景", term, len(result.SearchScene))
	return result.SearchScene, nil
}

// ============================================================
// 辅助：规范化 URL（StashDB 的 image 可能是相对路径）
// ============================================================

// normalizeStashImageURL StashDB 的 performer.Image / studio.Image 可能是相对路径，
// 需要拼上 CDN 前缀
func normalizeStashImageURL(u string) string {
	if u == "" {
		return ""
	}
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	// 相对路径，拼 StashDB 的 CDN
	return "https://stashdb.org" + ensureLeadingSlash(u)
}

func ensureLeadingSlash(s string) string {
	if strings.HasPrefix(s, "/") {
		return s
	}
	return "/" + s
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}