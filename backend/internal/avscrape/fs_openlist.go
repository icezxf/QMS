package avscrape

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"qmediasync/internal/helpers"
	"qmediasync/internal/models"
	"qmediasync/internal/openlist"
	"qmediasync/internal/v115open"
)

type FSOpenList struct {
	client *openlist.Client
	ctx    context.Context
}

func NewFSOpenList(p *models.AVPath) (*FSOpenList, error) {
	account, err := models.GetAccountById(p.AccountID)
	if err != nil {
		return nil, fmt.Errorf("获取账号失败: %w", err)
	}
	client := account.GetOpenListClient()
	if client == nil {
		return nil, fmt.Errorf("获取OpenList客户端失败")
	}
	return &FSOpenList{client: client, ctx: context.Background()}, nil
}

func (f *FSOpenList) List(path string) ([]string, error) {
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

func (f *FSOpenList) ListDetailed(path string) ([]FileEntry, error) {
	resp, err := f.client.FileList(f.ctx, path, 1, 1000)
	if err != nil {
		return nil, err
	}
	entries := make([]FileEntry, 0, len(resp.Content))
	for _, item := range resp.Content {
		t, _ := time.Parse(time.RFC3339, item.Modified)
		entries = append(entries, FileEntry{
			Name:  item.Name,
			Path:  path + "/" + item.Name,
			IsDir: item.IsDir,
			Size:  item.Size,
			MTime: t,
		})
	}
	return entries, nil
}

func (f *FSOpenList) Read(path string) ([]byte, error) {
	url := f.client.GetRawUrl(path)
	if url == "" {
		return nil, fmt.Errorf("获取直链失败: %s", path)
	}
	return helpers.ReadFromUrl(url, v115open.DEFAULTUA)
}

func (f *FSOpenList) Write(path string, data []byte) error {
	if f.Exists(path) {
		if err := f.Delete(path); err != nil {
			helpers.AppLogger.Warnf("[FS-OpenList] 删除已存在文件失败: %s => %v", path, err)
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
	_, err = f.client.Upload(tmpPath, path)
	return err
}

func (f *FSOpenList) MkdirAll(path string) error {
	return f.client.Mkdir(path)
}

func (f *FSOpenList) Move(src, dstDir, newName string) error {
	if newName != "" && newName != filepath.Base(src) {
		if err := f.client.Rename(filepath.Dir(src), filepath.Base(src), newName); err != nil {
			return err
		}
		src = filepath.Join(filepath.Dir(src), newName)
	}
	return f.client.Move(filepath.Dir(src), dstDir, []string{filepath.Base(src)})
}

func (f *FSOpenList) Copy(src, dstDir string) error {
	return f.client.Copy(filepath.Dir(src), dstDir, []string{filepath.Base(src)})
}

func (f *FSOpenList) Rename(path, newName string) error {
	return f.client.Rename(filepath.Dir(path), filepath.Base(path), newName)
}

func (f *FSOpenList) Exists(path string) bool {
	detail, err := f.client.FileDetail(path)
	return err == nil && detail != nil && detail.Name != ""
}

func (f *FSOpenList) Delete(path string) error {
	return f.client.Del(filepath.Dir(path), []string{filepath.Base(path)})
}

func (f *FSOpenList) DeleteDir(path string) error {
	return f.client.Del(filepath.Dir(path), []string{filepath.Base(path)})
}

func (f *FSOpenList) Download(remotePath, localPath string) error {
	url := f.client.GetRawUrl(remotePath)
	if url == "" {
		return fmt.Errorf("获取直链失败: %s", remotePath)
	}
	return helpers.DownloadFile(url, localPath, v115open.DEFAULTUA)
}

func (f *FSOpenList) Upload(localPath, remotePath string) error {
	_, err := f.client.Upload(localPath, remotePath)
	return err
}

// GetURL 获取直链 + 缓存应使用的 header
// 核心：直链签名按"生成直链时的 UA"算。
// OpenList 生成直链时用的 UA，就是 qmediasync 调 /api/fs/get 时传的 openlist.DEFAULTUA。
// 所以：
//   1. OpenList 返回了 header → 用它
//   2. 没返回 → 用 openlist.DEFAULTUA
func (f *FSOpenList) GetURL(path string) (string, error) {
	detail, err := f.client.FileDetail(path)
	if err != nil || detail == nil || detail.RawURL == "" {
		return "", fmt.Errorf("获取直链失败: %s, %v", path, err)
	}

	// OpenList 返回的 header 是 JSON 字符串，里面含正确的 UA
	if detail.Header != "" && detail.Header != "null" && detail.Header != "{}" {
		var hm map[string]string
		if err := json.Unmarshal([]byte(detail.Header), &hm); err == nil && len(hm) > 0 {
			h := http.Header{}
			for k, v := range hm {
				h.Set(k, v)
			}
			cacheURLHeader(detail.RawURL, h)
			helpers.AppLogger.Infof("[FS-OpenList] GetURL path=%s, header=%v", path, hm)
			return detail.RawURL, nil
		} else if err != nil {
			helpers.OpenListLog.Warnf("解析 OpenList header 失败: %v, 原文: %s", err, detail.Header)
		}
	}

	// OpenList 没返回 header，用 openlist.DEFAULTUA
	// 这是 qmediasync 调 OpenList 时的 UA，OpenList 会用它去请求 115 取直链，
	// 所以直链签名绑的就是这个 UA
	h := http.Header{}
	h.Set("User-Agent", openlist.DEFAULTUA)
	cacheURLHeader(detail.RawURL, h)
	helpers.AppLogger.Infof("[FS-OpenList] OpenList 未返回 header，使用 openlist.DEFAULTUA: %s", openlist.DEFAULTUA)

	return detail.RawURL, nil
}

func (f *FSOpenList) QueueUploads(files []LocalFile, dstDir string, accountId uint, sourceType string) (int, error) {
	if len(files) == 0 {
		return 0, nil
	}
	_ = f.client.Mkdir(dstDir)
	createdSubDirs := map[string]bool{}
	count := 0
	for _, file := range files {
		remotePath := dstDir + "/" + file.RemoteName
		parentPath := dstDir
		if idx := strings.LastIndex(file.RemoteName, "/"); idx > 0 {
			parentPath = dstDir + "/" + file.RemoteName[:idx]
			if !createdSubDirs[parentPath] {
				_ = f.client.Mkdir(parentPath)
				createdSubDirs[parentPath] = true
			}
		}
		fileName := filepath.Base(file.RemoteName)
		if err := models.AddUploadTaskFromAV(accountId, models.SourceType(sourceType), fileName, file.LocalPath, remotePath, parentPath); err != nil {
			helpers.AppLogger.Warnf("[AV上传队列] %s 加入队列失败: %v", file.RemoteName, err)
			continue
		}
		helpers.AppLogger.Infof("[AV上传队列] %s 已加入队列 (remote=%s)", file.RemoteName, remotePath)
		count++
	}
	return count, nil
}
