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
// 数据结构（严格对应 StashDB schema）
// ============================================================

type StashScene struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Details     string  `json:"details"`
	ReleaseDate string  `json:"release_date"` // "2019-04-23"
	Duration    int     `json:"duration"`     // 秒
	Code        *string `json:"code"`         // 可能 null
	Director    *string `json:"director"`     // 可能 null

	Studio *StashStudio `json:"studio"`

	Performers []StashPerformerRef `json:"performers"`
	Tags       []StashTag          `json:"tags"`
	Images     []StashImage        `json:"images"`
	Urls       []StashURL          `json:"urls"`
}

type StashStudio struct {
	ID     string       `json:"id"`
	Name   string       `json:"name"`
	Images []StashImage `json:"images"`
}

type StashPerformerRef struct {
	Performer StashPerformer `json:"performer"`
}

type StashPerformer struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Gender  string       `json:"gender"` // "FEMALE" / "MALE"
	Aliases []string     `json:"aliases"`
	Images  []StashImage `json:"images"`
}

type StashTag struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type StashImage struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
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
	} `json:"errors"`
}

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
// 字段模板
// ============================================================

const sceneFields = `
	id
	title
	details
	release_date
	duration
	code
	director
	studio {
		id
		name
		images { url width height }
	}
	performers {
		performer {
			id
			name
			gender
			aliases
			images { url width height }
		}
	}
	tags { id name }
	images { url width height }
	urls { url }
`

// ============================================================
// 公开方法
// ============================================================

// FindSceneByOshash 用 osHash 精确查找场景
// 未命中返回 (nil, nil)
func (c *StashDBClient) FindSceneByOshash(oshash string) (*StashScene, error) {
	if oshash == "" {
		return nil, fmt.Errorf("空 oshash")
	}

	query := fmt.Sprintf(`query FindScenesByFingerprints($fps: [[FingerprintQueryInput!]!]!) {
		findScenesBySceneFingerprints(fingerprints: $fps) {%s
		}
	}`, sceneFields)

	variables := map[string]interface{}{
		"fps": [][]map[string]string{
			{
				{
					"hash":      oshash,
					"algorithm": "OSHASH",
				},
			},
		},
	}

	var result struct {
		FindScenesBySceneFingerprints [][]*StashScene `json:"findScenesBySceneFingerprints"`
	}

	err := c.doGraphQL(query, variables, &result)
	if err != nil {
		return nil, err
	}

	if len(result.FindScenesBySceneFingerprints) == 0 ||
		len(result.FindScenesBySceneFingerprints[0]) == 0 {
		helpers.AppLogger.Infof("[StashDB] oshash %s 未命中", oshash)
		return nil, nil
	}

	scene := result.FindScenesBySceneFingerprints[0][0]
	helpers.AppLogger.Infof("[StashDB] oshash %s → scene %s (%s)",
		oshash, scene.ID, scene.Title)
	return scene, nil
}

// SearchScene 模糊搜索（osHash 未命中时的兜底）
func (c *StashDBClient) SearchScene(term string) ([]*StashScene, error) {
	if strings.TrimSpace(term) == "" {
		return nil, fmt.Errorf("空搜索词")
	}

	query := fmt.Sprintf(`query SearchScenes($term: String!) {
		searchScenes(term: $term) {
			count
			scenes {%s
			}
		}
	}`, sceneFields)

	var result struct {
		SearchScenes struct {
			Count  int           `json:"count"`
			Scenes []*StashScene `json:"scenes"`
		} `json:"searchScenes"`
	}

	err := c.doGraphQL(query, map[string]interface{}{"term": term}, &result)
	if err != nil {
		return nil, err
	}

	helpers.AppLogger.Infof("[StashDB] 搜索 %q 命中 %d 个场景", term, result.SearchScenes.Count)
	return result.SearchScenes.Scenes, nil
}

// FindSceneByID 按 ID 查询（用于 fallback）
func (c *StashDBClient) FindSceneByID(id string) (*StashScene, error) {
	if id == "" {
		return nil, fmt.Errorf("空 scene id")
	}

	query := fmt.Sprintf(`query FindScene($id: ID!) {
		findScene(id: $id) {%s
		}
	}`, sceneFields)

	var result struct {
		FindScene *StashScene `json:"findScene"`
	}

	err := c.doGraphQL(query, map[string]interface{}{"id": id}, &result)
	if err != nil {
		return nil, err
	}
	return result.FindScene, nil
}

// ============================================================
// 辅助
// ============================================================

// PrimaryImageURL 从 images 数组里取第一张的 URL
func PrimaryImageURL(images []StashImage) string {
	if len(images) == 0 {
		return ""
	}
	return images[0].URL
}

// 无用的占位（防止 unused 报错）
var _ = strings.TrimSpace

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}