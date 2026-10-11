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

	// cleanupMaxDeletesPerScan 单次扫描最多删多少棵残留子树
	cleanupMaxDeletesPerScan = 100
)

// cleanupSourceDir 递归清理源目录下的残留子目录
// 规则：
//   - 源目录本身保留
//   - 源目录下的任意子目录，如果连同所有子孙都没有 >1G 的视频，
//     就整棵删除（只调用一次 DeleteDir），不再单独删其子目录
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
		s.cleanupShallowestResidual(fs, e.Path, &deleted)
	}

	if deleted > 0 {
		helpers.AppLogger.Infof("[AV清理] 本次共删除 %d 个残留目录", deleted)
	}
}

// cleanupShallowestResidual 前序遍历：
//   - 如果 dir 整棵树都没有 >1G 的视频，直接删 dir 一次，返回 true
//   - 否则递归进子目录，继续找"最浅的纯残留目录"
//
// 返回值仅表示"dir 这一层是否被删掉"，调用方一般不需要依赖它。
func (s *Scanner) cleanupShallowestResidual(fs FileSystem, dir string, deleted *int) bool {
	if s.isPureResidual(fs, dir) {
		if *deleted >= cleanupMaxDeletesPerScan {
			helpers.AppLogger.Warnf("[AV清理] 已达到单次删除上限 %d，跳过 %s",
				cleanupMaxDeletesPerScan, dir)
			return true
		}

		if *deleted > 0 && cleanupDeleteInterval > 0 {
			time.Sleep(cleanupDeleteInterval)
		}

		if err := fs.DeleteDir(dir); err != nil {
			helpers.AppLogger.Warnf("[AV清理] 删除目录失败 %s: %v", dir, err)
			return false
		}
		*deleted++
		helpers.AppLogger.Infof("[AV清理] 已删除残留目录: %s", dir)
		return true
	}

	// 不是纯残留，递归子目录找最浅的纯残留目录
	entries, err := fs.ListDetailed(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir {
			continue
		}
		s.cleanupShallowestResidual(fs, e.Path, deleted)
	}
	return false
}

// isPureResidual 判断 dir 及其所有子孙是否都没有 >1G 的视频
// 列目录失败时保守返回 false（不删），避免误删
func (s *Scanner) isPureResidual(fs FileSystem, dir string) bool {
	entries, err := fs.ListDetailed(dir)
	if err != nil {
		return false
	}

	for _, e := range entries {
		if e.IsDir {
			if !s.isPureResidual(fs, e.Path) {
				return false
			}
			continue
		}
		if isVideoName(e.Name) && e.Size > cleanupBigVideoSize {
			return false
		}
	}
	return true
}

// isVideoName 判断文件名是否视频
func isVideoName(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return videoExts[ext]
}
