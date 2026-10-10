package actordb

import "time"

// ActorProfile 演员主档，从 AVMedia / AVENMedia 聚合出的唯一演员
type ActorProfile struct {
	ID              uint   `gorm:"primarykey"`
	Name            string `gorm:"uniqueIndex;size:191"` // 主名称（唯一键）
	Aliases         string `gorm:"type:text"`            // JSON 数组，别名
	AvatarURL       string `gorm:"size:512"`             // 远端头像地址
	LocalAvatarPath string `gorm:"size:512"`             // 本地缓存的头像文件路径
	Bio             string `gorm:"type:text"`            // 简介
	EmbyPersonID    string `gorm:"size:64;index"`        // Emby Person ID（匹配成功后写入）
	EmbyMatchStatus string `gorm:"size:32;default:'none'"` // none / matched / not_found / failed
	Source          string `gorm:"size:16"`              // av / aven / both
	AVWorkCount     int    `gorm:"default:0"`
	AVENWorkCount   int    `gorm:"default:0"`
	LastSyncAt      int64  // 与 Emby 最近一次同步时间戳
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (ActorProfile) TableName() string { return "actor_profiles" }

// ActorWork 演员-作品关联（多对多）
type ActorWork struct {
	ID         uint   `gorm:"primarykey"`
	ActorID    uint   `gorm:"index:idx_actor_work,priority:1;not null"`
	MediaType  string `gorm:"size:16;index:idx_actor_work,priority:2;not null"` // av / aven
	MediaID    uint   `gorm:"index:idx_actor_work,priority:3;not null"`
	Code       string `gorm:"size:64;index"`
	Title      string `gorm:"size:255"`
	EmbyItemID string `gorm:"size:64"`
	CreatedAt  time.Time
}

func (ActorWork) TableName() string { return "actor_works" }

// ActorSyncTask 演员同步任务记录
type ActorSyncTask struct {
	ID         uint      `gorm:"primarykey"`
	ActorID    uint      `gorm:"index"`
	ActorName  string    `gorm:"size:191"`
	TaskType   string    `gorm:"size:32"` // aggregate / sync_emby / sync_all
	Status     string    `gorm:"size:32;index"` // pending / running / done / failed
	Message    string    `gorm:"type:text"`
	StartedAt  time.Time
	FinishedAt time.Time
	CreatedAt  time.Time
}

func (ActorSyncTask) TableName() string { return "actor_sync_tasks" }

// EmbyActorConfig Emby 演员联动独立配置（与 models.EmbyConfig 无关）
type EmbyActorConfig struct {
	ID          uint   `gorm:"primarykey"`
	EmbyURL     string `gorm:"size:256"`
	EmbyAPIKey  string `gorm:"size:191"`
	SyncEnabled int    `gorm:"default:0"` // 是否启用定时同步
	SyncCron    string `gorm:"size:64"`   // 定时同步 Cron
	FetchAvatar int    `gorm:"default:1"` // 是否拉头像
	FetchBio    int    `gorm:"default:1"` // 是否拉简介
	FetchWorks  int    `gorm:"default:1"` // 是否拉作品列表
	UpdatedAt   time.Time
}

func (EmbyActorConfig) TableName() string { return "emby_actor_configs" }

// ============ 状态常量 ============

const (
	MatchStatusNone     = "none"
	MatchStatusMatched  = "matched"
	MatchStatusNotFound = "not_found"
	MatchStatusFailed   = "failed"
)

const (
	TaskStatusPending = "pending"
	TaskStatusRunning = "running"
	TaskStatusDone    = "done"
	TaskStatusFailed  = "failed"
)

const (
	TaskTypeAggregate = "aggregate"
	TaskTypeSyncEmby  = "sync_emby"
	TaskTypeSyncAll   = "sync_all"
)

const (
	MediaTypeAV   = "av"
	MediaTypeAVEN = "aven"
)
