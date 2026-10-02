package avscrape

import (
	"os"
	"path/filepath"

	"Q115-STRM/internal/helpers"
	"Q115-STRM/internal/models"
)

type FSLocal struct{}

func NewFSLocal() (*FSLocal, error) {
	return &FSLocal{}, nil
}

func (f *FSLocal) List(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}

func (f *FSLocal) ListDetailed(path string) ([]FileEntry, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]FileEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, FileEntry{
			Name:  e.Name(),
			Path:  filepath.Join(path, e.Name()),
			IsDir: e.IsDir(),
			Size:  info.Size(),
			MTime: info.ModTime(),
		})
	}
	return out, nil
}

func (f *FSLocal) Read(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (f *FSLocal) Write(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (f *FSLocal) MkdirAll(path string) error {
	return os.MkdirAll(path, 0755)
}

func (f *FSLocal) Move(src, dstDir, newName string) error {
	dstName := filepath.Base(src)
	if newName != "" {
		dstName = newName
	}
	dst := filepath.Join(dstDir, dstName)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}
	return helpers.MoveFile(src, dst, false)
}

func (f *FSLocal) Copy(src, dstDir string) error {
	dst := filepath.Join(dstDir, filepath.Base(src))
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}
	return helpers.CopyFile(src, dst)
}

func (f *FSLocal) Rename(path, newName string) error {
	newPath := filepath.Join(filepath.Dir(path), newName)
	return os.Rename(path, newPath)
}

func (f *FSLocal) Exists(path string) bool {
	return helpers.PathExists(path)
}

func (f *FSLocal) Delete(path string) error {
	return os.Remove(path)
}

func (f *FSLocal) DeleteDir(path string) error {
	return os.RemoveAll(path)
}

func (f *FSLocal) Download(remotePath, localPath string) error {
	return helpers.CopyFile(remotePath, localPath)
}

func (f *FSLocal) Upload(localPath, remotePath string) error {
	return helpers.CopyFile(localPath, remotePath)
}

func (f *FSLocal) GetURL(path string) (string, error) {
	return path, nil
}

// QueueUploads 本地直接复制到目标
func (f *FSLocal) QueueUploads(files []LocalFile, dstDir string, accountId uint, sourceType string) (int, error) {
	if len(files) == 0 {
		return 0, nil
	}
	count := 0
	for _, file := range files {
		remotePath := filepath.Join(dstDir, filepath.FromSlash(file.RemoteName))
		fileName := filepath.Base(file.RemoteName)
		if err := models.AddUploadTaskFromAV(accountId, models.SourceType(sourceType), fileName, file.LocalPath, remotePath, filepath.Dir(remotePath)); err != nil {
			helpers.AppLogger.Warnf("[AV上传队列] %s 加入队列失败: %v", file.RemoteName, err)
			continue
		}
		count++
	}
	return count, nil
}
