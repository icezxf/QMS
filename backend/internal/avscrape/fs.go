package avscrape

import (
	"fmt"
	"time"

	"qmediasync/internal/models"
)

// FileEntry 文件条目
type FileEntry struct {
	Name     string
	Path     string
	IsDir    bool
	Size     int64
	ID       string
	PickCode string
	MTime    time.Time
}

// LocalFile 待上传的本地文件
// RemoteName 可以是 "poster.jpg" 或 "extrafanart/scene-01.jpg"（含子目录）
type LocalFile struct {
	LocalPath  string
	RemoteName string
}

// FileSystem 文件系统抽象接口
type FileSystem interface {
	List(path string) ([]string, error)
	ListDetailed(path string) ([]FileEntry, error)
	Read(path string) ([]byte, error)
	Write(path string, data []byte) error
	MkdirAll(path string) error
	Move(src, dstDir, newName string) error
	Copy(src, dstDir string) error
	Rename(path, newName string) error
	Exists(path string) bool
	Delete(path string) error
	DeleteDir(path string) error
	Download(remotePath, localPath string) error
	Upload(localPath, remotePath string) error
	GetURL(path string) (string, error)

	// QueueUploads 把本地文件加入上传队列（异步走 GlobalUploadQueue）
	// 返回加入的任务数量
	QueueUploads(files []LocalFile, dstDir string, accountId uint, sourceType string) (int, error)
}

// NewFileSystem 根据 AVPath 的 SourceType 创建对应实现
func NewFileSystem(p *models.AVPath) (FileSystem, error) {
	switch p.SourceType {
	case "115":
		return NewFS115(p)
	case "openlist":
		return NewFSOpenList(p)
	case "local":
		return NewFSLocal()
	default:
		return nil, fmt.Errorf("不支持的源类型: %s", p.SourceType)
	}
}
