package avscrape

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"qmediasync/internal/helpers"
	"qmediasync/internal/models"
	"qmediasync/internal/v115open"
)

type FS115 struct {
	client *v115open.OpenClient
	ctx    context.Context
}

func NewFS115(p *models.AVPath) (*FS115, error) {
	account, err := models.GetAccountById(p.AccountID)
	if err != nil {
		return nil, fmt.Errorf("获取账号失败: %w", err)
	}
	client := account.Get115Client()
	if client == nil {
		return nil, fmt.Errorf("获取115客户端失败")
	}
	return &FS115{client: client, ctx: context.Background()}, nil
}

func (f *FS115) List(path string) ([]string, error) {
	entries, err := f.ListDetailed(path)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
	}
	return names, nil
}

func (f *FS115) ListDetailed(path string) ([]FileEntry, error) {
	detail, err := f.client.GetFsDetailByPath(f.ctx, path)
	if err != nil || detail == nil || detail.FileId == "" {
		return nil, fmt.Errorf("获取目录详情失败: %s, %v", path, err)
	}
	resp, err := f.client.GetFsList(f.ctx, detail.FileId, true, false, true, 0, 1150)
	if err != nil {
		return nil, err
	}
	entries := make([]FileEntry, 0, len(resp.Data))
	for _, item := range resp.Data {
		entries = append(entries, FileEntry{
			Name:     item.FileName,
			Path:     path + "/" + item.FileName,
			IsDir:    item.FileCategory == v115open.TypeDir,
			Size:     item.FileSize,
			ID:       item.FileId,
			PickCode: item.PickCode,
			MTime:    time.Unix(item.Ptime, 0),
		})
	}
	return entries, nil
}

func (f *FS115) Read(path string) ([]byte, error) {
	detail, err := f.client.GetFsDetailByPath(f.ctx, path)
	if err != nil || detail == nil || detail.FileId == "" {
		return nil, fmt.Errorf("获取文件详情失败: %s, %v", path, err)
	}
	url := f.client.GetDownloadUrl(f.ctx, detail.PickCode, v115open.DEFAULTUA, false)
	if url == "" {
		return nil, fmt.Errorf("获取下载链接失败: %s", path)
	}
	return helpers.ReadFromUrl(url, v115open.DEFAULTUA)
}

func (f *FS115) Write(path string, data []byte) error {
	if f.Exists(path) {
		if err := f.Delete(path); err != nil {
			helpers.AppLogger.Warnf("[FS-115] 删除已存在文件失败: %s => %v", path, err)
		}
	}

	tmpFile, err := os.CreateTemp("", "avscrape-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return err
	}
	tmpFile.Close()

	parentPath := filepath.ToSlash(filepath.Dir(path))
	parentDetail, err := f.client.GetFsDetailByPath(f.ctx, parentPath)
	if err != nil || parentDetail == nil || parentDetail.FileId == "" {
		return fmt.Errorf("获取父目录失败: %s", parentPath)
	}
	_, err = f.client.Upload(f.ctx, tmpPath, parentDetail.FileId, "", "")
	return err
}

func (f *FS115) MkdirAll(path string) error {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	currentPath := ""
	currentId := "0"

	for _, part := range parts {
		if part == "" {
			continue
		}
		if currentPath == "" {
			currentPath = "/" + part
		} else {
			currentPath = currentPath + "/" + part
		}
		detail, err := f.client.GetFsDetailByPath(f.ctx, currentPath)
		if err == nil && detail != nil && detail.FileId != "" {
			currentId = detail.FileId
			continue
		}
		newId, err := f.client.MkDir(f.ctx, currentId, part)
		if err != nil {
			return fmt.Errorf("创建目录失败 %s: %w", currentPath, err)
		}
		currentId = newId
	}
	return nil
}

func (f *FS115) Move(src, dstDir, newName string) error {
	srcDetail, err := f.client.GetFsDetailByPath(f.ctx, src)
	if err != nil || srcDetail == nil || srcDetail.FileId == "" {
		return fmt.Errorf("获取源文件失败: %s", src)
	}
	dstDetail, err := f.client.GetFsDetailByPath(f.ctx, dstDir)
	if err != nil || dstDetail == nil || dstDetail.FileId == "" {
		return fmt.Errorf("获取目标目录失败: %s", dstDir)
	}
	if _, err := f.client.Move(f.ctx, []string{srcDetail.FileId}, dstDetail.FileId); err != nil {
		return err
	}
	if newName != "" && newName != srcDetail.FileName {
		_, err = f.client.ReName(f.ctx, srcDetail.FileId, newName)
	}
	return err
}

func (f *FS115) Copy(src, dstDir string) error {
	srcDetail, err := f.client.GetFsDetailByPath(f.ctx, src)
	if err != nil || srcDetail == nil || srcDetail.FileId == "" {
		return fmt.Errorf("获取源文件失败: %s", src)
	}
	dstDetail, err := f.client.GetFsDetailByPath(f.ctx, dstDir)
	if err != nil || dstDetail == nil || dstDetail.FileId == "" {
		return fmt.Errorf("获取目标目录失败: %s", dstDir)
	}
	_, err = f.client.Copy(f.ctx, []string{srcDetail.FileId}, dstDetail.FileId, false)
	return err
}

func (f *FS115) Rename(path, newName string) error {
	detail, err := f.client.GetFsDetailByPath(f.ctx, path)
	if err != nil || detail == nil || detail.FileId == "" {
		return fmt.Errorf("获取文件失败: %s", path)
	}
	_, err = f.client.ReName(f.ctx, detail.FileId, newName)
	return err
}

func (f *FS115) Exists(path string) bool {
	detail, err := f.client.GetFsDetailByPath(f.ctx, path)
	return err == nil && detail != nil && detail.FileId != ""
}

func (f *FS115) Delete(path string) error {
	detail, err := f.client.GetFsDetailByPath(f.ctx, path)
	if err != nil || detail == nil || detail.FileId == "" {
		return nil
	}
	parentPath := filepath.ToSlash(filepath.Dir(path))
	parentDetail, err := f.client.GetFsDetailByPath(f.ctx, parentPath)
	if err != nil || parentDetail == nil || parentDetail.FileId == "" {
		return fmt.Errorf("获取父目录失败: %s", parentPath)
	}
	_, err = f.client.Del(f.ctx, []string{detail.FileId}, parentDetail.FileId)
	return err
}

func (f *FS115) DeleteDir(path string) error {
	return f.Delete(path)
}

func (f *FS115) Download(remotePath, localPath string) error {
	detail, err := f.client.GetFsDetailByPath(f.ctx, remotePath)
	if err != nil || detail == nil || detail.FileId == "" {
		return fmt.Errorf("获取文件详情失败: %s", remotePath)
	}
	url := f.client.GetDownloadUrl(f.ctx, detail.PickCode, v115open.DEFAULTUA, false)
	if url == "" {
		return fmt.Errorf("获取下载链接失败: %s", remotePath)
	}
	return helpers.DownloadFile(url, localPath, v115open.DEFAULTUA)
}

func (f *FS115) Upload(localPath, remotePath string) error {
	parentPath := filepath.ToSlash(filepath.Dir(remotePath))
	parentDetail, err := f.client.GetFsDetailByPath(f.ctx, parentPath)
	if err != nil || parentDetail == nil || parentDetail.FileId == "" {
		return fmt.Errorf("获取父目录失败: %s", parentPath)
	}
	_, err = f.client.Upload(f.ctx, localPath, parentDetail.FileId, "", "")
	return err
}

// GetURL 获取直链（用于 ffprobe）
func (f *FS115) GetURL(path string) (string, error) {
	detail, err := f.client.GetFsDetailByPath(f.ctx, path)
	if err != nil || detail == nil || detail.FileId == "" {
		return "", fmt.Errorf("获取文件详情失败: %s", path)
	}
	url := f.client.GetDownloadUrl(f.ctx, detail.PickCode, v115open.DEFAULTUA, false)
	if url == "" {
		return "", fmt.Errorf("获取直链失败: %s", path)
	}
	return url, nil
}

// QueueUploads 把本地文件加入上传队列（异步走 GlobalUploadQueue）
func (f *FS115) QueueUploads(files []LocalFile, dstDir string, accountId uint, sourceType string) (int, error) {
	if len(files) == 0 {
		return 0, nil
	}

	if err := f.MkdirAll(dstDir); err != nil {
		return 0, fmt.Errorf("创建目标目录失败: %w", err)
	}

	dstDetail, err := f.client.GetFsDetailByPath(f.ctx, dstDir)
	if err != nil || dstDetail == nil || dstDetail.FileId == "" {
		return 0, fmt.Errorf("获取目标目录失败: %s", dstDir)
	}
	dstDirId := dstDetail.FileId

	subDirCache := map[string]string{}

	count := 0
	for _, file := range files {
		parentId := dstDirId
		remoteName := file.RemoteName
		if idx := strings.LastIndex(remoteName, "/"); idx > 0 {
			subDir := remoteName[:idx]
			fileName := remoteName[idx+1:]
			subId, ok := subDirCache[subDir]
			if !ok {
				subId, err = f.ensureSubDirCached(dstDirId, dstDir, subDir, subDirCache)
				if err != nil {
					helpers.AppLogger.Warnf("[AV上传队列] 创建子目录失败 %s: %v", subDir, err)
					continue
				}
			}
			parentId = subId
			remoteName = fileName
		}

		remoteFullPath := dstDir + "/" + file.RemoteName
		if err := models.AddUploadTaskFromAV(accountId, models.SourceType(sourceType), remoteName, file.LocalPath, remoteFullPath, parentId); err != nil {
			helpers.AppLogger.Warnf("[AV上传队列] %s 加入队列失败: %v", file.RemoteName, err)
			continue
		}
		helpers.AppLogger.Infof("[AV上传队列] %s 已加入队列 (remote=%s, parentId=%s)", file.RemoteName, remoteFullPath, parentId)
		count++
	}
	return count, nil
}

func (f *FS115) ensureSubDirCached(rootId, rootPath, subDir string, cache map[string]string) (string, error) {
	parts := strings.Split(strings.Trim(subDir, "/"), "/")
	currentId := rootId
	currentPath := rootPath
	for i := 0; i < len(parts); i++ {
		subPath := parts[i]
		fullPath := currentPath + "/" + subPath
		cacheKey := strings.Join(parts[:i+1], "/")
		if id, ok := cache[cacheKey]; ok {
			currentId = id
			currentPath = fullPath
			continue
		}
		detail, err := f.client.GetFsDetailByPath(f.ctx, fullPath)
		if err == nil && detail != nil && detail.FileId != "" {
			cache[cacheKey] = detail.FileId
			currentId = detail.FileId
			currentPath = fullPath
			continue
		}
		newId, err := f.client.MkDir(f.ctx, currentId, subPath)
		if err != nil {
			return "", fmt.Errorf("创建子目录失败 %s: %w", fullPath, err)
		}
		cache[cacheKey] = newId
		currentId = newId
		currentPath = fullPath
	}
	return currentId, nil
}
