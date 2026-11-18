package scrape

import (
	"testing"

	"qmediasync/internal/models"
)

// 构造一个带完整 ffprobe 缓存（首次刮削已探测过）的媒体文件
func cachedMediaFile() *models.ScrapeMediaFile {
	return &models.ScrapeMediaFile{
		VideoCodecJson: "{}",
		AudioCodecJson: "[]",
		Resolution:     "1080p",
	}
}

func TestShouldSkipFFprobe(t *testing.T) {
	tests := []struct {
		name string
		file *models.ScrapeMediaFile
		want bool
	}{
		{
			name: "缓存完整且首次刮削：跳过重复探测",
			file: cachedMediaFile(),
			want: true,
		},
		{
			name: "缓存完整但重新刮削：强制重新探测",
			file: func() *models.ScrapeMediaFile {
				f := cachedMediaFile()
				f.IsReScrape = true
				return f
			}(),
			want: false,
		},
		{
			name: "缓存不完整：需要探测",
			file: &models.ScrapeMediaFile{
				VideoCodecJson: "{}",
				AudioCodecJson: "[]",
				// Resolution 为空
			},
			want: false,
		},
		{
			name: "无任何缓存：需要探测",
			file: &models.ScrapeMediaFile{},
			want: false,
		},
		{
			name: "重新刮削且无缓存：需要探测",
			file: &models.ScrapeMediaFile{IsReScrape: true},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldSkipFFprobe(tt.file); got != tt.want {
				t.Errorf("shouldSkipFFprobe() = %v, want %v", got, tt.want)
			}
		})
	}
}
