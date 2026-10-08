package aven

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"qmediasync/internal/helpers"
)

// ============================================================
// osHash 计算 + ffprobe 探测
// 一次下载头 2MB + 尾 64KB，同时完成：
//   1. 头尾各 64KB 算 osHash
//   2. 头 2MB 给 ffprobe 解析分辨率/HDR
// ============================================================

const (
	oshashChunkSize = 64 * 1024        // 64KB
	ffprobeHeadSize = 2 * 1024 * 1024  // 2MB
	oshashTailSize  = 64 * 1024        // 64KB
)

type ProbeResult struct {
	Oshash     string
	FileSize   int64
	Resolution string
	IsHDR      bool
}

// ProbeVideoByURL 下载头尾数据，计算 osHash + ffprobe 探测
// headers 是给 URL 请求用的（115 的 UA 等）
func ProbeVideoByURL(videoURL string, headers map[string]string) (*ProbeResult, error) {
	if videoURL == "" {
		return nil, fmt.Errorf("空 URL")
	}

	// ===== 1. 下载头部 2MB =====
	head, fileSize, err := downloadRange(videoURL, 0, ffprobeHeadSize-1, headers)
	if err != nil {
		return nil, fmt.Errorf("下载头部失败: %w", err)
	}

	// ===== 2. 下载尾部 64KB =====
	var tail []byte
	if fileSize > oshashTailSize {
		tail, err = downloadRange(videoURL, fileSize-oshashTailSize, fileSize-1, headers)
		if err != nil {
			helpers.AppLogger.Warnf("[欧美探测] 下载尾部失败: %v", err)
			// 尾部失败不致命，osHash 会返回空
		}
	}

	// ===== 3. 算 osHash =====
	oshash := computeOshash(head, tail, fileSize)

	// ===== 4. ffprobe 探测分辨率/HDR =====
	resolution, isHDR := probeFromBuffer(head)

	helpers.AppLogger.Infof("[欧美探测] fileSize=%d oshash=%s resolution=%s hdr=%v",
		fileSize, oshash, resolution, isHDR)

	return &ProbeResult{
		Oshash:     oshash,
		FileSize:   fileSize,
		Resolution: resolution,
		IsHDR:      isHDR,
	}, nil
}

// ============================================================
// 下载指定字节范围
// ============================================================

func downloadRange(videoURL string, start, end int64, headers map[string]string) ([]byte, int64, error) {
	req, err := http.NewRequest("GET", videoURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return nil, 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// 解析文件总大小
	var fileSize int64
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		if slash := strings.LastIndex(cr, "/"); slash > 0 {
			fmt.Sscanf(cr[slash+1:], "%d", &fileSize)
		}
	}
	if fileSize == 0 {
		fmt.Sscanf(resp.Header.Get("Content-Length"), "%d", &fileSize)
	}

	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, resp.Body); err != nil {
		return nil, 0, err
	}
	return buf.Bytes(), fileSize, nil
}

// ============================================================
// osHash 计算（头尾各 64KB，逐 8 字节小端累加，起始值 = fileSize）
// ============================================================

func computeOshash(head, tail []byte, fileSize int64) string {
	if fileSize < 2*oshashChunkSize {
		return ""
	}
	var hash uint64 = uint64(fileSize)
	for i := 0; i+8 <= len(head) && i < oshashChunkSize; i += 8 {
		hash += binary.LittleEndian.Uint64(head[i : i+8])
	}
	for i := 0; i+8 <= len(tail) && i < oshashChunkSize; i += 8 {
		hash += binary.LittleEndian.Uint64(tail[i : i+8])
	}
	return fmt.Sprintf("%016x", hash)
}

// ============================================================
// ffprobe 探测（临时文件 + 本地 ffprobe）
// ============================================================

func probeFromBuffer(data []byte) (string, bool) {
	if len(data) == 0 {
		return "", false
	}

	tmpFile, err := os.CreateTemp("", "aven-probe-*")
	if err != nil {
		helpers.AppLogger.Warnf("[欧美探测] 创建临时文件失败: %v", err)
		return "", false
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		helpers.AppLogger.Warnf("[欧美探测] 写入临时文件失败: %v", err)
		return "", false
	}
	tmpFile.Close()

	args := []string{
		"-v", "error",
		"-print_format", "json",
		"-show_streams",
		"-analyzeduration", "5000000",
		"-probesize", "2000000",
		tmpPath,
	}
	cmd := exec.Command("ffprobe", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()

	select {
	case err := <-done:
		if err != nil {
			helpers.AppLogger.Warnf("[欧美探测] ffprobe 失败: %v, stderr=%s", err, stderr.String())
			return "", false
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		helpers.AppLogger.Warnf("[欧美探测] ffprobe 超时")
		return "", false
	}

	var result struct {
		Streams []struct {
			Width    int    `json:"width"`
			Height   int    `json:"height"`
			PixFmt   string `json:"pix_fmt"`
			ColorTrc string `json:"color_transfer"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return "", false
	}
	if len(result.Streams) == 0 {
		return "", false
	}

	s := result.Streams[0]
	res := classifyResolution(s.Width, s.Height)
	hdr := isHDRPixelFormat(s.PixFmt, s.ColorTrc)
	return res, hdr
}

// ============================================================
// 分辨率分级（和日本 AV 一致）
// ============================================================

func classifyResolution(w, h int) string {
	if w == 0 && h == 0 {
		return ""
	}
	base := w
	if h > w {
		base = h
	}
	switch {
	case base >= 7680:
		return "8K"
	case base >= 7168:
		return "7K"
	case base >= 5760:
		return "6K"
	case base >= 4800:
		return "5K"
	case base >= 3840:
		return "4K"
	case base >= 2560:
		return "2K"
	case base >= 1920:
		return "1080p"
	case base >= 1280:
		return "720p"
	case base >= 854:
		return "480p"
	default:
		return fmt.Sprintf("%dp", base)
	}
}

func isHDRPixelFormat(pixFmt, colorTrc string) bool {
	if strings.Contains(pixFmt, "10le") || strings.Contains(pixFmt, "12le") {
		return true
	}
	trc := strings.ToLower(colorTrc)
	return trc == "smpte2084" || trc == "arib-std-b67"
}