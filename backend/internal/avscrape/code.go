package avscrape

import (
	"regexp"
	"strings"
)

var codePatterns = []*regexp.Regexp{
	// 带分隔符：3DSVR-1766 / SSIS-001 / PRVR-078
	regexp.MustCompile(`(?i)(\d?[A-Z]+)[-_\s](\d+)`),
	// 无分隔符：KIWVR00877 / SSIS001 / 3DSVR1766
	regexp.MustCompile(`(?i)(\d?[A-Z]+)(\d+)`),
}

// normalizeNumPart 番号数字部分标准化：去掉前导零，保留至少 3 位
// 00877 → 877；00001 → 001；01766 → 1766；078 → 078
func normalizeNumPart(num string) string {
	trimmed := strings.TrimLeft(num, "0")
	if trimmed == "" {
		trimmed = "0"
	}
	for len(trimmed) < 3 {
		trimmed = "0" + trimmed
	}
	return trimmed
}

// matchCodePart 对一段字符串跑番号正则
func matchCodePart(name string) string {
	for _, re := range codePatterns {
		m := re.FindStringSubmatch(name)
		if len(m) < 3 {
			continue
		}
		prefix := strings.ToUpper(m[1])
		num := m[2]

		// 排除明显不是番号的前缀
		switch prefix {
		case "MP4", "AVI", "MKV", "FHD", "UHD", "WEB",
			"CD", "PART", "DISC", "DISK",
			"4K", "8K", "2K", "HD", "SD":
			continue
		}

		// 前缀必须含至少一个字母，排除纯数字
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

		return prefix + "-" + normalizeNumPart(num)
	}
	return ""
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

	// 优先从 @ 后面开始（跳过 4k2.me@ / hkbisi.com@ / 489155.com@ 这类域名前缀）
	if at := strings.LastIndex(name, "@"); at >= 0 && at < len(name)-1 {
		if code := matchCodePart(name[at+1:]); code != "" {
			return code
		}
	}

	// 整体匹配（没有 @ 或 @ 后面没匹配到）
	return matchCodePart(name)
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