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

// 启动并同步字典
func InitDictDB() error {
	// ★ 核心：直接复用 QMS 现有的数据库连接！
	DictDB = db.Db
	if DictDB == nil {
		return fmt.Errorf("QMS 主数据库未初始化，无法加载字典")
	}

	// 自动建表（防止漏建）
	DictDB.AutoMigrate(&models.ActorMapping{}, &models.InfoMapping{}, &models.CNumberMapping{})

	// ★ 1核 CPU 优化：延迟 60 秒启动，避开 QMS 开机高峰
	go func() {
		time.Sleep(60 * time.Second)
		syncAllDicts()
	}()

	return nil
}

func syncAllDicts() {
	helpers.AppLogger.Info("[字典] 开始检查并同步 GitHub 字典数据...")

	var count int64
	DictDB.Model(&models.ActorMapping{}).Count(&count)
	if count == 0 {
		if err := importActorDict(); err != nil {
			helpers.AppLogger.Errorf("[字典] 导入演员字典失败: %v", err)
		}
	}

	DictDB.Model(&models.InfoMapping{}).Count(&count)
	if count == 0 {
		if err := importInfoDict(); err != nil {
			helpers.AppLogger.Errorf("[字典] 导入信息字典失败: %v", err)
		}
	}

	DictDB.Model(&models.CNumberMapping{}).Count(&count)
	if count == 0 {
		if err := importCNumberDict(); err != nil {
			helpers.AppLogger.Errorf("[字典] 导入番号字典失败: %v", err)
		}
	}

	runtime.GC()
}

// ==========================================
// 1. 演员字典导入 (mapping_actor.xml)
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
	helpers.AppLogger.Info("[字典] 正在下载演员字典...")
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
// 2. 信息字典导入 (mapping_info.xml)
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
	helpers.AppLogger.Info("[字典] 正在下载信息字典...")
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
// 3. 番号字典导入 (c_number.json)
// ==========================================
func importCNumberDict() error {
	helpers.AppLogger.Info("[字典] 正在下载番号字典...")
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

// 批量插入，防止 1 核 CPU 卡死
func batchInsert(data interface{}) error {
	// 使用 GORM 的反射或接口断言处理不同类型的切片
	switch v := data.(type) {
	case []models.ActorMapping:
		for i := 0; i < len(v); i += 50 {
			end := i + 50
			if end > len(v) { end = len(v) }
			if err := DictDB.Clauses(clause.OnConflict{DoNothing: true}).Create(v[i:end]).Error; err != nil { return err }
			time.Sleep(10 * time.Millisecond)
		}
		helpers.AppLogger.Infof("[字典] 成功导入 %d 条演员映射", len(v))
	case []models.InfoMapping:
		for i := 0; i < len(v); i += 50 {
			end := i + 50
			if end > len(v) { end = len(v) }
			if err := DictDB.Clauses(clause.OnConflict{DoNothing: true}).Create(v[i:end]).Error; err != nil { return err }
			time.Sleep(10 * time.Millisecond)
		}
		helpers.AppLogger.Infof("[字典] 成功导入 %d 条信息映射", len(v))
	case []models.CNumberMapping:
		for i := 0; i < len(v); i += 50 {
			end := i + 50
			if end > len(v) { end = len(v) }
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
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK { return nil, fmt.Errorf("HTTP %d", resp.StatusCode) }
	return io.ReadAll(resp.Body)
}