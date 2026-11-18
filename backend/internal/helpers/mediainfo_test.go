package helpers

import (
	"encoding/json"
	"testing"
)

// 直接验证 rawJSON 输入路径（avscrape/aven 使用），确保字幕流与 HDR/色彩等被补全。
func TestBuildMediaInfoJSON补全字幕与HDR(t *testing.T) {
	raw := []byte(`{
		"format": {"size":"12345678","duration":"3600.5","bit_rate":"5000000","format_name":"matroska,webm"},
		"streams": [
			{"index":0,"codec_type":"video","codec_name":"hevc","width":3840,"height":2160,"pix_fmt":"yuv420p10le","profile":"Main 10","level":120,"color_space":"bt2020nc","color_transfer":"smpte2084","color_primaries":"bt2020","avg_frame_rate":"24000/1001","bit_rate":"4000000","disposition":{"default":1,"forced":0},"tags":{"language":"und"}},
			{"index":1,"codec_type":"audio","codec_name":"aac","channels":6,"channel_layout":"5.1","sample_rate":"48000","bit_rate":"256000","disposition":{"default":1},"tags":{"language":"jpn"}},
			{"index":2,"codec_type":"subtitle","codec_name":"ass","disposition":{"default":0,"forced":1},"tags":{"language":"chi","title":"简体中文"}},
			{"index":3,"codec_type":"attachment","codec_name":"ttf","tags":{"filename":"font.ttf"}}
		]
	}`)

	out, err := BuildMediaInfoJSON(raw)
	if err != nil {
		t.Fatalf("生成失败：%v", err)
	}
	var result []MediaInfoOutput
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if len(result) != 1 {
		t.Fatalf("期望 1 个 MediaSourceInfo，实际 %d", len(result))
	}

	src := result[0].MediaSourceInfo
	if src.Size != 12345678 || src.Container != "matroska,webm" || src.RunTimeTicks != int64(3600.5*10000000) {
		t.Errorf("容器信息不符合预期：%+v", src)
	}
	if len(src.MediaStreams) != 3 {
		t.Fatalf("期望 3 个流（视频/音频/字幕），实际 %d", len(src.MediaStreams))
	}

	video := src.MediaStreams[0]
	if video.Type != "Video" || !video.IsHDR || video.ColorSpace != "bt2020nc" || video.Level != 120 || video.DisplayTitle != "2160p HEVC 10bit" {
		t.Errorf("视频流不符合预期：%+v", video)
	}
	if !video.Default || video.AvgFrameRate != "24000/1001" {
		t.Errorf("视频流 disposition/帧率不符合预期：%+v", video)
	}

	subtitle := src.MediaStreams[2]
	if subtitle.Type != "Subtitle" || subtitle.Codec != "ass" || subtitle.Language != "chi" || subtitle.Title != "简体中文" {
		t.Errorf("字幕流不符合预期：%+v", subtitle)
	}
	if !subtitle.Forced {
		t.Errorf("字幕 forced 标记不符合预期：%+v", subtitle)
	}
}

func TestBuildMediaInfoJSON空数据返回错误(t *testing.T) {
	if _, err := BuildMediaInfoJSON(nil); err == nil {
		t.Error("空数据应返回错误")
	}
	onlyAttach := []byte(`{"format":{},"streams":[{"index":0,"codec_type":"attachment","codec_name":"ttf"}]}`)
	if _, err := BuildMediaInfoJSON(onlyAttach); err == nil {
		t.Error("仅有附件流时应返回错误")
	}
}
