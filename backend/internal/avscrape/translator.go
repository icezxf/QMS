package avscrape

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"qmediasync/internal/helpers"
)

// ============================================================
// 翻译错误标记
//
// 扫描器（aven/scanner_en.go、avscrape/scanner.go）靠这个前缀识别
// 是否需要暂停任务。演员名匹配不到中文名不加这个前缀，不触发暂停。
// ============================================================

const TranslateErrorPrefix = "[翻译错误] "

// HasTranslateError 检查 warnings 里是否有翻译错误
func HasTranslateError(warnings []string) bool {
	for _, w := range warnings {
		if strings.HasPrefix(w, TranslateErrorPrefix) {
			return true
		}
	}
	return false
}

// ============================================================

type Translator struct {
	Engine      string
	Target      string
	// ===== 源语言：日文默认 "ja"，欧美用 "en" =====
	SourceLang  string
	// ==========================================
	DeepLKey    string
	BingKey     string
	BingRegion  string
	GeminiKey   string
	GeminiModel string
	HTTP        *http.Client
	GoogleAPIKey           string
	GoogleTranslateForTags bool
	GoogleTranslateForAll  bool
}

func NewTranslator(engine, target string) *Translator {
	if target == "" {
		target = "zh"
	}
	return &Translator{
		Engine: engine,
		Target: target,
		// ===== 默认日语 =====
		SourceLang: "ja",
		// ===========================
		HTTP: &http.Client{Timeout: 90 * time.Second},
	}
}

func (t *Translator) Translate(text string) (string, error) {
	if text == "" {
		return "", nil
	}
	var result string
	var err error
	switch t.Engine {
	case "gemini":
		result, err = t.geminiTranslate(text)
	case "deepl":
		result, err = t.deepl(text)
	case "bing":
		result, err = t.bing(text)
	case "google_free":
		result, err = t.googleFree(text)
	case "mymemory":
		result, err = t.myMemory(text)
	case "google_cloud":
		result, err = t.googleCloudTranslate(text)
	default:
		return text, nil
	}
	if err != nil {
		helpers.AppLogger.Warnf("[翻译] 失败(%s): %v", t.Engine, err)
		return text, err
	}
	return result, nil
}

// ============================================================
// Gemini 引擎（Interactions API，带重试）
// ============================================================

type geminiInteractionResp struct {
	Status string `json:"status"`
	Steps  []struct {
		Type    string `json:"type"`
		Content []struct {
			Text string `json:"text"`
			Type string `json:"type"`
		} `json:"content"`
	} `json:"steps"`
}

func extractGeminiText(out *geminiInteractionResp) (string, error) {
	for _, s := range out.Steps {
		if s.Type == "model_output" {
			for _, c := range s.Content {
				if c.Text != "" {
					return strings.TrimSpace(c.Text), nil
				}
			}
		}
	}
	return "", fmt.Errorf("Gemini 返回为空")
}

func (t *Translator) geminiCall(prompt string) (string, error) {
	if t.GeminiKey == "" {
		return "", fmt.Errorf("Gemini API Key 未配置")
	}
	model := t.GeminiModel
	if model == "" {
		model = "gemini-3.8-flash"
	}

	endpoint := "https://generativelanguage.googleapis.com/v1beta/interactions"
	body := map[string]interface{}{
		"model": model,
		"input": prompt,
	}
	payload, _ := json.Marshal(body)

	const maxAttempts = 5
	var lastErr error
	var lastBody string

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			helpers.AppLogger.Infof("[Gemini] 第 %d 次重试（前次失败: %v）", attempt, lastErr)
			time.Sleep(15 * time.Second)
		}

		req, _ := http.NewRequest("POST", endpoint, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-goog-api-key", t.GeminiKey)

		resp, err := t.HTTP.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		lastBody = string(respBody)

		if resp.StatusCode == http.StatusOK {
			var out geminiInteractionResp
			if err := json.Unmarshal(respBody, &out); err != nil {
				lastErr = err
				continue
			}
			return extractGeminiText(&out)
		}

		if resp.StatusCode == 503 || resp.StatusCode == 429 {
			lastErr = fmt.Errorf("Gemini HTTP %d", resp.StatusCode)
			continue
		}
		return "", fmt.Errorf("Gemini HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return "", fmt.Errorf("Gemini 重试 %d 次均失败: %v, 最后返回: %s", maxAttempts, lastErr, lastBody)
}

func (t *Translator) geminiTranslate(text string) (string, error) {
	var langName string
	switch t.SourceLang {
	case "en":
		langName = "英文"
	case "ja", "":
		langName = "日文"
	default:
		langName = "原文"
	}
	prompt := fmt.Sprintf("请把下面的%s翻译成简体中文，只返回翻译结果，不要加任何解释、不要加引号：\n%s", langName, text)
	return t.geminiCall(prompt)
}

func (t *Translator) geminiTranslateAll(r *ScrapeResult) error {
	if t.GeminiKey == "" {
		return fmt.Errorf("Gemini API Key 未配置")
	}

	actorNames := make([]string, 0, len(r.Actors))
	for _, a := range r.Actors {
		actorNames = append(actorNames, a.Name)
	}

	input := map[string]interface{}{
		"title":  r.Title,
		"plot":   r.Plot,
		"actors": actorNames,
		"genres": r.Genres,
	}
	inputJSON, _ := json.Marshal(input)

	var langName string
	switch t.SourceLang {
	case "en":
		langName = "英文"
	case "ja", "":
		langName = "日文"
	default:
		langName = "原文"
	}

	prompt := fmt.Sprintf(`你是一个%s影视元数据翻译助手。请把下面 JSON 中的所有%s内容翻译成简体中文。

规则：
1. 如果文本中出现 __ACTOR_数字__ 这样的标记（例如 __ACTOR_0__），必须原样保留，不要翻译、不要修改、不要加空格。
2. 演员名必须翻译为该演员公认的中文译名。如果不确定，就保留原文，不要音译。
3. 标题翻译要自然通顺，保留原意，不要机翻腔。
4. 剧情简介要完整翻译，不要省略、不要概括。
5. 标签（genres）要翻译成中文习惯用语。
6. 只返回 JSON，不要加任何解释、不要加 markdown 代码块标记。

输入 JSON：
%s

请直接返回翻译后的 JSON，结构必须和输入完全一致（title/plot/actors/genres 四个字段）。`, langName, langName, string(inputJSON))

	raw, err := t.geminiCall(prompt)
	if err != nil {
		return err
	}

	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var translated struct {
		Title  string   `json:"title"`
		Plot   string   `json:"plot"`
		Actors []string `json:"actors"`
		Genres []string `json:"genres"`
	}
	if err := json.Unmarshal([]byte(raw), &translated); err != nil {
		return fmt.Errorf("Gemini 返回 JSON 解析失败: %w, 原始返回: %s", err, truncate(raw, 200))
	}

	if translated.Title != "" {
		r.Title = translated.Title
	}
	if translated.Plot != "" {
		r.Plot = translated.Plot
	}
	if len(translated.Actors) == len(r.Actors) {
		for i := range r.Actors {
			if translated.Actors[i] != "" {
				r.Actors[i].Name = translated.Actors[i]
			}
		}
	} else if len(translated.Actors) > 0 {
		helpers.AppLogger.Warnf("[翻译] Gemini 演员数量不匹配: 原 %d, 译 %d", len(r.Actors), len(translated.Actors))
	}
	if len(translated.Genres) == len(r.Genres) {
		for i := range r.Genres {
			if translated.Genres[i] != "" {
				r.Genres[i] = translated.Genres[i]
			}
		}
	} else if len(translated.Genres) > 0 {
		helpers.AppLogger.Warnf("[翻译] Gemini 标签数量不匹配: 原 %d, 译 %d", len(r.Genres), len(translated.Genres))
	}

	helpers.AppLogger.Infof("[翻译] Gemini 完成: title=%s", truncate(r.Title, 40))
	return nil
}

// ============================================================
// TranslateResult 统一入口
// ============================================================

func (t *Translator) TranslateResult(r *ScrapeResult) []string {
	var warnings []string
	if r == nil {
		return warnings
	}

	// ===== 1. 用占位符保护标题/简介中的演员名 =====
	type actorPh struct {
		ph          string
		chineseName string
	}
	var phs []actorPh

	for i, a := range r.Actors {
		if a.Name == "" {
			continue
		}
		ph := fmt.Sprintf("__ACTOR_%d__", i)
		phs = append(phs, actorPh{ph: ph, chineseName: a.Name})

		variants := append([]string{}, a.Aliases...)
		variants = append(variants, a.Name)
		for _, v := range variants {
			if v == "" {
				continue
			}
			if strings.Contains(r.Title, v) {
				r.Title = strings.ReplaceAll(r.Title, v, ph)
			}
			if strings.Contains(r.Plot, v) {
				r.Plot = strings.ReplaceAll(r.Plot, v, ph)
			}
		}
	}

	restore := func() {
		for _, p := range phs {
			r.Title = strings.ReplaceAll(r.Title, p.ph, p.chineseName)
			r.Plot = strings.ReplaceAll(r.Plot, p.ph, p.chineseName)
		}
	}

	if len(phs) > 0 {
		helpers.AppLogger.Infof("[翻译] 已用 %d 个占位符保护演员名", len(phs))
	}

	// ===== 2. Google 全量翻译 =====
	if t.GoogleTranslateForAll && t.GoogleAPIKey != "" {
		helpers.AppLogger.Infof("[翻译] 使用 Google Cloud 全量翻译")
		w := t.translateAllWithGoogle(r)
		warnings = append(warnings, w...)
		restore()
		return warnings
	}

	// ===== 3. 走原引擎 =====
	if t.Engine == "gemini" {
		if err := t.geminiTranslateAll(r); err == nil {
			restore()
			return warnings
		} else {
			helpers.AppLogger.Warnf("[翻译] Gemini 失败，降级到 DeepL: %v", err)
			// 注意：这里是"降级提示"，不是最终失败，不加 TranslateErrorPrefix
			warnings = append(warnings, fmt.Sprintf("Gemini 翻译失败: %v，已降级到 DeepL", err))
			t.Engine = "deepl"
		}
	}

	// 标题翻译
	if s, err := t.Translate(r.Title); err != nil {
		warnings = append(warnings, TranslateErrorPrefix+fmt.Sprintf("标题翻译失败: %v", err))
	} else if s != "" {
		r.Title = s
		helpers.AppLogger.Infof("[翻译] 标题 -> %s", s)
	}

	// 简介翻译
	if s, err := t.Translate(r.Plot); err != nil {
		warnings = append(warnings, TranslateErrorPrefix+fmt.Sprintf("简介翻译失败: %v", err))
	} else if s != "" {
		r.Plot = s
		helpers.AppLogger.Infof("[翻译] 简介 -> %s", truncate(s, 50))
	}

	// ===== 标签翻译 =====
	for i, g := range r.Genres {
		// 日文场景下：无日文假名跳过；英文场景下不跳过
		if t.SourceLang != "en" && !isJapanese(g) {
			helpers.AppLogger.Infof("[翻译] 标签 %s 无日文假名，跳过翻译", g)
			continue
		}
		var translated string
		var err error
		if t.GoogleTranslateForTags && t.GoogleAPIKey != "" {
			translated, err = t.googleCloudTranslate(g)
			if err != nil {
				helpers.AppLogger.Warnf("[翻译] Google 翻标签失败，回退原引擎: %s => %v", g, err)
				translated, err = t.Translate(g)
			} else if translated != "" {
				helpers.AppLogger.Infof("[翻译-Google] 标签 %s -> %s", g, translated)
			}
		} else {
			translated, err = t.Translate(g)
		}
		if err != nil {
			warnings = append(warnings, TranslateErrorPrefix+fmt.Sprintf("标签 %s 翻译失败: %v", g, err))
			// 一旦失败就停止，避免刷屏
			break
		}
		if translated != "" && translated != g {
			r.Genres[i] = translated
		}
	}

	helpers.AppLogger.Infof("[翻译] 演员名保留原值，跳过机翻")
	restore()
	return warnings
}

// ============================================================
// Google Cloud Translation API Basic (v2)
// ============================================================

func (t *Translator) googleCloudTranslate(text string) (string, error) {
	if t.GoogleAPIKey == "" {
		return text, fmt.Errorf("Google Cloud Translation API Key 未配置")
	}
	if strings.TrimSpace(text) == "" {
		return text, nil
	}

	endpoint := "https://translation.googleapis.com/language/translate/v2?key=" + url.QueryEscape(t.GoogleAPIKey)

	targetLang := "zh-CN"
	switch t.Target {
	case "zh", "zh-CN", "zh-Hans", "":
		targetLang = "zh-CN"
	case "zh-Hant", "zh-TW":
		targetLang = "zh-TW"
	case "en":
		targetLang = "en"
	case "ja":
		targetLang = "ja"
	}

	sourceLang := t.SourceLang
	if sourceLang == "" {
		sourceLang = "ja"
	}

	body := map[string]interface{}{
		"q":      []string{text},
		"source": sourceLang,
		"target": targetLang,
		"format": "text",
	}
	payload, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(payload))
	if err != nil {
		return text, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.HTTP.Do(req)
	if err != nil {
		return text, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return text, fmt.Errorf("Google Translate HTTP %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}

	var out struct {
		Data struct {
			Translations []struct {
				TranslatedText string `json:"translatedText"`
			} `json:"translations"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return text, fmt.Errorf("Google Translate 解析失败: %w", err)
	}
	if len(out.Data.Translations) == 0 {
		return text, fmt.Errorf("Google Translate 返回为空")
	}

	result := out.Data.Translations[0].TranslatedText
	result = strings.ReplaceAll(result, "&quot;", "\"")
	result = strings.ReplaceAll(result, "&#39;", "'")
	result = strings.ReplaceAll(result, "&amp;", "&")
	result = strings.ReplaceAll(result, "&lt;", "<")
	result = strings.ReplaceAll(result, "&gt;", ">")
	return result, nil
}

func (t *Translator) translateAllWithGoogle(r *ScrapeResult) []string {
	var warnings []string

	if s, err := t.googleCloudTranslate(r.Title); err != nil {
		warnings = append(warnings, TranslateErrorPrefix+fmt.Sprintf("Google 标题翻译失败: %v", err))
	} else if s != "" {
		r.Title = s
		helpers.AppLogger.Infof("[翻译-Google] 标题 -> %s", truncate(s, 40))
	}

	if s, err := t.googleCloudTranslate(r.Plot); err != nil {
		warnings = append(warnings, TranslateErrorPrefix+fmt.Sprintf("Google 简介翻译失败: %v", err))
	} else if s != "" {
		r.Plot = s
		helpers.AppLogger.Infof("[翻译-Google] 简介 -> %s", truncate(s, 40))
	}

	for i, g := range r.Genres {
		if t.SourceLang != "en" && !isJapanese(g) {
			continue
		}
		if s, err := t.googleCloudTranslate(g); err != nil {
			warnings = append(warnings, TranslateErrorPrefix+fmt.Sprintf("Google 标签 %s 翻译失败: %v", g, err))
			break
		} else if s != "" && s != g {
			r.Genres[i] = s
		}
	}
	return warnings
}

// ============================================================
// 传统引擎
// ============================================================

func (t *Translator) deepl(text string) (string, error) {
	if t.DeepLKey == "" {
		return text, fmt.Errorf("DeepL API Key 未配置")
	}
	endpoint := "https://api.deepl.com/v2/translate"
	if strings.HasSuffix(t.DeepLKey, ":fx") {
		endpoint = "https://api-free.deepl.com/v2/translate"
	}

	targetLang := "ZH"
	switch t.Target {
	case "zh", "zh-Hans", "zh-CN":
		targetLang = "ZH"
	case "zh-Hant", "zh-TW":
		targetLang = "ZH-HANT"
	case "en":
		targetLang = "EN-US"
	case "ja":
		targetLang = "JA"
	}

	sourceLang := t.SourceLang
	if sourceLang == "" {
		sourceLang = "JA"
	} else {
		sourceLang = strings.ToUpper(sourceLang)
	}

	form := url.Values{}
	form.Set("text", text)
	form.Set("source_lang", sourceLang)
	form.Set("target_lang", targetLang)

	req, _ := http.NewRequest("POST", endpoint, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "DeepL-Auth-Key "+t.DeepLKey)

	resp, err := t.HTTP.Do(req)
	if err != nil {
		return text, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return text, fmt.Errorf("DeepL HTTP %d: %s", resp.StatusCode, string(body))
	}

	var out struct {
		Translations []struct {
			Text string `json:"text"`
		} `json:"translations"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return text, err
	}
	if len(out.Translations) > 0 {
		return out.Translations[0].Text, nil
	}
	return text, nil
}

func (t *Translator) bing(text string) (string, error) {
	if t.BingKey == "" {
		return text, fmt.Errorf("Bing API Key 未配置")
	}
	endpoint := "https://api.cognitive.microsofttranslator.com/translate"
	u, _ := url.Parse(endpoint)
	q := u.Query()
	q.Set("api-version", "3.0")

	sourceLang := t.SourceLang
	if sourceLang == "" {
		sourceLang = "ja"
	}
	q.Set("from", sourceLang)

	q.Set("to", "zh-Hans")
	u.RawQuery = q.Encode()

	body, _ := json.Marshal([]map[string]string{{"Text": text}})
	req, _ := http.NewRequest("POST", u.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Ocp-Apim-Subscription-Key", t.BingKey)
	req.Header.Set("Ocp-Apim-Subscription-Region", t.BingRegion)

	resp, err := t.HTTP.Do(req)
	if err != nil {
		return text, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return text, fmt.Errorf("Bing HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result []struct {
		Translations []struct {
			Text string `json:"text"`
		} `json:"translations"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return text, err
	}
	if len(result) > 0 && len(result[0].Translations) > 0 {
		return result[0].Translations[0].Text, nil
	}
	return text, nil
}

func (t *Translator) googleFree(text string) (string, error) {
	u := fmt.Sprintf("https://translate.googleapis.com/translate_a/single?client=gtx&sl=auto&tl=%s&dt=t&q=%s",
		t.Target, url.QueryEscape(text))
	resp, err := t.HTTP.Get(u)
	if err != nil {
		return text, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var raw []interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return text, err
	}
	if len(raw) == 0 {
		return text, nil
	}
	segments, ok := raw[0].([]interface{})
	if !ok {
		return text, nil
	}
	var sb strings.Builder
	for _, seg := range segments {
		if arr, ok := seg.([]interface{}); ok && len(arr) > 0 {
			if s, ok := arr[0].(string); ok {
				sb.WriteString(s)
			}
		}
	}
	return sb.String(), nil
}

func (t *Translator) myMemory(text string) (string, error) {
	src := t.SourceLang
	if src == "" {
		src = "ja"
	}
	langPair := src + "|zh-CN"
	u := fmt.Sprintf("https://api.mymemory.translated.net/get?q=%s&langpair=%s",
		url.QueryEscape(text), url.QueryEscape(langPair))
	resp, err := t.HTTP.Get(u)
	if err != nil {
		return text, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out struct {
		ResponseData struct {
			TranslatedText string `json:"translatedText"`
		} `json:"responseData"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return text, err
	}
	if out.ResponseData.TranslatedText == "" {
		return text, fmt.Errorf("MyMemory 返回空")
	}
	return out.ResponseData.TranslatedText, nil
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}