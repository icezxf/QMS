package dictdb

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"qmediasync/internal/db"
	"qmediasync/internal/helpers"
	"qmediasync/internal/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var DictDB *gorm.DB

func InitDictDB() error {
	DictDB = db.Db
	if DictDB == nil {
		return fmt.Errorf("QMS 主数据库未初始化，无法加载字典")
	}

	// 确保表存在
	DictDB.AutoMigrate(&models.ActorMapping{}, &models.InfoMapping{}, &models.CNumberMapping{})

	// ★ 启动时：延迟 60 秒执行一次（不管是空表还是旧表，启动时都做一次全量同步）
	go func() {
		time.Sleep(60 * time.Second)
		syncAllDicts(true) 
	}()

	// ★ 核心：启动定时任务，每 7 天更新一次
	go startWeeklyUpdate()

	return nil
}

// 每周定时更新任务
func startWeeklyUpdate() {
	// 设置每周更新（7天 * 24小时）
	ticker := time.NewTicker(7 * 24 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		helpers.AppLogger.Info("[字典] 触发每周定时更新，开始同步 GitHub 最新数据...")
		// 传入 false 表示不是启动时，是定时更新
		syncAllDicts(false)
	}
}

// 同步主逻辑
// isStartup: 如果是启动时，且表里有数据，就跳过；如果是定时更新，强制清空重导
func syncAllDicts(isStartup bool) {
	var count int64
	DictDB.Model(&models.ActorMapping{}).Count(&count)

	if isStartup && count > 0 {
		helpers.AppLogger.Info("[字典] 数据库已存在数据，跳过启动时的全量同步")
		return
	}

	// 如果是定时更新，或者表是空的，清空旧数据准备重新导入
	helpers.AppLogger.Info("[字典] 正在清空旧字典表...")
	DictDB.Exec("TRUNCATE TABLE actor_mappings RESTART IDENTITY;")
	DictDB.Exec("TRUNCATE TABLE info_mappings RESTART IDENTITY;")
	DictDB.Exec("TRUNCATE TABLE c_number_mappings RESTART IDENTITY;")

	helpers.AppLogger.Info("[字典] 清空完毕，开始拉取最新数据...")

	if err := importActorDict(); err != nil {
		helpers.AppLogger.Errorf("[字典] 导入演员字典失败: %v", err)
	}
	if err := importInfoDict(); err != nil {
		helpers.AppLogger.Errorf("[字典] 导入信息字典失败: %v", err)
	}
	if err := importCNumberDict(); err != nil {
		helpers.AppLogger.Errorf("[字典] 导入番号字典失败: %v", err)
	}

	runtime.GC() // 释放内存
	helpers.AppLogger.Info("[字典] 本次字典同步完成！")
}

// ==========================================
// 1. 演员字典
// ==========================================
type actorXML struct {
	Actors []struct {
		ZhCn    string `xml:"zh_cn,attr"`
		ZhTw    string `xml:"zh_tw,attr"`
		Jp      string `xml:"jp,attr"`
		Keyword string `xml:"keyword,attr"`
	} `xml:"a"`
}

func importActorDict() error {
	body, err := download("https://raw.githubusercontent.com/sqzw-x/mdcx/master/resources/mapping_table/mapping_actor.xml")
	if err != nil { return err }

	var data actorXML
	if err := xml.Unmarshal(body, &data); err != nil { return fmt.Errorf("解析演员 XML 失败: %w", err) }
	body = nil

	var mappings []models.ActorMapping
	for _, a := range data.Actors {
		zh := a.ZhCn
		if zh == "" { zh = a.ZhTw }
		if zh == "" || a.Jp == "" { continue }
		for _, jp := range strings.Split(a.Jp, ",") {
			jp = strings.TrimSpace(jp)
			if jp != "" {
				mappings = append(mappings, models.ActorMapping{
					JpName: jp, ZhName: zh, ZhTwName: a.ZhTw, Keyword: a.Keyword,
				})
			}
		}
	}
	return batchInsert(mappings)
}

// ==========================================
// 2. 信息字典
// ==========================================
type infoXML struct {
	Items []struct {
		Type    string `xml:"type,attr"`
		ZhCn    string `xml:"zh_cn,attr"`
		Jp      string `xml:"jp,attr"`
		Keyword string `xml:"keyword,attr"`
	} `xml:"a"`
}

func importInfoDict() error {
	body, err := download("https://raw.githubusercontent.com/sqzw-x/mdcx/master/resources/mapping_table/mapping_info.xml")
	if err != nil { return err }

	var data infoXML
	if err := xml.Unmarshal(body, &data); err != nil { return fmt.Errorf("解析信息 XML 失败: %w", err) }
	body = nil

	var mappings []models.InfoMapping
	for _, item := range data.Items {
		zh := item.ZhCn
		if zh == "" || item.Jp == "" { continue }
		category := item.Type
		if category == "" { category = "tag" }
		mappings = append(mappings, models.InfoMapping{
			Category: category, JpName: item.Jp, ZhName: zh, Keyword: item.Keyword,
		})
	}
	return batchInsert(mappings)
}

// ==========================================
// 3. 番号字典
// ==========================================
func importCNumberDict() error {
	body, err := download("https://raw.githubusercontent.com/sqzw-x/mdcx/master/resources/c_number/c_number.json")
	if err != nil { return err }

	var rawMap map[string]string
	if err := json.Unmarshal(body, &rawMap); err != nil { return fmt.Errorf("解析番号 JSON 失败: %w", err) }
	body = nil

	var mappings []models.CNumberMapping
	for num, title := range rawMap {
		if num == "" || title == "" { continue }
		mappings = append(mappings, models.CNumberMapping{Number: num, Title: title})
	}
	return batchInsert(mappings)
}

// 分批插入（保护 1 核 CPU）
func batchInsert(data interface{}) error {
	switch v := data.(type) {
	case []models.ActorMapping:
		for i := 0; i < len(v); i += 50 {
			end := i + 50; if end > len(v) { end = len(v) }
			if err := DictDB.Clauses(clause.OnConflict{DoNothing: true}).Create(v[i:end]).Error; err != nil { return err }
			time.Sleep(10 * time.Millisecond)
		}
		helpers.AppLogger.Infof("[字典] 成功导入 %d 条演员映射", len(v))
	case []models.InfoMapping:
		for i := 0; i < len(v); i += 50 {
			end := i + 50; if end > len(v) { end = len(v) }
			if err := DictDB.Clauses(clause.OnConflict{DoNothing: true}).Create(v[i:end]).Error; err != nil { return err }
			time.Sleep(10 * time.Millisecond)
		}
		helpers.AppLogger.Infof("[字典] 成功导入 %d 条信息映射", len(v))
	case []models.CNumberMapping:
		for i := 0; i < len(v); i += 50 {
			end := i + 50; if end > len(v) { end = len(v) }
			if err := DictDB.Clauses(clause.OnConflict{DoNothing: true}).Create(v[i:end]).Error; err != nil { return err }
			time.Sleep(10 * time.Millisecond)
		}
		helpers.AppLogger.Infof("[字典] 成功导入 %d 条番号映射", len(v))
	}
	return nil
}

func download(url string) ([]byte, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil { return nil, err }
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK { return nil, fmt.Errorf("HTTP %d", resp.StatusCode) }
	return io.ReadAll(resp.Body)
}