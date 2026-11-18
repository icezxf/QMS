package scrape

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"qmediasync/internal/helpers"
	"qmediasync/internal/models"
)

// 构造一份包含视频、音频、内封字幕流的 ffprobe 数据
func sampleFFprobeJson() *helpers.FFprobeJson {
	return &helpers.FFprobeJson{
		Format: helpers.FFprobeFormat{
			Size:       "12345678",
			Duration:   "3600.5",
			BitRate:    "5000000",
			FormatName: "matroska,webm",
		},
		Streams: []helpers.FFprobeStream{
			{
				Index: 0, CodecType: "video", CodecName: "hevc",
				Width: 3840, Height: 2160, PixelFormat: "yuv420p10le",
				Profile: "Main 10", Level: 120, ColorSpace: "bt2020nc",
				ColorTransfer: "smpte2084", ColorPrimaries: "bt2020",
				AvgFrameRate: "24000/1001", BitRate: "4000000",
				Disposition: map[string]int{"default": 1, "forced": 0},
				Tags:        map[string]string{"language": "und"},
			},
			{
				Index: 1, CodecType: "audio", CodecName: "aac",
				Channels: 6, ChannelLayout: "5.1", SampleRate: "48000",
				BitRate:     "256000",
				Disposition: map[string]int{"default": 1},
				Tags:        map[string]string{"language": "jpn"},
			},
			{
				Index: 2, CodecType: "subtitle", CodecName: "ass",
				Disposition: map[string]int{"default": 0, "forced": 0},
				Tags:        map[string]string{"language": "chi", "title": "简体中文"},
			},
			{
				Index: 3, CodecType: "attachment", CodecName: "ttf",
				Tags: map[string]string{"filename": "font.ttf"},
			},
		},
	}
}

func TestWriteMediaInfoJSON生成完整媒体信息(t *testing.T) {
	tmpDir := t.TempDir()
	mediaFile := &models.ScrapeMediaFile{FFprobeJson: sampleFFprobeJson()}
	s := &ScrapeBase{}

	fileName := s.WriteMediaInfoJSON(mediaFile, tmpDir, "movie.name")
	if fileName != "movie.name-mediainfo.json" {
		t.Fatalf("文件名不符合预期：%s", fileName)
	}

	content, err := os.ReadFile(filepath.Join(tmpDir, fileName))
	if err != nil {
		t.Fatalf("读取生成的 json 失败：%v", err)
	}

	var result []helpers.MediaInfoOutput
	if err := json.Unmarshal(content, &result); err != nil {
		t.Fatalf("解析生成的 json 失败：%v", err)
	}
	if len(result) != 1 {
		t.Fatalf("期望 1 个 MediaSourceInfo，实际 %d", len(result))
	}

	source := result[0].MediaSourceInfo
	if source.Size != 12345678 || source.Bitrate != 5000000 || source.Container != "mkv" {
		t.Errorf("容器信息不符合预期：%+v", source)
	}
	if source.RunTimeTicks != int64(3600.5*10000000) {
		t.Errorf("运行时长 ticks 不符合预期：%d", source.RunTimeTicks)
	}

	// 视频流：分辨率、位深、HDR、等级、展示标题
	if len(source.MediaStreams) != 3 {
		t.Fatalf("期望 3 个流（视频/音频/字幕），实际 %d", len(source.MediaStreams))
	}
	video := source.MediaStreams[0]
	if video.Type != "Video" || video.Width != 3840 || video.Height != 2160 || video.BitDepth != 10 {
		t.Errorf("视频流信息不符合预期：%+v", video)
	}
	if !video.IsHDR || video.Level != 120 || video.ColorSpace != "bt2020nc" {
		t.Errorf("HDR/等级/色彩空间不符合预期：%+v", video)
	}
	if video.DisplayTitle != "4K HDR 10 HEVC" {
		t.Errorf("视频展示标题不符合预期：%s", video.DisplayTitle)
	}

	// 音频流：声道、语言、采样率
	audio := source.MediaStreams[1]
	if audio.Type != "Audio" || audio.Channels != 6 || audio.ChannelLayout != "5.1" || audio.Language != "jpn" || audio.SampleRate != 48000 {
		t.Errorf("音频流信息不符合预期：%+v", audio)
	}

	// 字幕流：这是 avscrape 缺失的部分，必须验证
	subtitle := source.MediaStreams[2]
	if subtitle.Type != "Subtitle" || subtitle.Codec != "ass" || subtitle.Language != "chi" || subtitle.Title != "简体中文" {
		t.Errorf("字幕流信息不符合预期：%+v", subtitle)
	}
}

func TestWriteMediaInfoJSON无数据时跳过(t *testing.T) {
	tmpDir := t.TempDir()
	s := &ScrapeBase{}

	if name := s.WriteMediaInfoJSON(&models.ScrapeMediaFile{}, tmpDir, "empty"); name != "" {
		t.Errorf("无 ffprobe 数据时应返回空文件名，实际：%s", name)
	}
	entries, _ := os.ReadDir(tmpDir)
	if len(entries) != 0 {
		t.Errorf("无数据时不应生成文件，实际生成了 %d 个", len(entries))
	}
}

func TestWriteMediaInfoJSON忽略未知流类型(t *testing.T) {
	ff := sampleFFprobeJson()
	// 只保留一个附件流，不应生成文件
	ff.Streams = []helpers.FFprobeStream{{Index: 0, CodecType: "attachment", CodecName: "ttf"}}
	tmpDir := t.TempDir()
	s := &ScrapeBase{}

	if name := s.WriteMediaInfoJSON(&models.ScrapeMediaFile{FFprobeJson: ff}, tmpDir, "only-attach"); name != "" {
		t.Errorf("只有附件流时应返回空文件名，实际：%s", name)
	}
}
