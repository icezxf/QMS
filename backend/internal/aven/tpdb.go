package aven

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"qmediasync/internal/helpers"
)

// ============================================================
// TPDB GraphQL 客户端（认证方式：Authorization: Bearer）
// ============================================================

type TPDBClient struct {
	Endpoint string
	APIKey   string
	HTTP     *http.Client
	mu       sync.Mutex
	lastReq  time.Time
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

// TPDBScene TPDB 返回的场景数据
type TPDBScene struct {
	ID             string        `json:"id"`
	Title          string        `json:"title"`
	Description    string        `json:"description"`
	Rating         float64       `json:"rating"`
	Posters        TPDBImages    `json:"posters"`
	Background     TPDBImages    `json:"background"`
	BackgroundBack TPDBImages    `json:"background_back"`
	Performers     []TPDBPerfRef `json:"performers"`
}

type TPDBImages struct {
	Full   string `json:"full"`
	Large  string `json:"large"`
	Medium string `json:"medium"`
	Small  string `json:"small"`
}

type TPDBPerfRef struct {
	Name string `json:"name"`
	Face string `json:"face"`
}

// FindSceneByOshash 通过 oshash 查找 TPDB scene
func (c *TPDBClient) FindSceneByOshash(oshash string) (*TPDBScene, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("TPDB API Key 未配置")
	}

	const query = `query($f: [[FingerprintQueryInput!]!]!) {
		findScenesBySceneFingerprints(fingerprints: $f) {
			id
			title
			description
			rating
			posters { full large medium small }
			background { full large medium small }
			background_back { full large medium small }
			performers { name face }
		}
	}`

	variables := map[string]interface{}{
		"f": [][]map[string]string{
			{{"algorithm": "OSHASH", "hash": oshash}},
		},
	}

	var resp struct {
		Data struct {
			Scenes [][]TPDBScene `json:"findScenesBySceneFingerprints"`
		} `json:"data"`
	}

	if err := c.doGraphQL(query, variables, &resp); err != nil {
		return nil, err
	}

	if len(resp.Data.Scenes) == 0 || len(resp.Data.Scenes[0]) == 0 {
		return nil, nil
	}
	return &resp.Data.Scenes[0][0], nil
}

// doGraphQL 统一请求（带限速 + 重试）
func (c *TPDBClient) doGraphQL(query string, variables map[string]interface{}, out interface{}) error {
	c.mu.Lock()
	elapsed := time.Since(c.lastReq)
	if elapsed < tpdbMinInterval {
		c.mu.Unlock()
		time.Sleep(tpdbMinInterval - elapsed)
		c.mu.Lock()
	}
	c.lastReq = time.Now()
	c.mu.Unlock()

	payload, _ := json.Marshal(map[string]interface{}{
		"query":     query,
		"variables": variables,
	})

	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
			helpers.AppLogger.Infof("[TPDB] 第 %d 次重试", attempt)
		}

		req, err := http.NewRequest("POST", c.Endpoint, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		var gqlResp struct {
			Data   json.RawMessage `json:"data"`
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&gqlResp); err != nil {
			resp.Body.Close()
			lastErr = err
			continue
		}
		resp.Body.Close()

		if len(gqlResp.Errors) > 0 {
			lastErr = fmt.Errorf("GraphQL 错误: %s", gqlResp.Errors[0].Message)
			continue
		}

		return json.Unmarshal(gqlResp.Data, out)
	}
	return lastErr
}