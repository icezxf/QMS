package douban

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"Q115-STRM/internal/helpers"
)

const (
	// 全局请求间隔
	minRequestInterval = 2 * time.Second
	// 缓存有效期 2 天
	cacheTTL = 48 * time.Hour
	// 官方 App API Key（用于电影/季评分查询）
	defaultApiKey = "0ab215a8b1977939201640fa14c66bab"
	// 第三方聚合 API（用于电视剧评分查询）
	tvApiHost = "https://douban-idatabase.kfstorm.com"
)

// ==================== 全局限速 ====================

var (
	rateMu          sync.Mutex
	lastRequestTime time.Time
)

func globalRateLimit() {
	rateMu.Lock()
	defer rateMu.Unlock()
	elapsed := time.Since(lastRequestTime)
	if elapsed < minRequestInterval {
		time.Sleep(minRequestInterval - elapsed)
	}
	lastRequestTime = time.Now()
}

// ==================== 缓存 ====================

type cacheEntry struct {
	rating float64
	expire time.Time
}

var (
	cacheMu     sync.Mutex
	ratingCache = map[string]cacheEntry{}
)

func getCache(key string) (float64, bool) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	entry, ok := ratingCache[key]
	if !ok || time.Now().After(entry.expire) {
		delete(ratingCache, key)
		return 0, false
	}
	return entry.rating, true
}

func setCache(key string, rating float64) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	ratingCache[key] = cacheEntry{rating: rating, expire: time.Now().Add(cacheTTL)}
}

// 后台定期清理过期缓存，避免长期运行缓慢累积
func init() {
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		for range ticker.C {
			cacheMu.Lock()
			now := time.Now()
			for k, v := range ratingCache {
				if now.After(v.expire) {
					delete(ratingCache, k)
				}
			}
			cacheMu.Unlock()
		}
	}()
}

// ==================== API 响应结构 ====================

// 官方电影 API 响应
type doubanMovieApiResponse struct {
	Rating struct {
		Average string `json:"average"`
	} `json:"rating"`
	Title string `json:"title"`
	Msg   string `json:"msg"`
	Code  int    `json:"code"`
}

// 第三方聚合 API 响应
type doubanTvApiItem struct {
	DoubanID    string  `json:"douban_id"`
	ImdbID      string  `json:"imdb_id"`
	DoubanTitle string  `json:"douban_title"`
	Year        int     `json:"year"`
	Rating      float64 `json:"rating"`
}

// ==================== Client ====================

type Client struct {
	httpClient *http.Client
	apiKey     string
}

func NewClient(apiKey string) *Client {
	if apiKey == "" {
		apiKey = os.Getenv("DOUBAN_API_KEY")
	}
	if apiKey == "" {
		apiKey = defaultApiKey
	}
	return &Client{
		httpClient: &http.Client{Timeout: 15 * time.Second},
		apiKey:     apiKey,
	}
}

// ==================== 电影查询（官方 API，通过 IMDb ID） ====================

// GetRatingByImdb 通过 IMDb ID 获取电影豆瓣评分
func (c *Client) GetRatingByImdb(imdbId string) (float64, error) {
	if strings.TrimSpace(imdbId) == "" {
		return 0, nil
	}

	cacheKey := "movie_imdb_" + imdbId
	if rating, ok := getCache(cacheKey); ok {
		return rating, nil
	}

	globalRateLimit()

	apiURL := fmt.Sprintf("https://api.douban.com/v2/movie/imdb/%s", imdbId)
	formData := url.Values{}
	formData.Set("apikey", c.apiKey)

	req, err := http.NewRequest("POST", apiURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("豆瓣电影 API 返回非 200: %d", resp.StatusCode)
	}

	var result doubanMovieApiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	if result.Code != 0 {
		helpers.AppLogger.Warnf("[豆瓣API] 电影请求失败: %s (code: %d)", result.Msg, result.Code)
		return 0, fmt.Errorf("douban api error: %s", result.Msg)
	}

	ratingStr := strings.TrimSpace(result.Rating.Average)
	if ratingStr == "" {
		return 0, nil
	}

	rating, err := strconv.ParseFloat(ratingStr, 64)
	if err != nil {
		return 0, nil
	}

	setCache(cacheKey, rating)
	return rating, nil
}

// ==================== 电视剧查询（第三方聚合 API，通过 IMDb ID） ====================

// GetTVRatingByImdb 通过 IMDb ID 获取电视剧豆瓣评分
func (c *Client) GetTVRatingByImdb(imdbId string) (float64, error) {
	if strings.TrimSpace(imdbId) == "" {
		return 0, nil
	}

	cacheKey := "tv_imdb_" + imdbId
	if rating, ok := getCache(cacheKey); ok {
		return rating, nil
	}

	globalRateLimit()

	apiURL := fmt.Sprintf("%s/api/item?imdb_id=%s", tvApiHost, imdbId)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("第三方豆瓣 API 返回非 200: %d", resp.StatusCode)
	}

	var results []doubanTvApiItem
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return 0, err
	}

	if len(results) == 0 {
		return 0, nil
	}

	rating := results[0].Rating
	if rating <= 0 {
		return 0, nil
	}

	setCache(cacheKey, rating)
	return rating, nil
}

// ==================== 季评分（搜索方案） ====================

// GetSeasonRatingByTitle 通过「剧名+季号」搜索豆瓣季条目，获取该季评分
func (c *Client) GetSeasonRatingByTitle(title string, originalTitle string, seasonNumber int) (float64, error) {
	if strings.TrimSpace(title) == "" && strings.TrimSpace(originalTitle) == "" {
		return 0, nil
	}

	cacheKey := fmt.Sprintf("season_%s_%s_%d", title, originalTitle, seasonNumber)
	if rating, ok := getCache(cacheKey); ok {
		return rating, nil
	}

	seasonStr := seasonNumberToChinese(seasonNumber)

	// 依次尝试中文名、原始名
	candidates := make([]string, 0, 2)
	if title != "" {
		candidates = append(candidates, fmt.Sprintf("%s %s", title, seasonStr))
	}
	if originalTitle != "" && originalTitle != title {
		candidates = append(candidates, fmt.Sprintf("%s %s", originalTitle, seasonStr))
	}

	for _, keyword := range candidates {
		sid := c.searchSeasonId(keyword, seasonStr)
		if sid == "" {
			continue
		}
		rating, err := c.fetchRatingById(sid)
		if err == nil && rating > 0 {
			setCache(cacheKey, rating)
			helpers.AppLogger.Infof("[豆瓣] 季搜索命中: %s -> sid=%s, rating=%.1f", keyword, sid, rating)
			return rating, nil
		}
	}

	setCache(cacheKey, 0)
	return 0, nil
}

// searchSeasonId 用豆瓣搜索建议接口按「关键词」搜索，返回标题包含季号的条目 ID
func (c *Client) searchSeasonId(keyword string, seasonStr string) string {
	globalRateLimit()

	searchURL := fmt.Sprintf("https://movie.douban.com/j/subject_suggest?q=%s", url.QueryEscape(keyword))

	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36")
	req.Header.Set("Referer", "https://movie.douban.com/")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		helpers.AppLogger.Warnf("[豆瓣] subject_suggest 返回非 200: %d, keyword=%s", resp.StatusCode, keyword)
		return ""
	}

	// subject_suggest 返回的是数组
	var results []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Type  string `json:"type"`
		Year  string `json:"year"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return ""
	}

	// 匹配标题里包含季号
	for _, item := range results {
		if strings.Contains(item.Title, seasonStr) {
			return item.ID
		}
	}
	return ""
}

// fetchRatingById 用官方 App API 按豆瓣 ID 查询评分
func (c *Client) fetchRatingById(sid string) (float64, error) {
	globalRateLimit()

	apiURL := fmt.Sprintf("https://api.douban.com/v2/movie/subject/%s", sid)
	formData := url.Values{}
	formData.Set("apikey", c.apiKey)

	req, err := http.NewRequest("POST", apiURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("豆瓣详情返回非 200: %d", resp.StatusCode)
	}

	var result struct {
		Rating struct {
			Average float64 `json:"average"`
		} `json:"rating"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	return result.Rating.Average, nil
}

// seasonNumberToChinese 数字季号转中文
func seasonNumberToChinese(n int) string {
	m := map[int]string{
		1: "第一季", 2: "第二季", 3: "第三季", 4: "第四季", 5: "第五季",
		6: "第六季", 7: "第七季", 8: "第八季", 9: "第九季", 10: "第十季",
	}
	if s, ok := m[n]; ok {
		return s
	}
	return fmt.Sprintf("第%d季", n)
}
