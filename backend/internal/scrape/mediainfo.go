package scrape

import (
	"encoding/json"
	"os"
	"path/filepath"

	"qmediasync/internal/helpers"
	"qmediasync/internal/models"
)

// WriteMediaInfoJSON 根据本次刮削保存的 ffprobe 原始数据，生成媒体信息 JSON 文件。
// 文件名格式为 <视频名>-mediainfo.json，写入 tmpDir 目录。未探测到数据时静默跳过。
// 解析与补全逻辑统一收敛到 helpers.BuildMediaInfoJSON，与 avscrape/aven 输出格式一致。
// 返回生成的文件名，未生成时返回空字符串。
func (s *ScrapeBase) WriteMediaInfoJSON(mediaFile *models.ScrapeMediaFile, tmpDir, baseName string) string {
	ff := mediaFile.FFprobeJson
	if ff == nil || len(ff.Streams) == 0 {
		return ""
	}

	rawJSON, err := json.Marshal(ff)
	if err != nil {
		helpers.AppLogger.Errorf("序列化 ffprobe 数据失败：%v", err)
		return ""
	}
	content, err := helpers.BuildMediaInfoJSON(rawJSON)
	if err != nil {
		helpers.AppLogger.Errorf("生成 mediainfo.json 失败：%v", err)
		return ""
	}

	fileName := baseName + "-mediainfo.json"
	path := filepath.Join(tmpDir, fileName)
	if err := os.WriteFile(path, content, 0644); err != nil {
		helpers.AppLogger.Errorf("写入 mediainfo.json 失败：%v", err)
		return ""
	}
	helpers.AppLogger.Infof("生成媒体信息文件成功：%s", path)
	return fileName
}
