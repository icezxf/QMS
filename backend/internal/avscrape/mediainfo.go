package avscrape

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func writeMediaInfoJSON(tmpDir, baseName string, rawJSON []byte) *LocalFile {
	if len(rawJSON) == 0 {
		return nil
	}
	var ff struct {
		Format struct {
			Size     string `json:"size"`
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType     string `json:"codec_type"`
			CodecName     string `json:"codec_name"`
			Width         int    `json:"width"`
			Height        int    `json:"height"`
			PixFmt        string `json:"pix_fmt"`
			Channels      int    `json:"channels"`
			ChannelLayout string `json:"channel_layout"`
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
		
		var streams []map[string]interface{}
		for i, s := range ff.Streams {
			m := map[string]interface{}{"Index": i, "Codec": s.CodecName, "Language": s.Tags.Language}
			if s.CodecType == "video" {
				depth := 8
				if strings.Contains(s.PixFmt, "10le") || strings.Contains(s.PixFmt, "12le") {
					depth = 10
				}
				
				// 修复：取宽和高最大的值作为基准，解决 4096x2048 被误判为 1080p 的问题
				base := s.Width
				if s.Height > base {
					base = s.Height
				}
				
				p := "SD"
				if base >= 2160 {
					p = "2160p"
				} else if base >= 1080 {
					p = "1080p"
				} else if base >= 720 {
					p = "720p"
				}

				m["Type"] = "Video"
				m["Width"] = s.Width
				m["Height"] = s.Height
				m["BitDepth"] = depth
				m["DisplayTitle"] = fmt.Sprintf("%s %s %dbit", p, strings.ToUpper(s.CodecName), depth)
			} else if s.CodecType == "audio" {
				m["Type"] = "Audio"
				m["Channels"] = s.Channels
				m["ChannelLayout"] = s.ChannelLayout
				m["DisplayTitle"] = fmt.Sprintf("%s %s", strings.ToUpper(s.CodecName), s.ChannelLayout)
			} else {
				continue
			}
			streams = append(streams, m)
		}
		
		out := map[string]interface{}{"MediaSourceInfo": map[string]interface{}{
			"Size": size, "RunTimeTicks": int64(secs * 10000000), "MediaStreams": streams,
		}}
		b, _ := json.MarshalIndent([]interface{}{out}, "", "  ")
		jp := filepath.Join(tmpDir, baseName+"-mediainfo.json")
		if err := os.WriteFile(jp, b, 0644); err == nil {
			return &LocalFile{LocalPath: jp, RemoteName: baseName + "-mediainfo.json"}
		}
	}
	return nil
}