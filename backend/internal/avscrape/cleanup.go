package avscrape

import (
	"path/filepath"
	"strings"
	"time"

	"qmediasync/internal/helpers"
)

const (
	// cleanupBigVideoSize 大于 1G 的视频才算"有效视频"
	cleanupBigVideoSize = int64(1) << 30

	// cleanupDeleteInterval 每次删除之间的间隔，避免触发网盘风控
	cleanupDeleteInterval = 500 * time.Millisecond

	// cleanupMaxDeletesPerScan 单次扫描最多删多少个目录（防止极端情况）
	cleanupMaxDeletesPerScan = 100
)

// cleanupSourceDir 递归清理源目录下的残留子目录
// 规则：
//   - 源目录本身保留
//   - 源目录下的任意子目录，如果连同所有子目录、所有文件都没有 >1G 的视频，
//     就整个删除
func (s *Scanner) cleanupSourceDir(fs FileSystem, srcDir string) {
	entries, err := fs.ListDetailed(srcDir)
	if err != nil {
		helpers.AppLogger.Warnf("[AV清理] 列出源目录失败 %s: %v", srcDir, err)
		return
	}

	deleted := 0
	for _, e := range entries {
		if !e.IsDir {
			continue
		}
		s.cleanupDirRecursive(fs, e.Path, &deleted)
	}

	if deleted > 0 {
		helpers.AppLogger.Infof("[AV清理] 本次共删除 %d 个残留目录", deleted)
	}
}

// cleanupDirRecursive 后序遍历目录树
// 返回 true 表示这个目录已经被删掉
func (s *Scanner) cleanupDirRecursive(fs FileSystem, dir string, deleted *int) bool {
	entries, err := fs.ListDetailed(dir)
	if err != nil {
		// 列不出来就不动它，保守
		return false
	}

	hasBigVideo := false

	// 1. 先递归处理子目录
	for _, e := range entries {
		if !e.IsDir {
			continue
		}
		subRemoved := s.cleanupDirRecursive(fs, e.Path, deleted)
		if !subRemoved {
			// 子目录还在，说明里面（含深层）有 >1G 视频
			hasBigVideo = true
		}
	}

	// 2. 再检查当前目录的直接文件
	if !hasBigVideo {
		for _, e := range entries {
			if e.IsDir {
				continue
			}
			if !isVideoName(e.Name) {
				continue
			}
			if e.Size > cleanupBigVideoSize {
				hasBigVideo = true
				break
			}
		}
	}

	// 还有大视频，保留
	if hasBigVideo {
		return false
	}

	// 达到单次上限就停
	if *deleted >= cleanupMaxDeletesPerScan {
		helpers.AppLogger.Warnf("[AV清理] 已达到单次删除上限 %d，跳过 %s",
			cleanupMaxDeletesPerScan, dir)
		return false
	}

	// 节流：每次删除之前 sleep，避免连续 Del 请求
	if *deleted > 0 && cleanupDeleteInterval > 0 {
		time.Sleep(cleanupDeleteInterval)
	}

	// 删除整个目录
	if err := fs.DeleteDir(dir); err != nil {
		helpers.AppLogger.Warnf("[AV清理] 删除目录失败 %s: %v", dir, err)
		return false
	}
	*deleted++
	helpers.AppLogger.Infof("[AV清理] 已删除残留目录: %s", dir)
	return true
}

// isVideoName 判断文件名是否视频
func isVideoName(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return videoExts[ext]
}
