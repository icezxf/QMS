package avscrape

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var jav8HTTP = &http.Client{Timeout: 15 * time.Second}

var (
	jav8DetailRe = regexp.MustCompile(`href="(/v/\d+)"`)
	jav8TitleRe  = regexp.MustCompile(`<title>([^<]+)</title>`)
	jav8PlotRe   = regexp.MustCompile(`(?s)<div[^>]*class="[^"]*text-zh[^"]*"[^>]*>\s*<p[^>]*class="[^"]*intro-text[^"]*"[^>]*>(.*?)</p>`)
	jav8TagRe    = regexp.MustCompile(`<[^>]+>`)
)

// FetchJav8 从 jav8.me 抓取中文标题和中文剧情
// 输入：标准番号（如 SSIS-468）
// 返回：title（中文标题）、plot（中文剧情）
// 任何一步失败都返回空字符串，不返回 error（静默兜底）
func FetchJav8(code string) (title, plot string) {
	code = strings.TrimSpace(code)
	if code == "" {
		return "", ""
	}

	// 1. 搜索页
	searchURL := "https://jav8.me/search?type=id&q=" + url.QueryEscape(code)
	body, err := jav8Get(searchURL)
	if err != nil {
		return "", ""
	}

	m := jav8DetailRe.FindStringSubmatch(body)
	if len(m) < 2 {
		return "", ""
	}
	detailPath := m[1]

	// 限速，避免连续请求
	time.Sleep(200 * time.Millisecond)

	// 2. 详情页
	detailURL := "https://jav8.me" + detailPath
	body2, err := jav8Get(detailURL)
	if err != nil {
		return "", ""
	}

	// 3. 提取中文标题（<title>SSIS-468 河北彩花的... - JAV8</title>）
	title = extractJav8Title(body2, code)

	// 4. 提取中文剧情（<div class="text-zh"> 里的 <p class="intro-text">）
	plot = extractJav8Plot(body2)

	return title, plot
}

func extractJav8Title(htmlStr, code string) string {
	tm := jav8TitleRe.FindStringSubmatch(htmlStr)
	if len(tm) < 2 {
		return ""
	}
	raw := html.UnescapeString(strings.TrimSpace(tm[1]))
	raw = strings.TrimSuffix(raw, " - JAV8")
	raw = strings.TrimSpace(raw)

	// 校验番号是否在标题里，防止搜错片
	if code != "" && !strings.Contains(strings.ToUpper(raw), strings.ToUpper(code)) {
		return ""
	}

	// 去掉番号前缀
	upperCode := strings.ToUpper(code)
	upperRaw := strings.ToUpper(raw)
	if strings.HasPrefix(upperRaw, upperCode) {
		raw = strings.TrimSpace(raw[len(code):])
	}
	return raw
}

func extractJav8Plot(htmlStr string) string {
	m := jav8PlotRe.FindStringSubmatch(htmlStr)
	if len(m) < 2 {
		return ""
	}
	text := jav8TagRe.ReplaceAllString(m[1], "")
	text = html.UnescapeString(text)
	text = strings.TrimSpace(text)

	// 至少 15 个字符才算有效剧情，过滤空壳
	if len([]rune(text)) < 15 {
		return ""
	}
	return text
}

func jav8Get(rawURL string) (string, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")

	resp, err := jav8HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	return string(body), nil
}
