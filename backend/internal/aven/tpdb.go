package aven

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"qmediasync/internal/helpers"
)

// ============================================================
// TPDB GraphQL 客户端（认证：Authorization: Bearer）
// ============================================================

type TPDBClient struct {
	Endpoint string
	APIKey   string
	HTTP     *http.Client

	mu      sync.Mutex
	lastReq time.Time
}

const tpdbMinInterval = 1500 * time.Millisecond

func NewTPDBClient(endpoint, apiKey string) *TPDBClient {
	if endpoint == "" {
		endpoint = "https://theporndb.net/graphql"
	}
	return &TPDBClient{
		Endpoint: endpoint,
		APIKey:   apiKey,
		HTTP:     &http.Client{Timeout: 30 * time.Second},
	}
}

// ============================================================
// 数据结构
// ============================================================

type TPDBScene struct {
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Details     string       `json:"details"`     // TPDB 的剧情字段名
	Description string       `json:"description"` // 兼容旧版 / 某些 endpoint
	Date        string       `json:"date"`
	Duration    int          `json:"duration"`
	Rating      float64      `json:"rating"`
	Posters     TPDBImageSet `json:"posters"`
	Background  TPDBImageSet `json:"background"`
	// 注意：TPDB 的 background_back 是横版图，fanart 兜底用
	BackgroundBack TPDBImageSet    `json:"background_back"`
	Performers     []TPDBPerformer `json:"performers"`
	Studio         *TPDBStudio     `json:"studio"`
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

type TPDBStudio struct {
	Name string `json:"name"`
}

// GetPlot 统一返回剧情（优先 details，回退 description）
func (s *TPDBScene) GetPlot() string {
	if s.Details != "" {
		return s.Details
	}
	return s.Description
}

// ============================================================
// GraphQL 请求
// ============================================================

type tpdbGQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

type tpdbGQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (c *TPDBClient) doGraphQL(query string, variables map[string]interface{}, out interface{}) error {
	if c.APIKey == "" {
		return fmt.Errorf("TPDB API Key 未配置")
	}

	c.mu.Lock()
	elapsed := time.Since(c.lastReq)
	if elapsed < tpdbMinInterval {
		c.mu.Unlock()
		time.Sleep(tpdbMinInterval - elapsed)
		c.mu.Lock()
	}
	c.lastReq = time.Now()
	c.mu.Unlock()

	payload, _ := json.Marshal(tpdbGQLRequest{Query: query, Variables: variables})

	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
			helpers.AppLogger.Infof("[TPDB] 第 %d 次重试", attempt)
		}

		req, err := http.NewRequest("POST", c.Endpoint, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("构造请求失败: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
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
			return fmt.Errorf("TPDB 认证失败（HTTP 401）：请检查 API Key")
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			lastErr = fmt.Errorf("TPDB 限流（HTTP 429）")
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("TPDB HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
		}

		var gr tpdbGQLResponse
		if err := json.Unmarshal(body, &gr); err != nil {
			return fmt.Errorf("解析响应失败: %w", err)
		}
		if len(gr.Errors) > 0 {
			return fmt.Errorf("TPDB GraphQL 错误: %s", gr.Errors[0].Message)
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

// FindSceneByOshash 通过 oshash 查找 TPDB scene
// 未命中返回 (nil, nil)
func (c *TPDBClient) FindSceneByOshash(oshash string) (*TPDBScene, error) {
	if oshash == "" {
		return nil, fmt.Errorf("空 oshash")
	}

	// 注意：字段名用 details（TPDB schema 里是 details，不是 description）
	const query = `query($f: [[FingerprintQueryInput!]!]!) {
		findScenesBySceneFingerprints(fingerprints: $f) {
			id
			title
			details
			date
			duration
			rating
			posters { full large medium small }
			background { full large medium small }
			background_back { full large medium small }
			performers { name face }
			studio { name }
		}
	}`

	variables := map[string]interface{}{
		"f": [][]map[string]string{
			{
				{
					"hash":      oshash,
					"algorithm": "OSHASH",
				},
			},
		},
	}

	var result struct {
		FindScenesBySceneFingerprints [][]*TPDBScene `json:"findScenesBySceneFingerprints"`
	}

	err := c.doGraphQL(query, variables, &result)
	if err != nil {
		return nil, err
	}

	if len(result.FindScenesBySceneFingerprints) == 0 ||
		len(result.FindScenesBySceneFingerprints[0]) == 0 {
		helpers.AppLogger.Infof("[TPDB] oshash %s 未命中", oshash)
		return nil, nil
	}

	scene := result.FindScenesBySceneFingerprints[0][0]
	helpers.AppLogger.Infof("[TPDB] oshash %s → scene %s (%s) rating=%.2f",
		oshash, scene.ID, scene.Title, scene.Rating)
	return scene, nil
}
