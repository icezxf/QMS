package avscrape

import (
"regexp"
"strings"
)

var codePatterns = []*regexp.Regexp{
regexp.MustCompile(`(?i)([A-Z]{2,6})[-_\s]?(\d{2,5})`),
regexp.MustCompile(`(?i)([A-Z]{2,6})(\d{3,5})`),
}
func ExtractCode(filename string) string {
name := filename
if idx := strings.LastIndex(name, "."); idx > 0 {
name = name[:idx]
}
name = strings.ToUpper(name)
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
if prefix == "MP4" || prefix == "AVI" || prefix == "MKV" || prefix == "FHD" || prefix == "UHD" || prefix == "WEB" {
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
