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
	CodecTag       string `json:"CodecTag,omitempty"`
	Language       string `json:"Language,omitempty"`
	Title          string `json:"Title,omitempty"`
	Width          int64  `json:"Width,omitempty"`
	Height         int64  `json:"Height,omitempty"`
	BitDepth       int    `json:"BitDepth,omitempty"`
	PixelFormat    string `json:"PixelFormat,omitempty"`
	Profile        string `json:"Profile,omitempty"`
	Level          int    `json:"Level,omitempty"`
	RefFrames      int    `json:"RefFrames,omitempty"`
	ColorSpace     string `json:"ColorSpace,omitempty"`
	ColorTransfer  string `json:"ColorTransfer,omitempty"`
	ColorPrimaries string `json:"ColorPrimaries,omitempty"`
	ColorRange     string `json:"ColorRange,omitempty"`
	AspectRatio    string `json:"AspectRatio,omitempty"`
	IsInterlaced   bool   `json:"IsInterlaced,omitempty"`
	IsHDR          bool   `json:"IsHDR,omitempty"`
	// VideoRange/VideoRangeType 是 Emby 客户端点亮 HDR / 杜比视界角标的唯一依据，
	// 只给色彩三元组不够。取值与 Jellyfin 的 VideoRange/VideoRangeType 枚举一致。
	VideoRange     string `json:"VideoRange,omitempty"`
	VideoRangeType string `json:"VideoRangeType,omitempty"`
	// 杜比视界：来自 ffprobe 的 "DOVI configuration record" side data
	DvVersionMajor            int    `json:"DvVersionMajor,omitempty"`
	DvVersionMinor            int    `json:"DvVersionMinor,omitempty"`
	DvProfile                 int    `json:"DvProfile,omitempty"`
	DvLevel                   int    `json:"DvLevel,omitempty"`
	RpuPresentFlag            int    `json:"RpuPresentFlag,omitempty"`
	ElPresentFlag             int    `json:"ElPresentFlag,omitempty"`
	BlPresentFlag             int    `json:"BlPresentFlag,omitempty"`
	DvBlSignalCompatibilityID int    `json:"DvBlSignalCompatibilityId,omitempty"`
	Hdr10PlusPresentFlag      bool   `json:"Hdr10PlusPresentFlag,omitempty"`
	BitRate                   int64  `json:"BitRate,omitempty"`
	AvgFrameRate              string `json:"AvgFrameRate,omitempty"`
	Channels                  int64  `json:"Channels,omitempty"`
	ChannelLayout             string `json:"ChannelLayout,omitempty"`
	SampleRate                int64  `json:"SampleRate,omitempty"`
	Default                   bool   `json:"Default,omitempty"`
	Forced                    bool   `json:"Forced,omitempty"`
	DisplayTitle              string `json:"DisplayTitle,omitempty"`
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
			info.Type = "Video"
			info.Width = st.Width
			info.Height = st.Height
			info.BitDepth = bitDepthOf(st.PixelFormat)
			info.PixelFormat = st.PixelFormat
			info.Profile = st.Profile
			info.Level = st.Level
			info.RefFrames = st.Refs
			info.ColorSpace = st.ColorSpace
			info.ColorTransfer = st.ColorTransfer
			info.ColorPrimaries = st.ColorPrimaries
			info.ColorRange = st.ColorRange
			info.AspectRatio = st.DisplayAspectRatio
			info.IsInterlaced = st.FieldOrder != "" && !strings.EqualFold(st.FieldOrder, "progressive")
			info.CodecTag = st.CodecTagString
			applyDolbyVision(&info, st.SideDataList)
			info.VideoRange, info.VideoRangeType = videoRangeOf(info)
			info.IsHDR = info.VideoRange == "HDR"
			info.BitRate = streamBitrate
			info.AvgFrameRate = st.AvgFrameRate
			info.DisplayTitle = mediainfoDisplayName(st.Width, st.Height, st.CodecName, info.BitDepth)
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

// bitDepthOf 从像素格式推位深。之前 12bit 被算成 10bit，HDR 片源的位深显示不准。
func bitDepthOf(pixelFormat string) int {
	p := strings.ToLower(pixelFormat)
	switch {
	case strings.Contains(p, "12le"), strings.Contains(p, "12be"), strings.Contains(p, "p012"), strings.Contains(p, "p016"):
		return 12
	case strings.Contains(p, "10le"), strings.Contains(p, "10be"), strings.Contains(p, "p010"):
		return 10
	default:
		return 8
	}
}

// applyDolbyVision 把 ffprobe 的 side_data_list 落到流字段上。
//
// 杜比视界片的 codec/profile 和普通 HDR10 完全一样（都是 hevc Main 10），
// 只有这段 "DOVI configuration record" 能区分，漏了就只能当 HDR10 处理。
func applyDolbyVision(info *MediaInfoStream, sideData []FFprobeSideData) {
	for _, sd := range sideData {
		name := strings.ToUpper(sd.SideDataType)
		if strings.Contains(name, "SMPTE2094-40") || strings.Contains(name, "HDR10+") {
			info.Hdr10PlusPresentFlag = true
		}
		if strings.Contains(name, "DOVI") || strings.Contains(name, "DOLBY VISION") {
			info.DvVersionMajor = sd.DvVersionMajor
			info.DvVersionMinor = sd.DvVersionMinor
			info.DvProfile = sd.DvProfile
			info.DvLevel = sd.DvLevel
			info.RpuPresentFlag = sd.RpuPresentFlag
			info.ElPresentFlag = sd.ElPresentFlag
			info.BlPresentFlag = sd.BlPresentFlag
			info.DvBlSignalCompatibilityID = sd.DvBlSignalCompatibilityID
		}
	}
}

// videoRangeOf 推导 Emby 的 VideoRange / VideoRangeType。
//
// 规则与 Jellyfin MediaStream.GetVideoColorRange() 对齐：
// 是否 HDR 看 color_transfer（smpte2084=PQ、arib-std-b67=HLG），
// 杜比视界再看 profile 与 bl_signal_compatibility_id，基流色彩不匹配判 DOVIInvalid。
func videoRangeOf(s MediaInfoStream) (string, string) {
	isPQ := strings.EqualFold(s.ColorTransfer, "smpte2084")
	isHLG := strings.EqualFold(s.ColorTransfer, "arib-std-b67")
	base := "SDR"
	if isPQ || isHLG {
		base = "HDR"
	}

	profileOK := s.DvProfile == 5 || s.DvProfile == 7 || s.DvProfile == 8 || s.DvProfile == 10
	flagOK := s.RpuPresentFlag == 1 && s.BlPresentFlag == 1 &&
		(s.DvBlSignalCompatibilityID == 0 || s.DvBlSignalCompatibilityID == 1 ||
			s.DvBlSignalCompatibilityID == 2 || s.DvBlSignalCompatibilityID == 4 ||
			s.DvBlSignalCompatibilityID == 6)
	tag := strings.ToLower(s.CodecTag)
	tagDV := tag == "dovi" || tag == "dvh1" || tag == "dvhe" || tag == "dav1"

	if (profileOK && flagOK) || tagDV {
		var rng, typ string
		switch s.DvProfile {
		case 5:
			rng, typ = "HDR", "DOVI"
		case 7:
			rng, typ = "HDR", "DOVIWithEL"
		case 8:
			switch s.DvBlSignalCompatibilityID {
			case 1:
				rng, typ = "HDR", "DOVIWithHDR10"
			case 4:
				rng, typ = "HDR", "DOVIWithHLG"
			case 2:
				rng, typ = "SDR", "DOVIWithSDR"
			default:
				return base, "DOVIInvalid"
			}
		case 10:
			switch s.DvBlSignalCompatibilityID {
			case 0:
				rng, typ = "HDR", "DOVI"
			case 1:
				rng, typ = "HDR", "DOVIWithHDR10"
			case 2:
				rng, typ = "SDR", "DOVIWithSDR"
			case 4:
				rng, typ = "HDR", "DOVIWithHLG"
			default:
				return base, "DOVIInvalid"
			}
		default:
			// 只靠 codec tag 认出是 DV、profile 未知，基流是什么就是什么
			rng, typ = base, "DOVI"
		}

		expectTransfer := ""
		switch typ {
		case "DOVIWithHDR10", "DOVIWithEL":
			expectTransfer = "smpte2084"
		case "DOVIWithHLG":
			expectTransfer = "arib-std-b67"
		}
		if expectTransfer != "" {
			if !strings.EqualFold(s.ColorSpace, "bt2020nc") ||
				!strings.EqualFold(s.ColorPrimaries, "bt2020") ||
				!strings.EqualFold(s.ColorTransfer, expectTransfer) {
				return base, "DOVIInvalid"
			}
		}

		if s.Hdr10PlusPresentFlag {
			switch typ {
			case "DOVIWithHDR10":
				return "HDR", "DOVIWithHDR10Plus"
			case "DOVIWithEL":
				return "HDR", "DOVIWithELHDR10Plus"
			}
		}
		return rng, typ
	}

	if isPQ {
		if s.Hdr10PlusPresentFlag {
			return "HDR", "HDR10Plus"
		}
		return "HDR", "HDR10"
	}
	if isHLG {
		return "HDR", "HLG"
	}
	return "SDR", "SDR"
}
