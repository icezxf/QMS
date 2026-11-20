package scrape

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"qmediasync/internal/helpers"
	"qmediasync/internal/models"
	"qmediasync/internal/v115open"
)

// 目录直读：不依赖 scrape_media 里的扫描记录，直接把目录里的视频列出来补 json。
//
// 为什么需要它：补充 mediainfo.json 与"是否刮削过"没有任何关系，但原实现把
// 「数据库里有没有这条记录」当成了唯一入口，于是没扫描过的目录永远是
// "没有可补充的记录"。这里改成两条来源合并：
//   1. 数据库记录（优先，知道整理后的目标目录，路径最准）
//   2. 目录直读（兜底，完全不看数据库，只要有视频就能补）
// 两条来源按键去重，同一个文件不会补两次。

const (
	supplementScanMaxDepth   = 3    // 目录扫描深度（剧集一般是 剧名/Season xx/文件，3 层足够）
	supplementScanMaxEntries = 5000 // 单次任务最多收集的文件数，防止误选根目录打爆配额
	supplementScanPageSize   = 200  // 每次列目录的条数
	supplementScanMaxPages   = 25   // 单目录最多翻页数
)

// supplementDirEntry 统一各来源的目录条目，屏蔽 115/OpenList/本地 的字段差异。
type supplementDirEntry struct {
	name  string // 文件或目录名
	isDir bool
	id    string // 115 的 file_id；本地为绝对路径；OpenList 无 ID 时为空
	pick  string // 115 的 pickcode，其他来源为空
	size  int64  // 文件大小，目录为 0
}

// supplementScanStats 扫描过程的统计，用于把"一条都没有"翻译成能定位问题的提示。
type supplementScanStats struct {
	Dirs      int  // 成功列出的目录数
	Files     int  // 见到的文件数
	Videos    int  // 其中被识别为视频的数量
	HasJSON   int  // 已有同名 json 且被跳过
	TooSmall  int  // 小于最小文件大小被跳过
	DirErrors int  // 列目录失败的次数
	Truncated bool // 是否因为达到条数上限而提前结束
}

// listSupplementDir 列出目录下的一级条目。
func (t *MediaInfoSupplement) listSupplementDir(dirPath, dirID string) ([]supplementDirEntry, error) {
	switch t.scrapePath.SourceType {
	case models.SourceTypeLocal:
		if strings.TrimSpace(dirPath) == "" {
			return nil, fmt.Errorf("本地目录路径为空")
		}
		items, err := os.ReadDir(dirPath)
		if err != nil {
			return nil, err
		}
		entries := make([]supplementDirEntry, 0, len(items))
		for _, item := range items {
			e := supplementDirEntry{
				name:  item.Name(),
				isDir: item.IsDir(),
				id:    filepath.Join(dirPath, item.Name()),
			}
			if !item.IsDir() {
				if info, err := item.Info(); err == nil {
					e.size = info.Size()
				}
			}
			entries = append(entries, e)
		}
		return entries, nil

	case models.SourceType115:
		if t.base.v115Client == nil {
			return nil, fmt.Errorf("115 客户端未初始化")
		}
		entries := make([]supplementDirEntry, 0, supplementScanPageSize)
		offset := 0
		for page := 0; page < supplementScanMaxPages; page++ {
			resp, err := t.base.v115Client.GetFsList(t.ctx, dirID, true, false, true, offset, supplementScanPageSize)
			if err != nil {
				return entries, err
			}
			if resp == nil || len(resp.Data) == 0 {
				break
			}
			for _, f := range resp.Data {
				entries = append(entries, supplementDirEntry{
					name:  f.FileName,
					isDir: f.FileCategory == v115open.TypeDir,
					id:    f.FileId,
					pick:  f.PickCode,
					size:  f.FileSize,
				})
			}
			if len(resp.Data) < supplementScanPageSize {
				break
			}
			offset += supplementScanPageSize
		}
		return entries, nil

	case models.SourceTypeOpenList:
		if t.base.openlistClient == nil {
			return nil, fmt.Errorf("OpenList 客户端未初始化")
		}
		entries := make([]supplementDirEntry, 0, supplementScanPageSize)
		for page := 1; page <= supplementScanMaxPages; page++ {
			resp, err := t.base.openlistClient.FileList(t.ctx, dirPath, page, supplementScanPageSize)
			if err != nil {
				return entries, err
			}
			if resp == nil || len(resp.Content) == 0 {
				break
			}
			for _, f := range resp.Content {
				entries = append(entries, supplementDirEntry{
					name:  f.Name,
					isDir: f.IsDir,
					size:  f.Size,
				})
			}
			if int64(page*supplementScanPageSize) >= resp.Total {
				break
			}
		}
		return entries, nil

	case models.SourceTypeBaiduPan:
		return nil, fmt.Errorf("百度网盘没有可用的直链接口，无法补充媒体信息")
	default:
		return nil, fmt.Errorf("该网盘类型（%s）暂不支持补充媒体信息", t.scrapePath.SourceType)
	}
}

// collectSupplementTargets 汇总本次要处理的条目：数据库记录 + 目录直读，去重。
func (t *MediaInfoSupplement) collectSupplementTargets() ([]*models.ScrapeMediaFile, supplementScanStats, int) {
	stats := supplementScanStats{}
	targets := make([]*models.ScrapeMediaFile, 0, 64)
	seen := make(map[string]bool)
	preSkipped := 0

	// 1) 数据库记录优先：这些记录里有整理后的目标目录，比目录直读更准。
	for _, f := range collectSupplementFiles(t.scrapePath, t.options) {
		if f == nil {
			continue
		}
		dest, destID := supplementDest(f)
		key := supplementTargetKey(f.SourceType, dest, destID, f.VideoFilename)
		if seen[key] {
			continue
		}
		seen[key] = true
		targets = append(targets, f)
	}

	// 2) 目录直读：不看数据库，直接把网盘/本地目录里的视频列出来。
	//
	// 先扫源目录（未整理的视频都在这里）。只有当源目录一个视频都没有时，才去扫
	// 整理后的目录兜底——整理库往往有几百个条目，从根往下逐个子目录列会白耗配额。
	t.scanDirectoryTree(t.scrapePath.SourcePath, t.scrapePath.SourcePathId, &targets, seen, &stats, &preSkipped)
	if stats.Videos == 0 && len(targets) == 0 {
		t.scanDirectoryTree(t.scrapePath.DestPath, t.scrapePath.DestPathId, &targets, seen, &stats, &preSkipped)
	}

	if len(targets) >= supplementScanMaxEntries {
		stats.Truncated = true
	}
	return targets, stats, preSkipped
}

// scanDirectoryTree 从某个根目录开始按层列目录，把视频收集成待补条目。
// 深度与条数都有上限，避免误选到网盘根目录时打爆请求配额。
func (t *MediaInfoSupplement) scanDirectoryTree(
	rootPath, rootID string,
	targets *[]*models.ScrapeMediaFile,
	seen map[string]bool,
	stats *supplementScanStats,
	preSkipped *int,
) {
	rootPath = strings.TrimSpace(rootPath)
	rootID = strings.TrimSpace(rootID)
	if rootPath == "" && rootID == "" {
		return
	}

	type scanNode struct {
		path  string
		id    string
		depth int
	}
	queue := []scanNode{{rootPath, rootID, 0}}

	for len(queue) > 0 && len(*targets) < supplementScanMaxEntries {
		node := queue[0]
		queue = queue[1:]

		entries, err := t.listSupplementDir(node.path, node.id)
		if err != nil {
			stats.DirErrors++
			helpers.AppLogger.Warnf("[媒体信息补充] 列出目录 %s 失败：%v", node.path, err)
			continue
		}
		stats.Dirs++

		// 先收集本目录已有的 mediainfo.json，省掉逐条再查一次
		existingJSON := make(map[string]bool, 8)
		for _, e := range entries {
			if !e.isDir && strings.HasSuffix(strings.ToLower(e.name), "-mediainfo.json") {
				existingJSON[e.name] = true
			}
		}

		for _, e := range entries {
			if e.isDir {
				if node.depth+1 <= supplementScanMaxDepth {
					queue = append(queue, scanNode{joinDir(t.scrapePath.SourceType, node.path, e.name), e.id, node.depth + 1})
				}
				continue
			}

			stats.Files++
			if !isSupplementVideoFile(e.name, t.scrapePath.VideoExtList) {
				continue
			}
			stats.Videos++
			if minSize := t.scrapePath.MinVideoFileSize; minSize > 0 && e.size > 0 && e.size < minSize {
				stats.TooSmall++
				continue
			}

			base := strings.TrimSuffix(e.name, path.Ext(e.name))
			if !t.options.Overwrite && existingJSON[base+"-mediainfo.json"] {
				stats.HasJSON++
				*preSkipped++
				continue
			}

			key := supplementTargetKey(t.scrapePath.SourceType, node.path, node.id, e.name)
			if seen[key] {
				continue
			}
			seen[key] = true
			*targets = append(*targets, t.newTargetFromEntry(node.path, node.id, e))
		}
	}
}

// newTargetFromEntry 把目录条目包装成一条内存记录，复用 processOne / probe 的整套逻辑。
// ID 为 0 表示"来自目录直读"，处理时不再回查数据库也不做整理目录定位。
func (t *MediaInfoSupplement) newTargetFromEntry(dirPath, dirID string, e supplementDirEntry) *models.ScrapeMediaFile {
	fullPath := joinDir(t.scrapePath.SourceType, dirPath, e.name)
	// 本地类型没有目录 ID，PathId 按约定存绝对路径（sourceDir 与上传都依赖它）
	if dirID == "" && t.scrapePath.SourceType == models.SourceTypeLocal {
		dirID = dirPath
	}
	target := &models.ScrapeMediaFile{
		MediaType:     t.scrapePath.MediaType,
		SourceType:    t.scrapePath.SourceType,
		ScrapeType:    t.scrapePath.ScrapeType,
		SourcePath:    t.scrapePath.SourcePath,
		SourcePathId:  t.scrapePath.SourcePathId,
		Path:          dirPath,
		PathId:        dirID,
		VideoFilename: e.name,
		Status:        models.ScrapeMediaStatusScanned,
	}
	switch t.scrapePath.SourceType {
	case models.SourceType115:
		target.VideoPickCode = e.pick
		target.VideoFileId = e.id
	case models.SourceTypeLocal:
		target.VideoPickCode = fullPath
		target.VideoFileId = fullPath
	default:
		// OpenList 没有 pickcode，用完整路径当标识
		target.VideoPickCode = fullPath
		target.VideoFileId = fullPath
	}
	return target
}

// joinDir 按来源拼接目录路径：本地用系统分隔符，网盘统一用 /。
func joinDir(sourceType models.SourceType, dir, name string) string {
	if sourceType == models.SourceTypeLocal {
		return filepath.Join(dir, name)
	}
	return path.Join(dir, name)
}

// supplementTargetKey 去重键：来源 + 目录 + 文件名。
func supplementTargetKey(sourceType models.SourceType, dirPath, dirID, fileName string) string {
	if dirID != "" {
		return fmt.Sprintf("%s|%s|%s", sourceType, dirID, fileName)
	}
	return fmt.Sprintf("%s|%s|%s", sourceType, filepath.ToSlash(dirPath), fileName)
}

// isSupplementVideoFile 判断是否为需要处理的视频文件。
func isSupplementVideoFile(name string, configured []string) bool {
	ext := strings.ToLower(path.Ext(name))
	if ext == "" {
		return false
	}
	// .strm 只是指针文件，探测不到真实流
	if ext == ".strm" {
		return false
	}
	exts := configured
	if len(exts) == 0 {
		exts = []string{".mp4", ".mkv", ".avi", ".mov", ".wmv", ".flv", ".webm", ".m4v", ".3gp", ".ts", ".m2ts", ".rmvb", ".iso"}
	}
	for _, e := range exts {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		if e == ext {
			return true
		}
	}
	return false
}

// describeScanEmpty 目录直读也没找到视频时，把原因说清楚。
func describeScanEmpty(sp *models.ScrapePath, stats supplementScanStats) string {
	roots := strings.TrimSpace(sp.SourcePath)
	if d := strings.TrimSpace(sp.DestPath); d != "" && d != roots {
		roots += "、" + d
	}
	if stats.Dirs == 0 {
		if stats.DirErrors > 0 {
			return fmt.Sprintf("无法列出目录 %s（%d 次请求失败）。请检查网盘账号是否可用、目录是否还存在；详细原因见后台日志。", roots, stats.DirErrors)
		}
		return fmt.Sprintf("目录 %s 为空或不存在，没有可处理的视频", roots)
	}
	if stats.Files == 0 {
		return fmt.Sprintf("目录 %s 下一层都没有文件（已列出的 %d 个目录都是空的）", roots, stats.Dirs)
	}
	detail := fmt.Sprintf("已列出 %d 个目录、%d 个文件，其中视频 %d 个", stats.Dirs, stats.Files, stats.Videos)
	switch {
	case stats.Videos == 0:
		return detail + "，没有识别到视频文件。请确认该目录里确实是视频，或检查刮削目录配置的视频扩展名"
	case stats.HasJSON > 0 && stats.HasJSON == stats.Videos:
		return detail + "，且都已存在 -mediainfo.json（选择“覆盖重做”可强制重补）"
	case stats.TooSmall > 0 && stats.TooSmall == stats.Videos:
		return detail + "，但都小于刮削目录配置的最小文件大小，已被跳过"
	case stats.Truncated:
		return detail + "，已达到单次处理上限，剩余文件请稍后再补一次"
	}
	return detail + "，但未能生成待处理列表，请把这条提示发出来以便定位"
}
