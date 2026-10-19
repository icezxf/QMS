package dictdb

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"qmediasync/internal/db"
	"qmediasync/internal/helpers"
	"qmediasync/internal/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	DictDB     *gorm.DB
	dictCtx    context.Context
	dictCancel context.CancelFunc

	syncAllDictsMu sync.Mutex

	httpClient = &http.Client{Timeout: 60 * time.Second}
)

const (
	startupSyncDelay = 60 * time.Second
	batchSize        = 200
	maxDownloadBytes = 200 << 20 // 200MB 上限，防止异常响应撑爆内存
	maxRetries       = 3
)

func logInfof(format string, args ...any) {
	if helpers.AppLogger != nil {
		helpers.AppLogger.Infof(format, args...)
	}
}

func logErrorf(format string, args ...any) {
	if helpers.AppLogger != nil {
		helpers.AppLogger.Errorf(format, args...)
	}
}

func InitDictDB() error {
	DictDB = db.Db
	if DictDB == nil {
		return fmt.Errorf("QMS 主数据库未初始化，无法加载字典")
	}

	if err := DictDB.AutoMigrate(&models.ActorMapping{}, &models.InfoMapping{}, &models.CNumberMapping{}); err != nil {
		return fmt.Errorf("字典表迁移失败: %w", err)
	}

	dictCtx, dictCancel = context.WithCancel(context.Background())

	// 启动时延迟执行一次（表为空才真正拉取）
	go func() {
		select {
		case <-dictCtx.Done():
			return
		case <-time.After(startupSyncDelay):
		}
		syncAllDicts(true)
	}()

	// 每周三 3 点定时更新
	go startWeeklyUpdate(dictCtx)

	return nil
}

// StopDictDB 停止后台定时任务（服务关闭时调用）
func StopDictDB() {
	if dictCancel != nil {
		dictCancel()
	}
}

func startWeeklyUpdate(ctx context.Context) {
	for {
		now := time.Now()
		// time.Weekday: Sunday=0, Monday=1, ... Wednesday=3
		daysUntilWednesday := (3 - int(now.Weekday()) + 7) % 7
		nextWed := now.AddDate(0, 0, daysUntilWednesday)
		nextWed = time.Date(nextWed.Year(), nextWed.Month(), nextWed.Day(), 3, 0, 0, 0, nextWed.Location())
		if !nextWed.After(now) {
			nextWed = nextWed.AddDate(0, 0, 7)
		}

		duration := time.Until(nextWed)
		logInfof("[字典] 下次自动更新将在: %s (等待 %v)", nextWed.Format("2006-01-02 15:04:05"), duration.Round(time.Minute))

		timer := time.NewTimer(duration)
		select {
		case <-ctx.Done():
			timer.Stop()
			logInfof("[字典] 定时更新任务已停止")
			return
		case <-timer.C:
		}

		logInfof("[字典] 触发每周三定时更新，开始同步 GitHub 最新数据...")
		syncAllDicts(false)
	}
}

// syncAllDicts
//   - isStartup=true  ：启动时调用，表里有数据就跳过
//   - isStartup=false ：定时更新调用，强制全量刷新
//
// ★ 关键改进：先下载解析成功，再事务内清空+插入。任何一步失败，旧数据保持不变。
func syncAllDicts(isStartup bool) {
	syncAllDictsMu.Lock()
	defer syncAllDictsMu.Unlock()

	if DictDB == nil {
		logErrorf("[字典] 数据库未初始化，跳过同步")
		return
	}

	var count int64
	if err := DictDB.Model(&models.ActorMapping{}).Count(&count).Error; err != nil {
		logErrorf("[字典] 统计演员字典失败: %v", err)
		return
	}

	if isStartup && count > 0 {
		logInfof("[字典] 数据库已存在数据（演员映射 %d 条），跳过启动时的全量同步", count)
		return
	}

	logInfof("[字典] 开始拉取最新数据...")

	// 1. 先把三份数据全部下载并解析成功（失败则直接返回，不碰数据库）
	actors, err := fetchActorMappings()
	if err != nil {
		logErrorf("[字典] 获取演员字典失败，保留旧数据: %v", err)
		return
	}
	infos, err := fetchInfoMappings()
	if err != nil {
		logErrorf("[字典] 获取信息字典失败，保留旧数据: %v", err)
		return
	}
	cnumbers, err := fetchCNumberMappings()
	if err != nil {
		logErrorf("[字典] 获取番号字典失败，保留旧数据: %v", err)
		return
	}

	logInfof("[字典] 拉取完成：演员 %d 条，信息 %d 条，番号 %d 条，开始写入数据库",
		len(actors), len(infos), len(cnumbers))

	// 2. 事务内清空 + 插入，失败自动回滚
	err = DictDB.Transaction(func(tx *gorm.DB) error {
		if err := clearDictTables(tx); err != nil {
			return err
		}
		if err := batchInsert(tx, actors); err != nil {
			return fmt.Errorf("写入演员字典失败: %w", err)
		}
		if err := batchInsert(tx, infos); err != nil {
			return fmt.Errorf("写入信息字典失败: %w", err)
		}
		if err := batchInsert(tx, cnumbers); err != nil {
			return fmt.Errorf("写入番号字典失败: %w", err)
		}
		return nil
	})
	if err != nil {
		logErrorf("[字典] 同步失败，已回滚，旧数据保持不变: %v", err)
		return
	}

	runtime.GC()
	logInfof("[字典] 本次字典同步完成！")
}

// clearDictTables 按方言清空字典表并重置自增
func clearDictTables(tx *gorm.DB) error {
	tables := []string{"actor_mappings", "info_mappings", "c_number_mappings"}
	dialect := tx.Dialector.Name()

	for _, table := range tables {
		switch dialect {
		case "sqlite":
			if err := tx.Exec(fmt.Sprintf("DELETE FROM %s", table)).Error; err != nil {
				return fmt.Errorf("清空 %s 失败: %w", table, err)
			}
			// sqlite_sequence 仅在用过 AUTOINCREMENT 时存在，忽略错误
			tx.Exec(fmt.Sprintf("DELETE FROM sqlite_sequence WHERE name='%s'", table))
		case "mysql":
			if err := tx.Exec(fmt.Sprintf("TRUNCATE TABLE %s", table)).Error; err != nil {
				return fmt.Errorf("清空 %s 失败: %w", table, err)
			}
		case "postgres":
			if err := tx.Exec(fmt.Sprintf("TRUNCATE TABLE %s RESTART IDENTITY", table)).Error; err != nil {
				return fmt.Errorf("清空 %s 失败: %w", table, err)
			}
		default:
			if err := tx.Exec(fmt.Sprintf("DELETE FROM %s", table)).Error; err != nil {
				return fmt.Errorf("清空 %s 失败: %w", table, err)
			}
		}
	}
	return nil
}

// ==========================================
// 数据拉取与解析
// ==========================================

type actorXML struct {
	Actors []struct {
		ZhCn    string `xml:"zh_cn,attr"`
		ZhTw    string `xml:"zh_tw,attr"`
		Jp      string `xml:"jp,attr"`
		Keyword string `xml:"keyword,attr"`
	} `xml:"a"`
}

func fetchActorMappings() ([]models.ActorMapping, error) {
	body, err := download("https://raw.githubusercontent.com/sqzw-x/amane/main/resources/mapping_table/mapping_actor.xml")
	if err != nil {
		return nil, err
	}
	var data actorXML
	if err := xml.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("解析演员 XML 失败: %w", err)
	}

	mappings := make([]models.ActorMapping, 0, len(data.Actors))
	for _, a := range data.Actors {
		zh := a.ZhCn
		if zh == "" {
			zh = a.ZhTw
		}
		if zh == "" || a.Jp == "" {
			continue
		}
		for _, jp := range strings.Split(a.Jp, ",") {
			jp = strings.TrimSpace(jp)
			if jp != "" {
				mappings = append(mappings, models.ActorMapping{
					JpName: jp, ZhName: zh, ZhTwName: a.ZhTw, Keyword: a.Keyword,
				})
			}
		}
	}
	return mappings, nil
}

type infoXML struct {
	Items []struct {
		Type    string `xml:"type,attr"`
		ZhCn    string `xml:"zh_cn,attr"`
		Jp      string `xml:"jp,attr"`
		Keyword string `xml:"keyword,attr"`
	} `xml:"a"`
}

func fetchInfoMappings() ([]models.InfoMapping, error) {
	body, err := download("https://raw.githubusercontent.com/sqzw-x/amane/main/resources/mapping_table/mapping_info.xml")
	if err != nil {
		return nil, err
	}
	var data infoXML
	if err := xml.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("解析信息 XML 失败: %w", err)
	}

	mappings := make([]models.InfoMapping, 0, len(data.Items))
	for _, item := range data.Items {
		zh := item.ZhCn
		if zh == "" || item.Jp == "" {
			continue
		}
		category := item.Type
		if category == "" {
			category = "tag"
		}
		mappings = append(mappings, models.InfoMapping{
			Category: category, JpName: item.Jp, ZhName: zh, Keyword: item.Keyword,
		})
	}
	return mappings, nil
}

func fetchCNumberMappings() ([]models.CNumberMapping, error) {
	body, err := download("https://raw.githubusercontent.com/sqzw-x/mdcx/master/resources/c_number/c_number.json")
	if err != nil {
		return nil, err
	}
	var rawMap map[string]string
	if err := json.Unmarshal(body, &rawMap); err != nil {
		return nil, fmt.Errorf("解析番号 JSON 失败: %w", err)
	}

	mappings := make([]models.CNumberMapping, 0, len(rawMap))
	for num, title := range rawMap {
		if num == "" || title == "" {
			continue
		}
		mappings = append(mappings, models.CNumberMapping{Number: num, Title: title})
	}
	return mappings, nil
}

func batchInsert(tx *gorm.DB, data interface{}) error {
	switch v := data.(type) {
	case []models.ActorMapping:
		for i := 0; i < len(v); i += batchSize {
			end := min(i+batchSize, len(v))
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(v[i:end]).Error; err != nil {
				return err
			}
		}
		logInfof("[字典] 成功导入 %d 条演员映射", len(v))
	case []models.InfoMapping:
		for i := 0; i < len(v); i += batchSize {
			end := min(i+batchSize, len(v))
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(v[i:end]).Error; err != nil {
				return err
			}
		}
		logInfof("[字典] 成功导入 %d 条信息映射", len(v))
	case []models.CNumberMapping:
		for i := 0; i < len(v); i += batchSize {
			end := min(i+batchSize, len(v))
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(v[i:end]).Error; err != nil {
				return err
			}
		}
		logInfof("[字典] 成功导入 %d 条番号映射", len(v))
	}
	return nil
}

// download 带重试的下载，修复了原代码"先 Close 再 Read"的致命 bug
func download(url string) ([]byte, error) {
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		body, err := downloadOnce(url)
		if err == nil {
			return body, nil
		}
		lastErr = err
		logErrorf("[字典] 下载失败（第 %d/%d 次）: %s: %v", attempt, maxRetries, url, err)
		if attempt < maxRetries {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}
	}
	return nil, lastErr
}

func downloadOnce(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() // ★ 改成 defer，在 ReadAll 之后才关闭

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxDownloadBytes))
}
