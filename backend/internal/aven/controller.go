package aven

import (
	"fmt"
	"net/http"
	"strconv"

	"qmediasync/internal/helpers"
	"qmediasync/internal/models"
	"qmediasync/internal/synccron"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Controller struct {
	DB  *gorm.DB
	Svc *ServiceEN
}

func NewController(db *gorm.DB) *Controller {
	return &Controller{DB: db, Svc: NewServiceEN(db)}
}

// ============================================================
// 配置
// ============================================================

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

// ============================================================
// 媒体库
// ============================================================

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

	query := c.DB.Model(&models.AVENMedia{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if keyword != "" {
		kw := "%" + keyword + "%"
		query = query.Where("title LIKE ? OR studio LIKE ? OR actors LIKE ?", kw, kw, kw)
	}

	var total int64
	query.Count(&total)

	var list []models.AVENMedia
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

func (c *Controller) ReleaseMedia(ctx *gin.Context) {
	id, _ := strconv.Atoi(ctx.Param("id"))
	media := models.GetAVENMediaByID(uint(id))
	if media == nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "媒体记录不存在"})
		return
	}
	oshash := media.Oshash
	if err := c.Svc.ReleaseMedia(uint(id)); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if oshash != "" {
		SetScanFilterN([]string{oshash})
	}
	c.triggerAllAVENScans()
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "msg": "已放行，正在重新扫描"})
}

func (c *Controller) RestartMedia(ctx *gin.Context) {
	id, _ := strconv.Atoi(ctx.Param("id"))
	media := models.GetAVENMediaByID(uint(id))
	if media == nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "媒体记录不存在"})
		return
	}
	oshash := media.Oshash
	if err := c.Svc.RestartMedia(uint(id)); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if oshash != "" {
		SetScanFilterN([]string{oshash})
	}
	c.triggerAllAVENScans()
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "msg": "已重启，正在重新扫描"})
}

func (c *Controller) CancelMedia(ctx *gin.Context) {
	id, _ := strconv.Atoi(ctx.Param("id"))
	if err := c.Svc.CancelMedia(uint(id)); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "msg": "已取消"})
}

func (c *Controller) BatchDeleteMedia(ctx *gin.Context) {
	var req struct {
		IDs []uint `json:"ids"`
		All bool   `json:"all"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var ids []uint
	if !req.All {
		if len(req.IDs) == 0 {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "未选择任何记录"})
			return
		}
		ids = req.IDs
	}

	count, err := c.Svc.BatchDeleteMedia(ids)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	msg := fmt.Sprintf("已删除 %d 条记录", count)
	if req.All {
		msg = fmt.Sprintf("已清空全部 %d 条记录", count)
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "count": count, "msg": msg})
}

// ============================================================
// 刮削目录
// ============================================================

func (c *Controller) ListPaths(ctx *gin.Context) {
	var list []models.AVENPath
	c.DB.Order("created_at desc").Find(&list)
	ctx.JSON(http.StatusOK, gin.H{"list": list})
}

func (c *Controller) CreatePath(ctx *gin.Context) {
	var p models.AVENPath
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
	var p models.AVENPath
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
	var p models.AVENPath
	if err := c.DB.First(&p, id).Error; err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	ctx.JSON(http.StatusOK, p)
}

func (c *Controller) DeletePath(ctx *gin.Context) {
	id, _ := strconv.Atoi(ctx.Param("id"))
	c.DB.Delete(&models.AVENPath{}, id)
	ctx.JSON(http.StatusOK, gin.H{"ok": true})
}

func (c *Controller) ScanPath(ctx *gin.Context) {
	id, _ := strconv.Atoi(ctx.Param("id"))

	var p models.AVENPath
	if err := c.DB.First(&p, id).Error; err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "欧美刮削目录不存在"})
		return
	}
	if !p.Enable {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "该目录未启用"})
		return
	}
	if p.SourcePath == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "源路径为空"})
		return
	}

	if synccron.CheckNewTaskStatus(uint(id), synccron.SyncTaskTypeAVENScrape) != synccron.TaskStatusNone {
		ctx.JSON(http.StatusOK, gin.H{"ok": true, "msg": "任务已在队列中"})
		return
	}

	task := &synccron.NewSyncTask{
		ID:         uint(id),
		TaskType:   synccron.SyncTaskTypeAVENScrape,
		SourceType: models.SourceType(p.SourceType),
		AccountId:  p.AccountID,
	}
	if err := synccron.AddNewSyncTask(task); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "添加任务到队列失败: " + err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"ok":  true,
		"msg": fmt.Sprintf("欧美刮削任务已加入队列，目录：%s", p.SourcePath),
	})
}

// ============================================================
// 任务记录
// ============================================================

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

	query := c.DB.Model(&models.AVENTask{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if keyword != "" {
		query = query.Where("oshash LIKE ?", "%"+keyword+"%")
	}

	var total int64
	query.Count(&total)

	var list []models.AVENTask
	query.Order("created_at desc").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&list)
	ctx.JSON(http.StatusOK, gin.H{"list": list, "total": total})
}

func (c *Controller) ClearTasks(ctx *gin.Context) {
	c.DB.Where("1 = 1").Delete(&models.AVENTask{})
	ctx.JSON(http.StatusOK, gin.H{"ok": true})
}

// ============================================================
// 触发扫描
// ============================================================

func (c *Controller) triggerAllAVENScans() {
	var paths []models.AVENPath
	if err := c.DB.Where("enable = ?", true).Find(&paths).Error; err != nil {
		helpers.AppLogger.Errorf("[欧美触发] 查询目录失败: %v", err)
		return
	}
	if len(paths) == 0 {
		helpers.AppLogger.Warnf("[欧美触发] 没有启用的目录")
		return
	}
	for _, p := range paths {
		if synccron.CheckNewTaskStatus(p.ID, synccron.SyncTaskTypeAVENScrape) != synccron.TaskStatusNone {
			helpers.AppLogger.Infof("[欧美触发] 目录 %d 已在队列中，跳过", p.ID)
			continue
		}
		task := &synccron.NewSyncTask{
			ID:         p.ID,
			TaskType:   synccron.SyncTaskTypeAVENScrape,
			SourceType: models.SourceType(p.SourceType),
			AccountId:  p.AccountID,
		}
		if err := synccron.AddNewSyncTask(task); err != nil {
			helpers.AppLogger.Warnf("[欧美触发] 目录 %d 加入队列失败: %v", p.ID, err)
		} else {
			helpers.AppLogger.Infof("[欧美触发] 目录 %d 已加入队列", p.ID)
		}
	}
}