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
	if src.Size != 12345678 || src.Container != "mkv" || src.RunTimeTicks != int64(3600.5*10000000) {
		t.Errorf("容器信息不符合预期：%+v", src)
	}
	if len(src.MediaStreams) != 3 {
		t.Fatalf("期望 3 个流（视频/音频/字幕），实际 %d", len(src.MediaStreams))
	}

	video := src.MediaStreams[0]
	if video.Type != "Video" || !video.IsHDR || video.ColorSpace != "bt2020nc" || video.Level != 120 || video.DisplayTitle != "4K HDR 10 HEVC" {
		t.Errorf("视频流不符合预期：%+v", video)
	}
	// 标准字段名与浮点帧率：Emby 直接透传，不做二次转换（6 位小数，与官方一致）
	if !video.IsDefault || video.AverageFrameRate != 23.976025 || video.RealFrameRate != 0 {
		t.Errorf("视频流 disposition/帧率不符合预期：%+v", video)
	}
	// Emby 官方媒体详情字段：动态范围/扩展视频类型/显示语言
	if video.ExtendedVideoType != "Hdr10" || video.VideoRange != "HDR" {
		t.Errorf("视频 Emby 动态范围字段不符合预期：%+v", video)
	}

	// 音频：语言名 + 编码 + 声道布局
	audio := src.MediaStreams[1]
	if audio.Type != "Audio" || audio.DisplayTitle != "Japanese AAC 5.1" || !audio.IsDefault {
		t.Errorf("音频流不符合预期：%+v", audio)
	}
	if audio.DisplayLanguage != "Japanese" || audio.Language != "jpn" {
		t.Errorf("音频显示语言不符合预期：%+v", audio)
	}

	subtitle := src.MediaStreams[2]
	if subtitle.Type != "Subtitle" || subtitle.Codec != "ass" || subtitle.Language != "chi" || subtitle.Title != "简体中文" {
		t.Errorf("字幕流不符合预期：%+v", subtitle)
	}
	// 自定义标题（"简体中文"）优先于语言名：(强制) 后缀保留
	if !subtitle.IsForced || subtitle.DisplayTitle != "简体中文 (ASS) (强制)" || !subtitle.IsTextSubtitleStream || !subtitle.SupportsExternalStream {
		t.Errorf("字幕 forced/DisplayTitle 标记不符合预期：%+v", subtitle)
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

// mediaInfoOfVideo 只喂一条视频流，返回补全后的流信息。
func mediaInfoOfVideo(t *testing.T, videoJSON string) MediaInfoStream {
	t.Helper()
	raw := []byte(`{"format":{"size":"1000","duration":"60","bit_rate":"2000","format_name":"matroska,webm"},` +
		`"streams":[{"index":0,"codec_type":"video","codec_name":"hevc",` + videoJSON + `}]}`)
	out, err := BuildMediaInfoJSON(raw)
	if err != nil {
		t.Fatalf("生成失败：%v", err)
	}
	var result []MediaInfoOutput
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	return result[0].MediaSourceInfo.MediaStreams[0]
}

// 杜比视界的 profile 不在 codec/profile 字段里，只能从 side_data_list 读，
// 否则 DV 片源会被当成普通 HDR10。
func TestBuildMediaInfoJSON识别杜比视界(t *testing.T) {
	const hdrBase = `"width":3840,"height":2160,"pix_fmt":"yuv420p10le","color_space":"bt2020nc","color_transfer":"smpte2084","color_primaries":"bt2020"`
	cases := []struct {
		name       string
		extra      string
		wantRange  string
		wantType   string
		wantTitle  string
		wantExtTyp string // Emby ExtendedVideoType
		wantSubtyp string // Emby ExtendedVideoSubtype
	}{
		{"DV8 兼容 HDR10", `"codec_tag_string":"dvhe","side_data_list":[{"side_data_type":"DOVI configuration record","dv_version_major":1,"dv_profile":8,"dv_level":6,"rpu_present_flag":1,"bl_present_flag":1,"dv_bl_signal_compatibility_id":1}]`, "HDR", "DOVIWithHDR10", "4K Dolby Vision HEVC", "DolbyVision", "DoviProfile81"},
		{"DV8 with HDR10+", `"side_data_list":[{"side_data_type":"DOVI configuration record","dv_profile":8,"rpu_present_flag":1,"bl_present_flag":1,"dv_bl_signal_compatibility_id":1},{"side_data_type":"HDR Dynamic Metadata SMPTE2094-40 (HDR10+)"}]`, "HDR", "DOVIWithHDR10Plus", "4K Dolby Vision HEVC", "DolbyVision", "DoviProfile81"},
		{"DV5 纯杜比视界", `"side_data_list":[{"side_data_type":"DOVI configuration record","dv_profile":5,"rpu_present_flag":1,"bl_present_flag":1,"dv_bl_signal_compatibility_id":0}]`, "HDR", "DOVI", "4K Dolby Vision HEVC", "DolbyVision", "DoviProfile50"},
		{"DV7 带增强层", `"side_data_list":[{"side_data_type":"DOVI configuration record","dv_profile":7,"rpu_present_flag":1,"el_present_flag":1,"bl_present_flag":1,"dv_bl_signal_compatibility_id":6}]`, "HDR", "DOVIWithEL", "4K Dolby Vision HEVC", "DolbyVision", "DoviProfile76"},
		{"仅 HDR10+", `"side_data_list":[{"side_data_type":"HDR Dynamic Metadata SMPTE2094-40 (HDR10+)"}]`, "HDR", "HDR10Plus", "4K HDR 10+ HEVC", "Hdr10Plus", ""},
		{"普通 HDR10", ``, "HDR", "HDR10", "4K HDR 10 HEVC", "Hdr10", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			video := hdrBase
			if c.extra != "" {
				video += "," + c.extra
			}
			s := mediaInfoOfVideo(t, video)
			if s.VideoRange != c.wantRange || s.VideoRangeType != c.wantType {
				t.Errorf("得到 %s/%s，期望 %s/%s", s.VideoRange, s.VideoRangeType, c.wantRange, c.wantType)
			}
			if s.IsHDR != (c.wantRange == "HDR") {
				t.Errorf("IsHDR 与 VideoRange 不一致：%+v", s)
			}
			if s.DisplayTitle != c.wantTitle {
				t.Errorf("DisplayTitle 得到 %q，期望 %q", s.DisplayTitle, c.wantTitle)
			}
			if s.ExtendedVideoType != c.wantExtTyp {
				t.Errorf("ExtendedVideoType 得到 %q，期望 %q", s.ExtendedVideoType, c.wantExtTyp)
			}
			if s.ExtendedVideoSubtype != c.wantSubtyp {
				t.Errorf("ExtendedVideoSubtype 得到 %q，期望 %q", s.ExtendedVideoSubtype, c.wantSubtyp)
			}
			// DV 有效配置应带可读子类型描述（如 Profile 8.1）
			if c.wantSubtyp != "" && s.ExtendedVideoSubTypeDescription == "" {
				t.Errorf("DV 有效配置缺少 ExtendedVideoSubTypeDescription：%+v", s)
			}
		})
	}
}

// 色彩边界：HLG、10bit SDR 误判、DV 基流色彩不匹配、12bit 位深。
func TestBuildMediaInfoJSON色彩边界(t *testing.T) {
	hlg := mediaInfoOfVideo(t, `"width":3840,"height":2160,"color_space":"bt2020nc","color_transfer":"arib-std-b67","color_primaries":"bt2020"`)
	if hlg.VideoRangeType != "HLG" || !hlg.IsHDR {
		t.Errorf("HLG 判定错误：%+v", hlg)
	}

	// 旧实现按像素格式判 HDR，10bit SDR 会被误标
	sdr := mediaInfoOfVideo(t, `"width":1920,"height":1080,"pix_fmt":"yuv420p10le","color_transfer":"bt709"`)
	if sdr.IsHDR || sdr.VideoRangeType != "SDR" {
		t.Errorf("10bit SDR 被误判为 HDR：%+v", sdr)
	}

	// DV 头但基流不是 bt2020/PQ，按规范判无效
	bad := mediaInfoOfVideo(t, `"width":1920,"height":1080,"pix_fmt":"yuv420p10le","color_space":"bt709","color_transfer":"bt709","color_primaries":"bt709","side_data_list":[{"side_data_type":"DOVI configuration record","dv_profile":8,"rpu_present_flag":1,"bl_present_flag":1,"dv_bl_signal_compatibility_id":1}]`)
	if bad.VideoRangeType != "DOVIInvalid" {
		t.Errorf("DV 基流色彩校验未生效：%+v", bad)
	}

	deep := mediaInfoOfVideo(t, `"width":3840,"height":2160,"pix_fmt":"yuv420p12le","color_transfer":"smpte2084"`)
	if deep.BitDepth != 12 {
		t.Errorf("12bit 位深错误：%d", deep.BitDepth)
	}
}

// 用与用户实际片源一致的 ffprobe 原始输入验证端到端输出。
func TestE2E真实DV8片源(t *testing.T) {
	raw := []byte(`{
		"format":{"size":"19046511596","duration":"9742.524","bit_rate":"15639899","format_name":"matroska,webm"},
		"streams":[
			{"index":0,"codec_type":"video","codec_name":"hevc","codec_tag_string":"dvhe","width":3840,"height":2160,"pix_fmt":"yuv420p10le","profile":"Main 10","level":150,"color_space":"bt2020nc","color_transfer":"smpte2084","color_primaries":"bt2020","avg_frame_rate":"24000/1001","bit_rate":"14000000","disposition":{"default":1},
			 "side_data_list":[{"side_data_type":"DOVI configuration record","dv_version_major":1,"dv_profile":8,"dv_level":6,"rpu_present_flag":1,"bl_present_flag":1,"dv_bl_signal_compatibility_id":1}]},
			{"index":1,"codec_type":"audio","codec_name":"eac3","channels":6,"channel_layout":"5.1(side)","sample_rate":"48000","bit_rate":"640000","disposition":{"default":1},"tags":{"language":"ger","title":"German"}}
		]
	}`)
	out, err := BuildMediaInfoJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	var r []MediaInfoOutput
	json.Unmarshal(out, &r)
	src := r[0].MediaSourceInfo
	v := src.MediaStreams[0]
	a := src.MediaStreams[1]
	t.Logf("Container=%s", src.Container)
	t.Logf("视频 DisplayTitle=%s VideoRangeType=%s IsDefault=%v AvgFPS=%.3f IsHDR=%v",
		v.DisplayTitle, v.VideoRangeType, v.IsDefault, v.AverageFrameRate, v.IsHDR)
	t.Logf("音频 DisplayTitle=%s IsDefault=%v", a.DisplayTitle, a.IsDefault)
	if src.Container != "mkv" {
		t.Errorf("容器错误")
	}
	if v.DisplayTitle != "4K Dolby Vision HEVC" {
		t.Errorf("DisplayTitle 错误")
	}
	if v.VideoRangeType != "DOVIWithHDR10" {
		t.Errorf("VideoRangeType 错误")
	}
	// 标准字段：视频（6 位小数帧率，与截图一致：23.976025）
	if !v.IsDefault || v.AverageFrameRate != 23.976025 || !v.IsHDR || v.Protocol != "File" {
		t.Errorf("视频标准字段错误：%+v", v)
	}
	// Emby 官方媒体详情字段：动态范围 DolbyVision / Profile 8.1 / 显示语言
	if v.ExtendedVideoType != "DolbyVision" || v.ExtendedVideoSubtype != "DoviProfile81" ||
		v.ExtendedVideoSubTypeDescription != "Profile 8.1" {
		t.Errorf("视频 Emby 扩展字段错误：%+v", v)
	}
	if a.DisplayLanguage != "German" {
		t.Errorf("音频显示语言错误：%+v", a)
	}
	// 音频：Title="German" 与语言名一致 → "German EAC3 5.1(side)"
	if a.DisplayTitle != "German EAC3 5.1(side)" || !a.IsDefault || a.Protocol != "File" {
		t.Errorf("音频标准字段错误：%+v", a)
	}
}
