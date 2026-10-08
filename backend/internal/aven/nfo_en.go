package aven

import (
	"encoding/json"
	"fmt"
	"strings"

	"qmediasync/internal/avscrape"
	"qmediasync/internal/models"
)

// GenerateNFON 生成欧美 NFO（和日本 AV 格式一致，但内容全是英文/翻译后的中文）
func GenerateNFON(r *avscrape.ScrapeResult) string {
	if r == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" ?>` + "\n")
	sb.WriteString("<movie>\n")

	sb.WriteString(fmt.Sprintf("  <title><![CDATA[%s]]></title>\n", r.Title))
	sb.WriteString(fmt.Sprintf("  <originaltitle><![CDATA[%s]]></originaltitle>\n", r.OriginalTitle))
	sb.WriteString(fmt.Sprintf("  <sorttitle><![CDATA[%s]]></sorttitle>\n", r.Code))
	sb.WriteString(fmt.Sprintf("  <num>%s</num>\n", r.Code))
	sb.WriteString(fmt.Sprintf("  <uniqueid type=\"stashdb\" default=\"true\">%s</uniqueid>\n", r.Code))

	if r.Oshash != "" {
		sb.WriteString(fmt.Sprintf("  <uniqueid type=\"oshash\" default=\"false\">%s</uniqueid>\n", r.Oshash))
	}

	if r.Plot != "" {
		sb.WriteString(fmt.Sprintf("  <plot><![CDATA[%s]]></plot>\n", r.Plot))
	}
	if r.Runtime > 0 {
		sb.WriteString(fmt.Sprintf("  <runtime>%d</runtime>\n", r.Runtime))
	}
	// 日期截断到 YYYY-MM-DD
	if r.ReleaseDate != "" {
		dateOnly := r.ReleaseDate
		if len(dateOnly) > 10 {
			dateOnly = dateOnly[:10]
		}
		sb.WriteString(fmt.Sprintf("  <premiered>%s</premiered>\n", dateOnly))
		sb.WriteString(fmt.Sprintf("  <releasedate>%s</releasedate>\n", dateOnly))
	}
	if r.Director != "" {
		sb.WriteString(fmt.Sprintf("  <director><![CDATA[%s]]></director>\n", r.Director))
	}
	if r.Studio != "" {
		sb.WriteString(fmt.Sprintf("  <studio><![CDATA[%s]]></studio>\n", r.Studio))
	}
	if r.Series != "" {
		sb.WriteString(fmt.Sprintf("  <series><![CDATA[%s]]></series>\n", r.Series))
	}

	// ===== 多演员（>=2）→ 加 <set> 合集 "共演"（和日本 AV 一致）=====
	if len(r.Actors) >= 2 {
		sb.WriteString("  <set>\n")
		sb.WriteString("    <name>共演</name>\n")
		sb.WriteString("  </set>\n")
	}
	// =================================================================

	// 评分：StashDB 没有，留空
	if r.Rating > 0 {
		sb.WriteString(fmt.Sprintf("  <rating>%.2f</rating>\n", r.Rating))
	}
	if r.Poster != "" {
		sb.WriteString(fmt.Sprintf("  <poster>%s</poster>\n", r.Poster))
		sb.WriteString(fmt.Sprintf("  <cover>%s</cover>\n", r.Poster))
	}
	if r.Fanart != "" {
		sb.WriteString(fmt.Sprintf("  <fanart>%s</fanart>\n", r.Fanart))
	}
	if r.Trailer != "" {
		sb.WriteString(fmt.Sprintf("  <trailer>%s</trailer>\n", r.Trailer))
	}

	// ===== Genres + ExtraTags 合并去重 =====
	seen := map[string]bool{}
	writeGenre := func(g string) {
		if g == "" || seen[g] {
			return
		}
		seen[g] = true
		sb.WriteString(fmt.Sprintf("  <genre><![CDATA[%s]]></genre>\n", g))
		sb.WriteString(fmt.Sprintf("  <tag><![CDATA[%s]]></tag>\n", g))
	}
	for _, g := range r.Genres {
		writeGenre(g)
	}
	for _, g := range r.ExtraTags {
		writeGenre(g)
	}
	// =====================================

	for _, a := range r.Actors {
		sb.WriteString("  <actor>\n")
		sb.WriteString(fmt.Sprintf("    <name><![CDATA[%s]]></name>\n", a.Name))
		sb.WriteString("    <type>Actor</type>\n")
		if a.Role != "" {
			sb.WriteString(fmt.Sprintf("    <role><![CDATA[%s]]></role>\n", a.Role))
		}
		if a.Image != "" {
			sb.WriteString(fmt.Sprintf("    <thumb>%s</thumb>\n", a.Image))
		}
		sb.WriteString("  </actor>\n")
	}

	for _, u := range r.Urls {
		sb.WriteString(fmt.Sprintf("  <website>%s</website>\n", u))
	}

	sb.WriteString("</movie>\n")
	return sb.String()
}

// MediaFromResultN 把 ScrapeResult 转成 AVENMedia 入库
func MediaFromResultN(r *avscrape.ScrapeResult, oshash string, fileSize int64) *models.AVENMedia {
	if r == nil {
		return nil
	}
	genres, _ := json.Marshal(r.Genres)
	actors, _ := json.Marshal(r.Actors)
	previews, _ := json.Marshal(r.PreviewImages)
	urls, _ := json.Marshal(r.Urls)

	return &models.AVENMedia{
		StashID:       r.Code,
		Oshash:        oshash,
		Title:         r.Title,
		OriginalTitle: r.OriginalTitle,
		Plot:          r.Plot,
		PlotOriginal:  r.PlotOriginal,
		Runtime:       r.Runtime,
		ReleaseDate:   r.ReleaseDate,
		Studio:        r.Studio,
		Series:        r.Series,
		Genres:        string(genres),
		Actors:        string(actors),
		Poster:        r.Poster,
		Fanart:        r.Fanart,
		PreviewImages: string(previews),
		Trailer:       r.Trailer,
		Rating:        r.Rating,
		Urls:          string(urls),
		NFOContent:    GenerateNFON(r),
		Source:        "stashdb",
		Resolution:    r.Resolution,
		IsHDR:         r.IsHDR,
		FileSize:      fileSize,
	}
}