package avscrape

import (
	"fmt"
	"net/http"
	"strconv"

	"qmediasync/internal/models"
	"qmediasync/internal/synccron"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Controller struct {
	DB  *gorm.DB
	Svc *Service
}

func NewController(db *gorm.DB) *Controller {
	return &Controller{DB: db, Svc: NewService(db)}
}

func (c *Controller) GetConfig(ctx *gin.Context) {
	cfg, err := LoadConfig(c.DB)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, cfg)
}

func (c *Controller) SaveConfig(ctx *gin.Context) {
	var cfg Config
	if err := ctx.ShouldBindJSON(&cfg); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := SaveConfig(c.DB, &cfg); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true})
}

// Scrape 手动刮削单个番号，可选带 oshash
// POST /api/avscrape/scrape  { "code": "SNOS-377", "oshash": "可选" }
func (c *Controller) Scrape(ctx *gin.Context) {
	var req struct {
		Code   string `json:"code"`
		Oshash string `json:"oshash"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil || req.Code == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "code required"})
		return
	}
	result, err := c.Svc.Scrape(req.Code, req.Oshash)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, result)
}

// ListMedia 媒体库列表，支持状态筛选和关键字搜索
func (c *Controller) ListMedia(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	status := ctx.DefaultQuery("status", "")
	keyword := ctx.DefaultQuery("keyword", "")

	query := c.DB.Model(&models.AVMedia{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if keyword != "" {
		kw := "%" + keyword + "%"
		query = query.Where("code LIKE ? OR title LIKE ? OR actors LIKE ?", kw, kw, kw)
	}

	var total int64
	query.Count(&total)

	var list []models.AVMedia
	query.Order("updated_at desc").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&list)

	ctx.JSON(http.StatusOK, gin.H{"list": list, "total": total})
}

func (c *Controller) GetMedia(ctx *gin.Context) {
	id, _ := strconv.Atoi(ctx.Param("id"))
	m, err := c.Svc.GetMedia(uint(id))
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	ctx.JSON(http.StatusOK, m)
}

// ReleaseMedia 放行暂停的媒体：跳过失败检查，继续走完流程
func (c *Controller) ReleaseMedia(ctx *gin.Context) {
	id, _ := strconv.Atoi(ctx.Param("id"))
	if err := c.Svc.ReleaseMedia(uint(id)); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "msg": "已放行，下次扫描将强制走完"})
}

// RestartMedia 重启刮削：清理 tmp 和 AVMedia 状态，从头刮削
func (c *Controller) RestartMedia(ctx *gin.Context) {
	id, _ := strconv.Atoi(ctx.Param("id"))
	if err := c.Svc.RestartMedia(uint(id)); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "msg": "已重启，记录和临时文件已清理"})
}

// CancelMedia 取消刮削：清理 tmp 和记录
func (c *Controller) CancelMedia(ctx *gin.Context) {
	id, _ := strconv.Atoi(ctx.Param("id"))
	if err := c.Svc.CancelMedia(uint(id)); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "msg": "已取消，临时文件已清理"})
}

func (c *Controller) ListPaths(ctx *gin.Context) {
	var list []models.AVPath
	c.DB.Order("created_at desc").Find(&list)
	ctx.JSON(http.StatusOK, gin.H{"list": list})
}

func (c *Controller) CreatePath(ctx *gin.Context) {
	var p models.AVPath
	if err := ctx.ShouldBindJSON(&p); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := c.DB.Create(&p).Error; err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, p)
}

func (c *Controller) UpdatePath(ctx *gin.Context) {
	id, _ := strconv.Atoi(ctx.Param("id"))
	var p models.AVPath
	if err := ctx.ShouldBindJSON(&p); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p.ID = uint(id)
	if err := c.DB.Save(&p).Error; err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, p)
}

func (c *Controller) GetPath(ctx *gin.Context) {
	id, _ := strconv.Atoi(ctx.Param("id"))
	var p models.AVPath
	if err := c.DB.First(&p, id).Error; err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	ctx.JSON(http.StatusOK, p)
}

func (c *Controller) DeletePath(ctx *gin.Context) {
	id, _ := strconv.Atoi(ctx.Param("id"))
	c.DB.Delete(&models.AVPath{}, id)
	ctx.JSON(http.StatusOK, gin.H{"ok": true})
}

// ScanPath POST /api/avscrape/paths/:id/scan
// 改成往 synccron 队列加任务，和原模块共用串行队列，避免 115 风控
func (c *Controller) ScanPath(ctx *gin.Context) {
	id, _ := strconv.Atoi(ctx.Param("id"))

	var p models.AVPath
	if err := c.DB.First(&p, id).Error; err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "AV 刮削目录不存在"})
		return
	}
	if !p.Enable {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "该目录未启用，请先编辑并开启「启用」开关"})
		return
	}
	if p.SourcePath == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "源路径为空，请先编辑目录并填写源路径"})
		return
	}

	// 判断是否已经在队列里
	if synccron.CheckNewTaskStatus(uint(id), synccron.SyncTaskTypeAVScrape) != synccron.TaskStatusNone {
		ctx.JSON(http.StatusOK, gin.H{"ok": true, "msg": "任务已在队列中"})
		return
	}

	task := &synccron.NewSyncTask{
		ID:         uint(id),
		TaskType:   synccron.SyncTaskTypeAVScrape,
		SourceType: models.SourceType(p.SourceType),
		AccountId:  p.AccountID,
	}
	if err := synccron.AddNewSyncTask(task); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "添加任务到队列失败: " + err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"ok":  true,
		"msg": fmt.Sprintf("AV 刮削任务已加入队列，目录：%s", p.SourcePath),
	})
}

// ListTasks 任务记录列表，支持状态筛选和番号搜索
func (c *Controller) ListTasks(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	status := ctx.DefaultQuery("status", "")
	keyword := ctx.DefaultQuery("keyword", "")

	query := c.DB.Model(&models.AVTask{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if keyword != "" {
		query = query.Where("code LIKE ?", "%"+keyword+"%")
	}

	var total int64
	query.Count(&total)

	var list []models.AVTask
	query.Order("created_at desc").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&list)
	ctx.JSON(http.StatusOK, gin.H{"list": list, "total": total})
}

func (c *Controller) ClearTasks(ctx *gin.Context) {
	c.DB.Where("1 = 1").Delete(&models.AVTask{})
	ctx.JSON(http.StatusOK, gin.H{"ok": true})
}
