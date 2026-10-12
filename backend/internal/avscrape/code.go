package avscrape

import (
	"regexp"
	"strings"
)

var codePatterns = []*regexp.Regexp{
	// 3DSVR-1766 / SSIS-001 / MIDV-192 / DSVR-1766 / 1PONDO-123456 等
	// 前缀：字母数字混合，至少 2 位、最多 10 位，且必须含字母
	regexp.MustCompile(`(?i)\b([A-Z][A-Z0-9]{1,9})[-_\s]?(\d{2,6})\b`),
	// 无分隔符：SSIS001 / 3DSVR1766
	regexp.MustCompile(`(?i)\b([A-Z][A-Z0-9]{1,9})(\d{3,6})\b`),
}

func ExtractCode(filename string) string {
	name := filename
	if idx := strings.LastIndex(name, "."); idx > 0 {
		name = name[:idx]
	}
	name = strings.ToUpper(name)

	// FC2 特殊处理
	if strings.Contains(name, "FC2") {
		re := regexp.MustCompile(`FC2[-_]?PPV[-_]?(\d{5,7})`)
		if m := re.FindStringSubmatch(name); len(m) > 1 {
			return "FC2-PPV-" + m[1]
		}
	}

	for _, re := range codePatterns {
		m := re.FindStringSubmatch(name)
		if len(m) >= 3 {
			prefix := strings.ToUpper(m[1])
			num := m[2]
			if prefix == "MP4" || prefix == "AVI" || prefix == "MKV" ||
				prefix == "FHD" || prefix == "UHD" || prefix == "WEB" {
				continue
			}
			// 前缀必须含至少一个字母，避免把纯数字当前缀
			hasLetter := false
			for _, r := range prefix {
				if r >= 'A' && r <= 'Z' {
					hasLetter = true
					break
				}
			}
			if !hasLetter {
				continue
			}
			return prefix + "-" + num
		}
	}
	return ""
}

func ExtractCodeFromPath(path string) string {
	parts := strings.Split(path, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if code := ExtractCode(parts[i]); code != "" {
			return code
		}
	}
	return ""
}
