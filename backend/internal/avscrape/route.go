package avscrape

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func Register(r *gin.Engine, db *gorm.DB) error {
	ctrl := NewController(db)
	g := r.Group("/api/avscrape")
	{
		g.GET("/config", ctrl.GetConfig)
		g.POST("/config", ctrl.SaveConfig)
		g.POST("/scrape", ctrl.Scrape)
		g.GET("/library", ctrl.ListMedia)
		g.GET("/library/:id", ctrl.GetMedia)

		// ===== 新增：暂停后的人工干预 =====
		g.POST("/library/:id/release", ctrl.ReleaseMedia)
		g.POST("/library/:id/restart", ctrl.RestartMedia)
		g.POST("/library/:id/cancel", ctrl.CancelMedia)
		// ===================================

		g.GET("/paths", ctrl.ListPaths)
		g.POST("/paths", ctrl.CreatePath)
		g.GET("/paths/:id", ctrl.GetPath)
		g.PUT("/paths/:id", ctrl.UpdatePath)
		g.DELETE("/paths/:id", ctrl.DeletePath)
		g.POST("/paths/:id/scan", ctrl.ScanPath)

		g.GET("/tasks", ctrl.ListTasks)
		g.DELETE("/tasks", ctrl.ClearTasks)
	}
	return nil
}
