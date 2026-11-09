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
		} `json:"streams"`
	}
	if json.Unmarshal(rawJSON, &ff) == nil {
		var size int64
		fmt.Sscanf(ff.Format.Size, "%d", &size)
		var secs float64
		fmt.Sscanf(ff.Format.Duration, "%f", &secs)
		
		var streams []map[string]interface{}
		for i, s := range ff.Streams {
			m := map[string]interface{}{"Index": i, "Codec": s.CodecName}
			if s.CodecType == "video" {
				p := "SD"
				if s.Height >= 2160 { p = "2160p" } else if s.Height >= 1080 { p = "1080p" } else if s.Height >= 720 { p = "720p" }
				m["Type"] = "Video"
				m["Width"] = s.Width
				m["Height"] = s.Height
				m["BitDepth"] = 8
				m["DisplayTitle"] = fmt.Sprintf("%s %s 8bit", p, strings.ToUpper(s.CodecName))
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
