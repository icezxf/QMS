package avscrape

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type MetaTubeClient struct {
	Server string
	HTTP   *http.Client
}

func NewMetaTubeClient(server string) *MetaTubeClient {
	return &MetaTubeClient{Server: strings.TrimRight(server, "/"), HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (m *MetaTubeClient) Name() string { return "metatube" }

type metaTubeSearchResp struct {
	Data []struct {
		ID          string `json:"id"`       // 用于 Detail 查询，如 midv00192
		Number      string `json:"number"`   // 展示用，如 MIDV-192
		Title       string `json:"title"`
		Provider    string `json:"provider"`
		CoverURL    string `json:"cover_url"`
		ReleaseDate string `json:"release_date"`
	} `json:"data"`
}

type metaTubeDetailResp struct {
	Data struct {
		ID            string   `json:"id"`
		Number        string   `json:"number"`
		Title         string   `json:"title"`
		Summary       string   `json:"summary"`
		Provider      string   `json:"provider"`
		Director      string   `json:"director"`
		Actors        []string `json:"actors"`
		CoverURL      string   `json:"cover_url"`
		BackdropURL   string   `json:"backdrop_url"`
		PreviewImages []string `json:"preview_images"`
		PreviewVideo  string   `json:"preview_video_url"`
		Maker         string   `json:"maker"`
		Label         string   `json:"label"`
		Series        string   `json:"series"`
		Genres        []string `json:"genres"`
		Runtime       int      `json:"runtime"`
		ReleaseDate   string   `json:"release_date"`
		Score         float64  `json:"score"`
		Homepage      string   `json:"homepage"`
	} `json:"data"`
}

func (m *MetaTubeClient) Search(code string) ([]*ScrapeResult, error) {
	u := fmt.Sprintf("%s/v1/movies/search?q=%s", m.Server, url.QueryEscape(code))
	resp, err := m.HTTP.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var sr metaTubeSearchResp
	if err := json.Unmarshal(body, &sr); err != nil {
		return nil, fmt.Errorf("metatube search decode: %w", err)
	}

	var out []*ScrapeResult
	for _, item := range sr.Data {
		out = append(out, &ScrapeResult{
			Code:        item.Number,
			Title:       item.Title,
			Poster:      item.CoverURL,
			ReleaseDate: item.ReleaseDate,
			Source:      "metatube:" + item.Provider,
			// ===== 关键：用 Search 返回的 id（midv00192），不是 number（MIDV-192）
			ProviderID:  item.Provider + "/" + item.ID,
			HasChinese:  !isJapanese(item.Title) && containsChinese(item.Title),
		})
	}
	return out, nil
}

func (m *MetaTubeClient) Detail(code string, providerID string) (*ScrapeResult, error) {
	u := fmt.Sprintf("%s/v1/movies/%s", m.Server, providerID)
	resp, err := m.HTTP.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var dr metaTubeDetailResp
	if err := json.Unmarshal(body, &dr); err != nil {
		return nil, fmt.Errorf("metatube detail decode: %w", err)
	}

	r := &ScrapeResult{
		Code:          dr.Data.Number,
		Title:         dr.Data.Title,
		OriginalTitle: dr.Data.Title,
		Plot:          dr.Data.Summary,
		Runtime:       dr.Data.Runtime,
		ReleaseDate:   dr.Data.ReleaseDate,
		Director:      dr.Data.Director,
		Studio:        dr.Data.Maker,
		Label:         dr.Data.Label,
		Series:        dr.Data.Series,
		Genres:        dr.Data.Genres,
		Poster:        dr.Data.CoverURL,
		Fanart:        dr.Data.BackdropURL,
		PreviewImages: dr.Data.PreviewImages,
		Trailer:       dr.Data.PreviewVideo,
		Rating:        dr.Data.Score,
		Source:        "metatube:" + dr.Data.Provider,
		ProviderID:    providerID,
	}
	if dr.Data.Homepage != "" {
		r.Urls = append(r.Urls, dr.Data.Homepage)
	}
	for _, name := range dr.Data.Actors {
		r.Actors = append(r.Actors, Actor{Name: name})
	}
	r.HasChinese = !isJapanese(r.Title) && containsChinese(r.Title)
	return r, nil
}
