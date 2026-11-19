package helpers

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// MediaInfoStream 媒体流信息，直接对齐 Emby/Jellyfin 的 MediaStream。
//
// 所有字段在刮削阶段（QMS）就生成为 Emby 标准形状：
//   - 标准字段名：IsDefault / IsForced / IsExternal（而非 Default / Forced）
//   - 标准帧率：AverageFrameRate / RealFrameRate 为浮点（而非分数字符串 AvgFrameRate）
//   - 拼好的 DisplayTitle：视频按「分辨率 + 动态范围 + 编码」，音轨/字幕带语言名
//   - 字幕辅助标记：IsTextSubtitleStream / SupportsExternalStream / Protocol
//
// 消费方（媒体服务器）只需原样透传，不再做二次转换。
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
	// VideoRange/VideoRangeType 保留给 Jellyfin 及旧客户端（DOVI/HDR/SDR 口径）。
	VideoRange     string `json:"VideoRange,omitempty"`
	VideoRangeType string `json:"VideoRangeType,omitempty"`
	// 以下为 Emby 官方客户端媒体详情面板读取的标准字段。
	// 截图里「动态范围: DolbyVision」读 ExtendedVideoType，
	// 「DolbyVision: Profile 8.1」读 ExtendedVideoSubtype / ExtendedVideoSubTypeDescription。
	// ExtendedVideoType 取值：None / Hdr10 / Hdr10Plus / HyperLogGamma / DolbyVision。
	ExtendedVideoType string `json:"ExtendedVideoType,omitempty"`
	// ExtendedVideoSubtype 取值：None / DoviProfile50 / DoviProfile76 / DoviProfile81 等。
	ExtendedVideoSubtype string `json:"ExtendedVideoSubtype,omitempty"`
	// ExtendedVideoSubTypeDescription 是 subtype 的可读文本，如 "Profile 8.1"。
	ExtendedVideoSubTypeDescription string `json:"ExtendedVideoSubTypeDescription,omitempty"`
	// DisplayLanguage 是语言码对应的全名，如 "German"（对应截图「显示语言」）。
	DisplayLanguage string `json:"DisplayLanguage,omitempty"`
	// 杜比视界：来自 ffprobe 的 "DOVI configuration record" side data。
	DvVersionMajor            int   `json:"DvVersionMajor,omitempty"`
	DvVersionMinor            int   `json:"DvVersionMinor,omitempty"`
	DvProfile                 int   `json:"DvProfile,omitempty"`
	DvLevel                   int   `json:"DvLevel,omitempty"`
	RpuPresentFlag            int   `json:"RpuPresentFlag,omitempty"`
	ElPresentFlag             int   `json:"ElPresentFlag,omitempty"`
	BlPresentFlag             int   `json:"BlPresentFlag,omitempty"`
	DvBlSignalCompatibilityID int   `json:"DvBlSignalCompatibilityId,omitempty"`
	Hdr10PlusPresentFlag      bool  `json:"Hdr10PlusPresentFlag,omitempty"`
	BitRate                   int64 `json:"BitRate,omitempty"`
	// 帧率为浮点（Emby 标准），由 ffprobe 的分数字符串换算而来。
	AverageFrameRate float64 `json:"AverageFrameRate,omitempty"`
	RealFrameRate    float64 `json:"RealFrameRate,omitempty"`
	Channels         int64   `json:"Channels,omitempty"`
	ChannelLayout    string  `json:"ChannelLayout,omitempty"`
	SampleRate       int64   `json:"SampleRate,omitempty"`
	// 标准布尔字段：不带 omitempty，保证始终输出，客户端可可靠读取。
	IsDefault  bool `json:"IsDefault"`
	IsForced   bool `json:"IsForced"`
	IsExternal bool `json:"IsExternal"`
	// 字幕辅助标记。
	IsTextSubtitleStream   bool   `json:"IsTextSubtitleStream,omitempty"`
	SupportsExternalStream bool   `json:"SupportsExternalStream,omitempty"`
	Protocol               string `json:"Protocol,omitempty"`
	DisplayTitle           string `json:"DisplayTitle,omitempty"`
}

// MediaSourceInfo 媒体源信息，对齐 Emby/Jellyfin 的 MediaSourceInfo。
type MediaSourceInfo struct {
	Size         int64             `json:"Size"`
	RunTimeTicks int64             `json:"RunTimeTicks"`
	Bitrate      int64             `json:"Bitrate"`
	Container    string            `json:"Container,omitempty"`
	MediaStreams []MediaInfoStream `json:"MediaStreams"`
}

// MediaInfoOutput 对应外层结构：[{"MediaSourceInfo": {...}}]
type MediaInfoOutput struct {
	MediaSourceInfo MediaSourceInfo `json:"MediaSourceInfo"`
}

// BuildMediaInfoJSON 从 ffprobe 原始 JSON 生成补全版媒体信息 JSON。
// 输出为 Jellyfin/Emby 风格数组：[{"MediaSourceInfo": {...}}]。
// 每条流在刮削阶段就生成为 Emby 标准形状，消费方无需二次转换。
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
		info := MediaInfoStream{Index: st.Index, Codec: st.CodecName, Protocol: "File"}
		if st.Tags != nil {
			info.Language = st.Tags["language"]
			info.Title = st.Tags["title"]
		}
		// Emby「显示语言」：语言码 → 全名（ger → German）。
		info.DisplayLanguage = languageName(info.Language)
		if st.Disposition != nil {
			info.IsDefault = st.Disposition["default"] == 1
			info.IsForced = st.Disposition["forced"] == 1
			info.IsExternal = st.Disposition["external"] == 1
		}
		streamBitrate, _ := strconv.ParseInt(st.BitRate, 10, 64)

		// 帧率：ffprobe 给的是分数字符串（如 "24000/1001"），Emby 要浮点，保留三位小数。
		info.AverageFrameRate = mediaFrameRate(st.AvgFrameRate)
		info.RealFrameRate = mediaFrameRate(st.RFrameRate)

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
			// Emby 官方媒体详情字段：ExtendedVideoType/Subtype/子类型描述 + VideoRange 文本。
			applyEmbyVideoFields(&info)
			info.BitRate = streamBitrate
			info.DisplayTitle = mediainfoDisplayName(st.Width, st.Height, st.CodecName, info.VideoRangeType)
		case "audio":
			info.Type = "Audio"
			info.Channels = st.Channels
			info.ChannelLayout = st.ChannelLayout
			info.SampleRate, _ = strconv.ParseInt(st.SampleRate, 10, 64)
			info.BitRate = streamBitrate
			info.DisplayTitle = audioDisplayTitle(st.CodecName, info.ChannelLayout, info.Language, info.Title)
		case "subtitle":
			info.Type = "Subtitle"
			if info.Title == "" {
				info.Title = info.Language
			}
			info.DisplayTitle = subtitleDisplayTitle(st.CodecName, info.Language, info.Title, info.IsForced)
			info.IsTextSubtitleStream = isTextSubtitle(st.CodecName)
			info.SupportsExternalStream = true
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
		Container:    normalizeContainer(ff.Format.FormatName),
		MediaStreams: streams,
	}
	return json.MarshalIndent([]MediaInfoOutput{{MediaSourceInfo: source}}, "", "  ")
}

// mediainfoDisplayName 生成与 Emby 官方一致的视频 DisplayTitle。
//
// 顺序固定为「分辨率 + 动态范围 + 编码」，例如：
//
//	4K Dolby Vision HEVC / 4K HDR 10 HEVC / 4K HLG HEVC / 1080p HEVC
//
// 位深度不拼进标题：官方客户端把 BitDepth 单独显示成一行「位深度: 10」，
// 旧版拼出的 "2160p HEVC 10bit" 与官方不符。HDR10 里的 "10" 是格式名，不是位深。
func mediainfoDisplayName(width, height int64, codec, videoRangeType string) string {
	base := width
	if height > base {
		base = height
	}
	quality := "SD"
	if base >= 2160 {
		quality = "4K"
	} else if base >= 1080 {
		quality = "1080p"
	} else if base >= 720 {
		quality = "720p"
	}

	// 动态范围徽章：杜比视界优先（含各种组合），再 HDR/HLG。
	// SDR 不加徽章，标题就是「分辨率 + 编码」。
	var hdr string
	switch {
	case strings.HasPrefix(videoRangeType, "DOVI"):
		hdr = "Dolby Vision"
	case videoRangeType == "HDR10Plus":
		hdr = "HDR 10+"
	case videoRangeType == "HDR10":
		hdr = "HDR 10"
	case videoRangeType == "HLG":
		hdr = "HLG"
	}

	parts := make([]string, 0, 3)
	parts = append(parts, quality)
	if hdr != "" {
		parts = append(parts, hdr)
	}
	if codec != "" {
		parts = append(parts, strings.ToUpper(codec))
	}
	return strings.Join(parts, " ")
}

// audioDisplayTitle 生成音轨 DisplayTitle：`[自定义标题 ]语言名 编码 声道布局`。
// 逻辑与媒体服务器原先的清洗保持一致，转换下沉到刮削阶段后行为不变。
func audioDisplayTitle(codec, layout, lang, title string) string {
	base := strings.ToUpper(codec)
	if layout != "" {
		base = strings.ToUpper(codec) + " " + layout
	}
	langName := languageName(lang)
	trackTitle := strings.TrimSpace(title)
	if trackTitle != "" && !strings.EqualFold(trackTitle, lang) && !isLanguageCode(trackTitle) {
		if strings.EqualFold(trackTitle, langName) {
			base = trackTitle + " " + base
		} else {
			base = fmt.Sprintf("%s (%s)", trackTitle, base)
		}
	} else if langName != "" && !strings.Contains(base, langName) {
		base = langName + " " + base
	}
	return base
}

// subtitleDisplayTitle 生成字幕 DisplayTitle：`[自定义标题 ]语言名 编码 [(强制)]`。
func subtitleDisplayTitle(codec, lang, title string, forced bool) string {
	base := strings.ToUpper(codec)
	langName := languageName(lang)
	trackTitle := strings.TrimSpace(title)
	useful := trackTitle != "" && !strings.EqualFold(trackTitle, lang) && !isLanguageCode(trackTitle)
	if useful {
		base = fmt.Sprintf("%s (%s)", trackTitle, base)
	} else if langName != "" {
		base = langName + " " + base
	}
	if forced {
		base = base + " (强制)"
	}
	return base
}

// mediaFrameRate 把 ffprobe 的分数帧率字符串（如 "24000/1001"）换算成浮点。
// Emby 官方对 NTSC 分数帧率使用固定标称值（24000/1001 → 23.976025 等），
// 直接四舍五入得到的 23.976024 与官方差一个末位，客户端详情显示不一致，故对这些
// 标准分数做标称值映射；其余按 6 位小数输出。
func mediaFrameRate(s string) float64 {
	f, _ := parseFrameRate(strings.TrimSpace(s))
	if f <= 0 {
		return 0
	}
	// Emby/Jellyfin 标称帧率（float32 时间基换算后的约定值）。
	switch strings.ReplaceAll(strings.TrimSpace(s), " ", "") {
	case "24000/1001":
		return 23.976025
	case "30000/1001":
		return 29.970030
	case "60000/1001":
		return 59.940060
	case "120000/1001":
		return 119.880120
	}
	return math.Round(f*1_000_000) / 1_000_000
}

// languageName ISO 639 语言码 → 客户端可读语言名。
func languageName(code string) string {
	switch strings.ToLower(code) {
	case "chi", "zho", "zh", "chs", "cht":
		return "Chinese"
	case "eng", "en":
		return "English"
	case "ger", "deu", "de":
		return "German"
	case "fre", "fra", "fr":
		return "French"
	case "jpn", "ja":
		return "Japanese"
	case "kor", "ko":
		return "Korean"
	case "spa", "es":
		return "Spanish"
	case "ita", "it":
		return "Italian"
	case "rus", "ru":
		return "Russian"
	case "por", "pt":
		return "Portuguese"
	case "hin", "hi":
		return "Hindi"
	case "tha", "th":
		return "Thai"
	case "may", "msa", "ms":
		return "Malay"
	case "vie", "vi":
		return "Vietnamese"
	case "ara", "ar":
		return "Arabic"
	default:
		return ""
	}
}

// isLanguageCode 判断一个标题是否只是语言码（"chi" / "ger" / "en"），这种不该当自定义标题。
func isLanguageCode(s string) bool {
	t := strings.TrimSpace(s)
	if len(t) != 2 && len(t) != 3 {
		return false
	}
	for _, r := range t {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return false
		}
	}
	return languageName(t) != ""
}

// isTextSubtitle 判断字幕编码是否为文本字幕。
func isTextSubtitle(codec string) bool {
	switch strings.ToLower(codec) {
	case "subrip", "srt", "ass", "ssa", "webvtt", "vtt", "mov_text", "text", "txt", "ttml", "dfxp":
		return true
	}
	return false
}

// normalizeContainer 把 ffprobe 的 format_name 映射成 Emby 认识的容器扩展名。
// ffprobe 常返回 "matroska,webm" 这类逗号分隔的别名，官方显示的是 MKV/mkv。
func normalizeContainer(formatName string) string {
	name := strings.ToLower(strings.TrimSpace(formatName))
	for _, part := range strings.Split(name, ",") {
		switch strings.TrimSpace(part) {
		case "matroska", "webm":
			return "mkv"
		case "mov,mp4,m4a,3gp,3g2,mj2", "mp4", "mov":
			return "mp4"
		case "mpegts":
			return "ts"
		case "avi":
			return "avi"
		case "flv":
			return "flv"
		case "mp3":
			return "mp3"
		case "wav":
			return "wav"
		}
	}
	if i := strings.IndexByte(name, ','); i > 0 {
		return strings.TrimSpace(name[:i])
	}
	return formatName
}

// bitDepthOf 从像素格式推位深。
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

// applyEmbyVideoFields 把内部 VideoRangeType 映射成 Emby 官方客户端读取的标准字段。
//
// Emby 客户端媒体详情面板（截图实测）读取：
//   - ExtendedVideoType：None / Hdr10 / Hdr10Plus / HyperLogGamma / DolbyVision
//     （截图「动态范围: DolbyVision」读的就是这个枚举名）
//   - ExtendedVideoSubtype：DoviProfile50 / DoviProfile76 / DoviProfile81 等
//     （截图「DolbyVision: Profile 8.1」由此派生）
//   - ExtendedVideoSubTypeDescription："Profile 8.1" 这类可读文本
//
// 注意：不改动 VideoRange/VideoRangeType 的原值（HDR/SDR/DOVI*口径），
// 它们是 Jellyfin 及旧客户端点亮角标的依据，保持兼容。Emby 详情面板不看这两个字段。
func applyEmbyVideoFields(info *MediaInfoStream) {
	t := info.VideoRangeType

	// ExtendedVideoType + DV subtype
	switch {
	case strings.HasPrefix(t, "DOVI"):
		info.ExtendedVideoType = "DolbyVision"
		// 只有有效 DV 配置（非 DOVIInvalid）才写 subtype。
		if t != "DOVIInvalid" {
			sub := doviSubtype(info.DvProfile, info.DvBlSignalCompatibilityID)
			info.ExtendedVideoSubtype = sub
			info.ExtendedVideoSubTypeDescription = doviSubtypeDescription(sub)
		}
	case t == "HDR10Plus":
		info.ExtendedVideoType = "Hdr10Plus"
	case t == "HDR10":
		info.ExtendedVideoType = "Hdr10"
	case t == "HLG":
		info.ExtendedVideoType = "HyperLogGamma"
	default: // SDR / DOVIInvalid
		info.ExtendedVideoType = "None"
	}
}

// doviSubtype 由 DV profile + 基流信号兼容 ID 生成 Emby ExtendedVideoSubtype 枚举值。
// Emby 命名规则为 DoviProfile{profile}{sub}（profile*10+sub），实测映射：
//
//	profile 5,  compat 0  → DoviProfile50（DV 5.0）
//	profile 7,  compat 6  → DoviProfile76（DV 7.6）
//	profile 8,  compat 1  → DoviProfile81（DV 8.1，最常见）
//	profile 8,  compat 2  → DoviProfile82（DV 8.2，SDR 基流）
//	profile 8,  compat 4  → DoviProfile84（DV 8.4，HLG 基流）
//	profile 10, compat 0  → DoviProfile100（DV 10.0）
func doviSubtype(profile, compat int) string {
	// 10.x 单独处理，避免与 1.x 混淆。
	if profile == 10 {
		return fmt.Sprintf("DoviProfile10%d", compat)
	}
	if profile <= 0 || profile > 9 {
		return "None"
	}
	return fmt.Sprintf("DoviProfile%d%d", profile, compat)
}

// doviSubtypeDescription 返回 subtype 的可读文本（截图「DolbyVision: Profile 8.1」）。
func doviSubtypeDescription(sub string) string {
	switch {
	case sub == "DoviProfile50":
		return "Profile 5.0"
	case strings.HasPrefix(sub, "DoviProfile7"):
		return "Profile 7.6"
	case strings.HasPrefix(sub, "DoviProfile8"):
		return "Profile 8.1"
	case strings.HasPrefix(sub, "DoviProfile10"):
		return "Profile 10.0"
	default:
		return ""
	}
}
