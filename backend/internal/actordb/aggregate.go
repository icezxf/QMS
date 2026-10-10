package actordb

import (
	"encoding/json"
	"strings"

	"qmediasync/internal/helpers"
)

// AggregateNames 从 AVMedia/AVENMedia 挖演员名字
// 每个 loader 返回一批 Actors JSON 字符串
func (r *Repo) AggregateNames(loaders ...func() ([]string, error)) error {
	seen := make(map[string]struct{})
	for _, load := range loaders {
		rows, err := load()
		if err != nil {
			helpers.AppLogger.Warnf("[演员库] 加载数据失败: %v", err)
			continue
		}
		for _, raw := range rows {
			for _, name := range parseActorNames(raw) {
				seen[name] = struct{}{}
			}
		}
	}

	added := 0
	for name := range seen {
		if err := r.UpsertActorName(name); err == nil {
			added++
		}
	}
	helpers.AppLogger.Infof("[演员库] 聚合完成，共发现 %d 位演员", len(seen))
	return nil
}

// parseActorNames 兼容两种 JSON 格式
func parseActorNames(raw string) []string {
	s := strings.TrimSpace(raw)
	if s == "" || s == "[]" || s == "null" {
		return nil
	}

	// 格式 1：[{"name":"..."}, ...]
	var objList []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(s), &objList); err == nil && len(objList) > 0 && objList[0].Name != "" {
		out := make([]string, 0, len(objList))
		for _, o := range objList {
			if n := strings.TrimSpace(o.Name); n != "" {
				out = append(out, n)
			}
		}
		return out
	}

	// 格式 2：["name1", "name2"]
	var simple []string
	if err := json.Unmarshal([]byte(s), &simple); err == nil {
		out := make([]string, 0, len(simple))
		for _, n := range simple {
			if t := strings.TrimSpace(n); t != "" {
				out = append(out, t)
			}
		}
		return out
	}
	return nil
}
