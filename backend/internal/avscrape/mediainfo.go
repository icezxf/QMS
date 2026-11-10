package avscrape

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func writeMediaInfoJSON(tmpDir, baseName string, rawJSON []byte) *LocalFile {
	if len(rawJSON) == 0 {
		return nil
	}
	
	// 扩充 ffprobe 解析结构
	var ff struct {
		Format struct {
			Size       string `json:"size"`
			Duration   string `json:"duration"`
			BitRate    string `json:"bit_rate"`
			FormatName string `json:"format_name"`
		} `json:"format"`
		Streams []struct {
			CodecType     string `json:"codec_type"`
			CodecName     string `json:"codec_name"`
			Width         int    `json:"width"`
			Height        int    `json:"height"`
			PixFmt        string `json:"pix_fmt"`
			Profile       string `json:"profile"`
			Level         int    `json:"level"`
			BitRate       string `json:"bit_rate"`
			AvgFrameRate  string `json:"avg_frame_rate"`
			Channels      int    `json:"channels"`
			ChannelLayout string `json:"channel_layout"`
			SampleRate    string `json:"sample_rate"`
			Tags          struct {
				Language string `json:"language"`
			} `json:"tags"`
		} `json:"streams"`
	}
	
	if json.Unmarshal(rawJSON, &ff) == nil {
		var size int64
		fmt.Sscanf(ff.Format.Size, "%d", &size)
		var secs float64
		fmt.Sscanf(ff.Format.Duration, "%f", &secs)
		ticks := int64(secs * 10000000)
		
		var bitrate int64
		fmt.Sscanf(ff.Format.BitRate, "%d", &bitrate)
		
		var streams []map[string]interface{}
		for i, s := range ff.Streams {
			m := map[string]interface{}{"Index": i, "Codec": s.CodecName, "Language": s.Tags.Language}
			if s.CodecType == "video" {
				depth := 8
				if strings.Contains(s.PixFmt, "10le") || strings.Contains(s.PixFmt, "12le") { depth = 10 }
				
				base := s.Width
				if s.Height > base { base = s.Height }
				p := "SD"
				if base >= 2160 { p = "2160p" } else if base >= 1080 { p = "1080p" } else if base >= 720 { p = "720p" }

				m["Type"] = "Video"
				m["Width"] = s.Width
				m["Height"] = s.Height
				m["BitDepth"] = depth
				m["DisplayTitle"] = fmt.Sprintf("%s %s %dbit", p, strings.ToUpper(s.CodecName), depth)
				m["Profile"] = s.Profile
				m["Level"] = s.Level
				if br, err := strconv.ParseInt(s.BitRate, 10, 64); err == nil { m["BitRate"] = br }
			} else if s.CodecType == "audio" {
				m["Type"] = "Audio"
				m["Channels"] = s.Channels
				m["ChannelLayout"] = s.ChannelLayout
				m["DisplayTitle"] = fmt.Sprintf("%s %s", strings.ToUpper(s.CodecName), s.ChannelLayout)
				if br, err := strconv.ParseInt(s.BitRate, 10, 64); err == nil { m["BitRate"] = br }
				if sr, err := strconv.ParseInt(s.SampleRate, 10, 64); err == nil { m["SampleRate"] = sr }
			} else {
				continue
			}
			streams = append(streams, m)
		}
		
		out := map[string]interface{}{"MediaSourceInfo": map[string]interface{}{
			"Size":         size,
			"RunTimeTicks": ticks,
			"Bitrate":      bitrate,
			"Container":    ff.Format.FormatName,
			"MediaStreams": streams,
		}}
		b, _ := json.MarshalIndent([]interface{}{out}, "", "  ")
		jp := filepath.Join(tmpDir, baseName+"-mediainfo.json")
		if err := os.WriteFile(jp, b, 0644); err == nil {
			return &LocalFile{LocalPath: jp, RemoteName: baseName + "-mediainfo.json"}
		}
	}
	return nil
}