package aven

import (
	"path/filepath"

	"qmediasync/internal/helpers"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func Register(r *gin.Engine, db *gorm.DB) error {
	ctrl := NewController(db)
	g := r.Group("/api/aven")
	{
		// 静态图片服务
		g.Static("/images", filepath.Join(helpers.ConfigDir, "aven_images"))

		// 配置
		g.GET("/config", ctrl.GetConfig)
		g.POST("/config", ctrl.SaveConfig)

		// 媒体库
		g.GET("/library", ctrl.ListMedia)
		g.GET("/library/:id", ctrl.GetMedia)
		g.POST("/library/batch-delete", ctrl.BatchDeleteMedia)
		g.POST("/library/:id/release", ctrl.ReleaseMedia)
		g.POST("/library/:id/restart", ctrl.RestartMedia)
		g.POST("/library/:id/cancel", ctrl.CancelMedia)

		// 刮削目录
		g.GET("/paths", ctrl.ListPaths)
		g.POST("/paths", ctrl.CreatePath)
		g.GET("/paths/:id", ctrl.GetPath)
		g.PUT("/paths/:id", ctrl.UpdatePath)
		g.DELETE("/paths/:id", ctrl.DeletePath)
		g.POST("/paths/:id/scan", ctrl.ScanPath)

		// 任务
		g.GET("/tasks", ctrl.ListTasks)
		g.DELETE("/tasks", ctrl.ClearTasks)
	}
	return nil
}