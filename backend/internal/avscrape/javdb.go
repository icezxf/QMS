package avscrape

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"

	"qmediasync/internal/helpers"
)

type JavDBClient struct {
	Cookie  string
	HTTP    *http.Client
	lastReq time.Time
	mu      sync.Mutex
	cache   map[string]*javdbRatingCache
}

type javdbRatingCache struct {
	rating float64
	votes  int
	at     time.Time
}

func NewJavDBClient(cookie string) *JavDBClient {
	return &JavDBClient{
		Cookie: cookie,
		HTTP:   &http.Client{Timeout: 30 * time.Second},
		cache:  make(map[string]*javdbRatingCache),
	}
}

// ===== 改动：兼容 "4.5分, 由2315人評價" 这类格式 =====
var javdbScoreRe = regexp.MustCompile(`([\d.]+)\s*分.*?(\d+)\s*人`)
// ===================================================

func (c *JavDBClient) GetRating(code string) (float64, int, error) {
	if code == "" {
		return 0, 0, fmt.Errorf("空番号")
	}

	if c.Cookie == "" {
		helpers.AppLogger.Warnf("[JavDB] Cookie 未配置，尝试直连（可能触发 Cloudflare 挑战）")
	}

	c.mu.Lock()
	if entry, ok := c.cache[code]; ok {
		if time.Since(entry.at) < 24*time.Hour {
			c.mu.Unlock()
			helpers.AppLogger.Infof("[JavDB] 命中缓存: %s => %.2f (%d人)", code, entry.rating, entry.votes)
			return entry.rating, entry.votes, nil
		}
	}

	elapsed := time.Since(c.lastReq)
	if elapsed < 15*time.Second {
		wait := 15*time.Second - elapsed
		c.mu.Unlock()
		helpers.AppLogger.Infof("[JavDB] 限速等待 %.1f 秒", wait.Seconds())
		time.Sleep(wait)
		c.mu.Lock()
	}
	c.lastReq = time.Now()
	c.mu.Unlock()

	searchURL := fmt.Sprintf("https://javdb.com/search?f=all&q=%s", code)
	req, _ := http.NewRequest("GET", searchURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36")
	if c.Cookie != "" {
		req.Header.Set("Cookie", c.Cookie)
	}
	req.Header.Set("Referer", "https://javdb.com/")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("请求搜索页失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("JavDB HTTP %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return 0, 0, fmt.Errorf("解析 HTML 失败: %w", err)
	}

	// ===== 放宽选择器：div.item 直接选，不再要求 movie-list 父容器 =====
	items := doc.Find("div.item")
	if items.Length() == 0 {
		return 0, 0, fmt.Errorf("JavDB 返回异常（可能触发 Cloudflare 挑战或 Cookie 过期）")
	}
	// ===================================================================

	normalized := strings.ToUpper(strings.ReplaceAll(code, "-", ""))
	var rating float64
	var votes int
	var found bool

	items.EachWithBreak(func(i int, s *goquery.Selection) bool {
		titleCode := strings.ToUpper(strings.TrimSpace(s.Find("div.video-title strong").Text()))
		titleCode = strings.ReplaceAll(titleCode, "-", "")
		if titleCode != normalized {
			return true
		}
		scoreText := strings.TrimSpace(s.Find("div.score span.value").Text())
		scoreText = strings.ReplaceAll(scoreText, "\n", " ")
		scoreText = strings.Join(strings.Fields(scoreText), " ")
		helpers.AppLogger.Infof("[JavDB] %s 找到条目，scoreText=%q", code, scoreText)

		m := javdbScoreRe.FindStringSubmatch(scoreText)
		if len(m) != 3 {
			helpers.AppLogger.Warnf("[JavDB] 解析评分失败: %s", scoreText)
			return false
		}
		fmt.Sscanf(m[1], "%f", &rating)
		fmt.Sscanf(m[2], "%d", &votes)
		found = true
		return false
	})

	if !found {
		return 0, 0, fmt.Errorf("JavDB 未找到番号 %s（或评分元素未匹配）", code)
	}

	// JavDB 是 5 分制，×2 转成 10 分制
	rating = rating * 2

	c.mu.Lock()
	c.cache[code] = &javdbRatingCache{rating: rating, votes: votes, at: time.Now()}
	c.mu.Unlock()

	helpers.AppLogger.Infof("[JavDB] %s => %.2f (%d人)", code, rating, votes)
	return rating, votes, nil
}
