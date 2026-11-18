package avscrape

import (
	"os"
	"path/filepath"

	"qmediasync/internal/helpers"
)

// writeMediaInfoJSON 根据 ffprobe 原始数据生成 <baseName>-mediainfo.json。
// 解析与补全逻辑统一收敛到 helpers.BuildMediaInfoJSON，与其他刮削模块保持一致。
func writeMediaInfoJSON(tmpDir, baseName string, rawJSON []byte) *LocalFile {
	b, err := helpers.BuildMediaInfoJSON(rawJSON)
	if err != nil {
		return nil
	}
	jp := filepath.Join(tmpDir, baseName+"-mediainfo.json")
	if err := os.WriteFile(jp, b, 0644); err != nil {
		return nil
	}
	return &LocalFile{LocalPath: jp, RemoteName: baseName + "-mediainfo.json"}
}
