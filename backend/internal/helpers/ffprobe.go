package helpers

import (
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type FFprobeStream struct {
	Index              int               `json:"index"`
	CodecName          string            `json:"codec_name"`
	CodecLongName      string            `json:"codec_long_name"`
	CodecType          string            `json:"codec_type"`
	CodecTagString     string            `json:"codec_tag_string"`
	CodecTag           string            `json:"codec_tag"`
	RFrameRate         string            `json:"r_frame_rate"`
	AvgFrameRate       string            `json:"avg_frame_rate"`
	TimeBase           string            `json:"time_base"`
	StartTime          string            `json:"start_time"`
	Duration           string            `json:"duration"`
	DurationTS         float64           `json:"duration_ts"`
	Width              int64             `json:"width"`
	Height             int64             `json:"height"`
	Channels           int64             `json:"channels"`
	SampleRate         string            `json:"sample_rate"`
	PixelFormat        string            `json:"pix_fmt"`
	DisplayAspectRatio string            `json:"display_aspect_ratio"`
	BitRate            string            `json:"bit_rate"`
	NB_Frames          string            `json:"nb_frames"`
	Profile            string            `json:"profile"`             // 编码档次，如 Main 10
	Level              int               `json:"level"`               // 编码等级，如 120
	ChannelLayout      string            `json:"channel_layout"`      // 声道布局，如 5.1
	ColorSpace         string            `json:"color_space"`         // 色彩空间，如 bt2020nc
	ColorTransfer      string            `json:"color_transfer"`      // 色彩转换，如 smpte2084
	ColorPrimaries     string            `json:"color_primaries"`     // 原色，如 bt2020
	ColorRange         string            `json:"color_range"`         // 色彩范围，如 tv/pc
	FieldOrder         string            `json:"field_order"`         // 扫描方式，如 progressive
	SampleAspectRatio  string            `json:"sample_aspect_ratio"` // 像素宽高比，如 1:1
	Refs               int               `json:"refs"`                // 参考帧数量
	Disposition        map[string]int    `json:"disposition"`         // 流标记，如 default/forced
	SideDataList       []FFprobeSideData `json:"side_data_list"`      // 杜比视界/HDR10+ 元数据在这里
	Tags               map[string]string `json:"tags"`
}

// FFprobeSideData 对应 ffprobe 的 side_data_list 项。
//
// 杜比视界的 profile/level 不体现在 codec/profile 字段里（DV 片源的 profile 同样是
// "Main 10"），只能从 "DOVI configuration record" 这段 side data 读出来；HDR10+ 同理，
// 靠 side_data_type 里的 SMPTE2094-40 标识。
type FFprobeSideData struct {
	SideDataType              string `json:"side_data_type"`
	DvVersionMajor            int    `json:"dv_version_major"`
	DvVersionMinor            int    `json:"dv_version_minor"`
	DvProfile                 int    `json:"dv_profile"`
	DvLevel                   int    `json:"dv_level"`
	RpuPresentFlag            int    `json:"rpu_present_flag"`
	ElPresentFlag             int    `json:"el_present_flag"`
	BlPresentFlag             int    `json:"bl_present_flag"`
	DvBlSignalCompatibilityID int    `json:"dv_bl_signal_compatibility_id"`
}

type FFprobeFormat struct {
	Filename       string            `json:"filename"`
	NbStreams      int               `json:"nb_streams"`
	NbPrograms     int               `json:"nb_programs"`
	NbStreamGroups int               `json:"nb_stream_groups"`
	FormatName     string            `json:"format_name"`
	FormatLongName string            `json:"format_long_name"`
	StartTime      string            `json:"start_time"`
	Duration       string            `json:"duration"`
	Size           string            `json:"size"`
	BitRate        string            `json:"bit_rate"`
	ProbeScore     int               `json:"probe_score"`
	Tags           map[string]string `json:"tags"`
}
type FFprobeJson struct {
	Streams []FFprobeStream `json:"streams"`
	Format  FFprobeFormat   `json:"format"`
}

// 用 ffprobe 提取视频文件的视频、音频、字幕流信息
func GetFFprobeJson(videoUrl string) (*FFprobeJson, error) {
	AppLogger.Infof("ffprobe 查询 URL %s 的信息", videoUrl)
	cmd := exec.Command("ffprobe", "-v", "quiet", "-print_format", "json", "-show_streams", "-show_format", videoUrl)
	output, err := cmd.CombinedOutput()
	if err != nil {
		AppLogger.Errorf("执行 ffprobe 命令失败：%v", err)
		return nil, err
	}
	var ffprobeJson FFprobeJson
	if err := json.Unmarshal(output, &ffprobeJson); err != nil {
		AppLogger.Errorf("解析 ffprobe 输出失败：%v", err)
		return nil, err
	}
	return &ffprobeJson, nil
}

// isHDRFormat 检测是否为 HDR 格式
func IsHDRFormat(pixelFormat string) bool {
	hdrFormats := map[string]bool{
		"yuv420p10le": true,
		"yuv420p10be": true,
		"yuv422p10le": true,
		"yuv422p10be": true,
		"yuv444p10le": true,
		"yuv444p10be": true,
		"yuv420p12le": true,
		"yuv420p12be": true,
		"yuv422p12le": true,
		"yuv422p12be": true,
		"yuv444p12le": true,
		"yuv444p12be": true,
		"gbrp10le":    true,
		"gbrp10be":    true,
		"gbrp12le":    true,
		"gbrp12be":    true,
		"p010le":      true,
		"p010be":      true,
		"p012le":      true,
		"p012be":      true,
	}

	return hdrFormats[pixelFormat]
}

// 常见宽高比映射
var commonAspectRatios = map[string][]string{
	"16:9":  {"16:9", "1.78:1", "1.78"},
	"16:10": {"16:10", "1.6:1", "1.6"},
	"4:3":   {"4:3", "1.33:1", "1.33"},
	"21:9":  {"21:9", "2.33:1", "2.33", "64:27"},
	"18:9":  {"18:9", "2:1", "2.0"},
	"3:2":   {"3:2", "1.5:1", "1.5"},
	"1:1":   {"1:1", "1.0"},
}

// CalculateStandardAspectRatio 计算并返回标准化的宽高比
func CalculateStandardAspectRatio(width, height int) string {
	if width == 0 || height == 0 {
		return "Unknown"
	}

	// 计算精确宽高比
	exactRatio := float64(width) / float64(height)

	// 匹配最常见的宽高比
	return matchCommonAspectRatio(width, height, exactRatio)
}

// 匹配常见宽高比
func matchCommonAspectRatio(width, height int, exactRatio float64) string {
	// 容差范围
	tolerance := 0.05

	// 检查常见宽高比
	for standard := range commonAspectRatios {
		// 解析标准宽高比
		stdWidth, stdHeight := parseAspectRatio(standard)
		if stdWidth == 0 || stdHeight == 0 {
			continue
		}

		stdRatio := float64(stdWidth) / float64(stdHeight)

		// 检查是否在容差范围内
		if math.Abs(exactRatio-stdRatio) <= tolerance {
			return standard
		}
	}

	// 如果没有匹配到常见比例，返回计算出的简化比例
	gcd := calculateGCD(width, height)
	return fmt.Sprintf("%d:%d", width/gcd, height/gcd)
}

// 解析宽高比字符串
func parseAspectRatio(ratio string) (int, int) {
	var w, h int
	_, err := fmt.Sscanf(ratio, "%d:%d", &w, &h)
	if err != nil {
		return 0, 0
	}
	return w, h
}

// 计算最大公约数
func calculateGCD(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// ResolutionDetector 分辨率检测器
type ResolutionDetector struct {
	tolerance int // 容差像素
}

func NewResolutionDetector(tolerance int) *ResolutionDetector {
	return &ResolutionDetector{
		tolerance: tolerance,
	}
}

// VideoResolution 视频分辨率信息
type VideoResolution struct {
	StandardName string // 标准名称："1080p", "2160p" 等
	CommonName   string // 通用名称："Full HD", "4K" 等
	Width        int    // 宽度
	Height       int    // 高度
	IsStandard   bool   // 是否为标准分辨率
}

// 完整的分辨率标准库
var resolutionLibrary = []VideoResolution{
	// 8K 系列
	{StandardName: "4320p", CommonName: "8K UHD", Width: 7680, Height: 4320, IsStandard: true},
	{StandardName: "4320p", CommonName: "8K", Width: 7680, Height: 4320, IsStandard: true},

	// 5K 系列
	{StandardName: "2880p", CommonName: "5K", Width: 5120, Height: 2880, IsStandard: true},

	// 4K 系列
	{StandardName: "2160p", CommonName: "4K UHD", Width: 3840, Height: 2160, IsStandard: true},
	{StandardName: "2160p", CommonName: "4K DCI", Width: 4096, Height: 2160, IsStandard: true},
	{StandardName: "2160p", CommonName: "4K", Width: 3840, Height: 2160, IsStandard: true},

	// 2K 系列
	{StandardName: "1440p", CommonName: "2K QHD", Width: 2560, Height: 1440, IsStandard: true},
	{StandardName: "1080p", CommonName: "2K DCI", Width: 2048, Height: 1080, IsStandard: true},

	// 1080p 系列
	{StandardName: "1080p", CommonName: "Full HD", Width: 1920, Height: 1080, IsStandard: true},
	{StandardName: "1080p", CommonName: "FHD", Width: 1920, Height: 1080, IsStandard: true},

	// 720p 系列
	{StandardName: "720p", CommonName: "HD", Width: 1280, Height: 720, IsStandard: true},
	{StandardName: "720p", CommonName: "HD Ready", Width: 1280, Height: 720, IsStandard: true},

	// 480p 系列
	{StandardName: "480p", CommonName: "SD", Width: 640, Height: 480, IsStandard: true},
	{StandardName: "480p", CommonName: "NTSC", Width: 720, Height: 480, IsStandard: false},
	{StandardName: "480p", CommonName: "PAL", Width: 768, Height: 576, IsStandard: false},

	// 低分辨率
	{StandardName: "360p", CommonName: "nHD", Width: 640, Height: 360, IsStandard: true},
	{StandardName: "240p", CommonName: "240p", Width: 426, Height: 240, IsStandard: true},
	{StandardName: "144p", CommonName: "144p", Width: 256, Height: 144, IsStandard: true},
}

// DetectResolution 检测分辨率（增强版）
func (d *ResolutionDetector) DetectResolution(width, height int) *VideoResolution {
	// 优先精确匹配
	for _, resolution := range resolutionLibrary {
		if width == resolution.Width && height == resolution.Height {
			return &resolution
		}
	}

	// 容差匹配宽度
	for _, resolution := range resolutionLibrary {
		if math.Abs(float64(width)-float64(resolution.Width)) <= float64(d.tolerance) {
			// 检查宽高比是否合理
			expectedHeight := (width * resolution.Height) / resolution.Width
			if math.Abs(float64(height)-float64(expectedHeight)) <= float64(d.tolerance) {
				result := resolution
				result.Width = width
				result.Height = height
				result.IsStandard = false
				return &result
			}
		}
	}

	// 基于宽度和常见宽高比判断
	return d.estimateByWidthAndRatio(width, height)
}

// 基于宽度和宽高比估算
func (d *ResolutionDetector) estimateByWidthAndRatio(width, height int) *VideoResolution {
	aspectRatio := float64(width) / float64(height)

	var standardName, commonName string
	var estimatedHeight int

	// 根据宽高比分类
	switch {
	case aspectRatio >= 2.3: // 超宽屏（21:9）
		estimatedHeight = (width * 9) / 21
		commonName = "Ultra Wide "
	case aspectRatio >= 1.7: // 宽屏（16:9）
		estimatedHeight = (width * 9) / 16
		commonName = "Widescreen "
	case aspectRatio >= 1.5: // 3:2
		estimatedHeight = (width * 2) / 3
		commonName = "3:2 "
	case aspectRatio >= 1.3: // 4:3
		estimatedHeight = (width * 3) / 4
		commonName = "4:3 "
	default: // 其他比例
		estimatedHeight = height
		commonName = "Custom "
	}

	// 确定标准高度分类
	switch {
	case estimatedHeight >= 2000:
		standardName = "2160p"
		commonName += "4K"
	case estimatedHeight >= 1000:
		standardName = "1080p"
		commonName += "Full HD"
	case estimatedHeight >= 700:
		standardName = "720p"
		commonName += "HD"
	case estimatedHeight >= 400:
		standardName = "480p"
		commonName += "SD"
	case estimatedHeight >= 300:
		standardName = "360p"
		commonName += "nHD"
	default:
		standardName = "240p"
		commonName += "Low Res"
	}

	return &VideoResolution{
		StandardName: standardName,
		CommonName:   commonName,
		Width:        width,
		Height:       estimatedHeight,
		IsStandard:   false,
	}
}

// GetSimpleResolutionName 获取简化的分辨率名称
func GetSimpleResolutionName(width int) string {
	switch {
	case width >= 7680:
		return "8K"
	case width >= 5120:
		return "5K"
	case width >= 3840:
		return "4k"
	case width >= 2560:
		return "2160p"
	case width >= 1920:
		return "1080p"
	case width >= 1280:
		return "720p"
	case width >= 720:
		return "480p"
	case width >= 640:
		return "360p"
	default:
		return "240p"
	}
}

func GetResolutionLevel(width, height int64) *VideoResolution {
	detector := NewResolutionDetector(64)
	resolution := detector.DetectResolution(int(width), int(height))
	return resolution
}

// CalculateBitrate 计算视频比特率 (bps)
func CalculateBitrate(stream *FFprobeStream, format *FFprobeFormat) (int64, error) {
	// 方法 1：直接使用流中的比特率
	if stream.BitRate != "" && stream.BitRate != "N/A" && stream.BitRate != "0" {
		bitrate, err := strconv.ParseInt(stream.BitRate, 10, 64)
		if err == nil && bitrate > 0 {
			return bitrate, nil
		}
	}

	// 方法 2：使用格式中的总比特率
	if format.BitRate != "" && format.BitRate != "N/A" && format.BitRate != "0" {
		bitrate, err := strconv.ParseInt(format.BitRate, 10, 64)
		if err == nil && bitrate > 0 {
			return bitrate, nil
		}
	}

	// 方法 3：通过文件大小和时长计算
	if format.Size != "" && format.Duration != "" {
		return calculateBitrateFromSizeAndDuration(format.Size, format.Duration)
	}

	// 方法 4：通过帧数和帧率估算
	if stream.NB_Frames != "" && stream.AvgFrameRate != "" {
		return estimateBitrateFromFrames(stream.NB_Frames, stream.AvgFrameRate, format.Size)
	}

	return 0, fmt.Errorf("无法计算比特率：信息不足")
}

// calculateBitrateFromSizeAndDuration 通过文件大小和时长计算比特率
func calculateBitrateFromSizeAndDuration(sizeStr, durationStr string) (int64, error) {
	size, err := strconv.ParseInt(sizeStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("解析文件大小失败：%v", err)
	}

	duration, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, fmt.Errorf("解析时长失败：%v", err)
	}

	if duration <= 0 {
		return 0, fmt.Errorf("时长必须大于 0")
	}

	// 比特率 =（文件大小 * 8）/ 时长（bps）
	bitrate := float64(size) * 8 / duration
	return int64(math.Round(bitrate)), nil
}

// estimateBitrateFromFrames 通过帧数和帧率估算比特率
func estimateBitrateFromFrames(nbFramesStr, frameRateStr, sizeStr string) (int64, error) {
	nbFrames, err := strconv.ParseInt(nbFramesStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("解析帧数失败：%v", err)
	}

	frameRate, err := parseFrameRate(frameRateStr)
	if err != nil {
		return 0, fmt.Errorf("解析帧率失败：%v", err)
	}

	size, err := strconv.ParseInt(sizeStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("解析文件大小失败：%v", err)
	}

	if frameRate <= 0 || nbFrames <= 0 {
		return 0, fmt.Errorf("帧率和帧数必须大于 0")
	}

	// 时长 = 帧数 / 帧率
	duration := float64(nbFrames) / frameRate
	// 比特率 = (文件大小 * 8) / 时长
	bitrate := float64(size) * 8 / duration

	return int64(math.Round(bitrate)), nil
}

// parseFrameRate 解析帧率字符串（如 "30000/1001"）
func parseFrameRate(frameRateStr string) (float64, error) {
	// 如果是分数形式
	if matches := regexp.MustCompile(`^(\d+)/(\d+)$`).FindStringSubmatch(frameRateStr); len(matches) == 3 {
		numerator, _ := strconv.ParseFloat(matches[1], 64)
		denominator, _ := strconv.ParseFloat(matches[2], 64)
		if denominator != 0 {
			return numerator / denominator, nil
		}
	}

	// 如果是小数形式
	if value, err := strconv.ParseFloat(frameRateStr, 64); err == nil {
		return value, nil
	}

	return 0, fmt.Errorf("无法解析帧率：%s", frameRateStr)
}

// ParseDurationToSeconds 将 ffprobe 的 duration 格式化为秒
func ParseDurationToSeconds(durationStr string) (int64, error) {
	if durationStr == "" || durationStr == "N/A" {
		return 0, fmt.Errorf("duration 为空或不可用")
	}

	// 尝试直接解析为秒（浮点数）
	if seconds, err := strconv.ParseFloat(durationStr, 64); err == nil {
		return int64(math.Round(seconds)), nil
	}

	// 尝试解析时间格式（HH:MM:SS.ms 或 HH:MM:SS）
	if seconds, err := parseTimeFormat(durationStr); err == nil {
		return seconds, nil
	}

	return 0, fmt.Errorf("无法解析 duration 格式：%s", durationStr)
}

// parseTimeFormat 解析时间格式（HH:MM:SS.ms）
func parseTimeFormat(timeStr string) (int64, error) {
	parts := strings.Split(timeStr, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, fmt.Errorf("时间格式不正确")
	}

	var hours, minutes, seconds int64
	var err error

	// 处理不同的时间格式
	switch len(parts) {
	case 2: // MM:SS
		minutes, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return 0, err
		}
		seconds, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, err
		}

	case 3: // HH:MM:SS 或 HH:MM:SS.ms
		hours, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return 0, err
		}
		minutes, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, err
		}

		// 处理秒部分（可能包含毫秒）
		seconds, err = parseSecondsWithMilliseconds(parts[2])
		if err != nil {
			return 0, err
		}
	}

	totalSeconds := int64(hours*3600 + minutes*60 + seconds)
	return totalSeconds, nil
}

// parseSecondsWithMilliseconds 解析秒和毫秒
func parseSecondsWithMilliseconds(secondsStr string) (int64, error) {
	// 检查是否包含毫秒
	if strings.Contains(secondsStr, ".") {
		seconds, err := strconv.ParseFloat(secondsStr, 64)
		if err != nil {
			return 0, err
		}
		return int64(math.Round(seconds)), nil
	}
	// 没有毫秒，直接解析为整数
	seconds, err := strconv.ParseFloat(secondsStr, 64)
	if err != nil {
		return 0, err
	}
	return int64(math.Round(seconds)), nil
}
