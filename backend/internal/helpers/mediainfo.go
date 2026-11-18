package helpers

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// MediaInfoStream 媒体流信息，对齐 Emby/Jellyfin 的 MediaStream。
// 字段名沿用 avscrape 既有输出（大写驼峰），保证媒体服务器及既有消费方兼容。
type MediaInfoStream struct {
	Index          int    `json:"Index"`
	Type           string `json:"Type"`
	Codec          string `json:"Codec,omitempty"`
	Language       string `json:"Language,omitempty"`
	Title          string `json:"Title,omitempty"`
	Width          int64  `json:"Width,omitempty"`
	Height         int64  `json:"Height,omitempty"`
	BitDepth       int    `json:"BitDepth,omitempty"`
	PixelFormat    string `json:"PixelFormat,omitempty"`
	Profile        string `json:"Profile,omitempty"`
	Level          int    `json:"Level,omitempty"`
	ColorSpace     string `json:"ColorSpace,omitempty"`
	ColorTransfer  string `json:"ColorTransfer,omitempty"`
	ColorPrimaries string `json:"ColorPrimaries,omitempty"`
	IsHDR          bool   `json:"IsHDR,omitempty"`
	BitRate        int64  `json:"BitRate,omitempty"`
	AvgFrameRate   string `json:"AvgFrameRate,omitempty"`
	Channels       int64  `json:"Channels,omitempty"`
	ChannelLayout  string `json:"ChannelLayout,omitempty"`
	SampleRate     int64  `json:"SampleRate,omitempty"`
	Default        bool   `json:"Default,omitempty"`
	Forced         bool   `json:"Forced,omitempty"`
	DisplayTitle   string `json:"DisplayTitle,omitempty"`
}

// MediaSourceInfo 媒体源信息，对齐 Emby/Jellyfin 的 MediaSourceInfo。
type MediaSourceInfo struct {
	Size         int64             `json:"Size"`
	RunTimeTicks int64             `json:"RunTimeTicks"`
	Bitrate      int64             `json:"Bitrate"`
	Container    string            `json:"Container,omitempty"`
	MediaStreams []MediaInfoStream `json:"MediaStreams"`
}

// MediaInfoOutput 对应 avscrape 既有的外层结构：[{"MediaSourceInfo": {...}}]
type MediaInfoOutput struct {
	MediaSourceInfo MediaSourceInfo `json:"MediaSourceInfo"`
}

// BuildMediaInfoJSON 从 ffprobe 原始 JSON 生成补全版媒体信息 JSON。
// 输出为 Jellyfin/Emby 风格数组：[{"MediaSourceInfo": {...}}]。
// 相比原 avscrape 实现，补全了字幕流、HDR、帧率、色彩空间、disposition 等字段。
// 入参 rawJSON 为 ffprobe -print_format json -show_format -show_streams 的原始输出。
func BuildMediaInfoJSON(rawJSON []byte) ([]byte, error) {
	if len(rawJSON) == 0 {
		return nil, fmt.Errorf("ffprobe 原始数据为空")
	}
	var ff FFprobeJson
	if err := json.Unmarshal(rawJSON, &ff); err != nil {
		return nil, err
	}
	if len(ff.Streams) == 0 {
		return nil, fmt.Errorf("ffprobe 未返回任何流")
	}

	size, _ := strconv.ParseInt(ff.Format.Size, 10, 64)
	seconds, _ := strconv.ParseFloat(ff.Format.Duration, 64)
	runTimeTicks := int64(seconds * 10000000)
	bitrate, _ := strconv.ParseInt(ff.Format.BitRate, 10, 64)

	streams := make([]MediaInfoStream, 0, len(ff.Streams))
	for _, st := range ff.Streams {
		info := MediaInfoStream{Index: st.Index, Codec: st.CodecName}
		if st.Tags != nil {
			info.Language = st.Tags["language"]
			info.Title = st.Tags["title"]
		}
		if st.Disposition != nil {
			info.Default = st.Disposition["default"] == 1
			info.Forced = st.Disposition["forced"] == 1
		}
		streamBitrate, _ := strconv.ParseInt(st.BitRate, 10, 64)

		switch st.CodecType {
		case "video":
			bitDepth := 8
			if strings.Contains(st.PixelFormat, "10le") || strings.Contains(st.PixelFormat, "10be") ||
				strings.Contains(st.PixelFormat, "12le") || strings.Contains(st.PixelFormat, "12be") {
				bitDepth = 10
			}
			info.Type = "Video"
			info.Width = st.Width
			info.Height = st.Height
			info.BitDepth = bitDepth
			info.PixelFormat = st.PixelFormat
			info.Profile = st.Profile
			info.Level = st.Level
			info.ColorSpace = st.ColorSpace
			info.ColorTransfer = st.ColorTransfer
			info.ColorPrimaries = st.ColorPrimaries
			info.IsHDR = IsHDRFormat(st.PixelFormat)
			info.BitRate = streamBitrate
			info.AvgFrameRate = st.AvgFrameRate
			info.DisplayTitle = mediainfoDisplayName(st.Width, st.Height, st.CodecName, bitDepth)
		case "audio":
			info.Type = "Audio"
			info.Channels = st.Channels
			info.ChannelLayout = st.ChannelLayout
			info.SampleRate, _ = strconv.ParseInt(st.SampleRate, 10, 64)
			info.BitRate = streamBitrate
			if info.ChannelLayout != "" {
				info.DisplayTitle = fmt.Sprintf("%s %s", strings.ToUpper(st.CodecName), info.ChannelLayout)
			} else {
				info.DisplayTitle = strings.ToUpper(st.CodecName)
			}
		case "subtitle":
			info.Type = "Subtitle"
			if info.Title == "" {
				info.Title = info.Language
			}
			info.DisplayTitle = strings.ToUpper(st.CodecName)
		default:
			continue
		}
		streams = append(streams, info)
	}

	if len(streams) == 0 {
		return nil, fmt.Errorf("ffprobe 未返回可识别的音视频/字幕流")
	}

	source := MediaSourceInfo{
		Size:         size,
		RunTimeTicks: runTimeTicks,
		Bitrate:      bitrate,
		Container:    ff.Format.FormatName,
		MediaStreams: streams,
	}
	return json.MarshalIndent([]MediaInfoOutput{{MediaSourceInfo: source}}, "", "  ")
}

// mediainfoDisplayName 生成分辨率/编码/位深展示标题，如 1080p HEVC 10bit。
func mediainfoDisplayName(width, height int64, codec string, bitDepth int) string {
	base := width
	if height > base {
		base = height
	}
	quality := "SD"
	if base >= 2160 {
		quality = "2160p"
	} else if base >= 1080 {
		quality = "1080p"
	} else if base >= 720 {
		quality = "720p"
	}
	return fmt.Sprintf("%s %s %dbit", quality, strings.ToUpper(codec), bitDepth)
}
