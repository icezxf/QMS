package actordb

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	Repo     *Repo
	LoadAV   func() ([]string, error)
	LoadAVEN func() ([]string, error)
}

func NewHandler(repo *Repo) *Handler {
	return &Handler{Repo: repo}
}

func (h *Handler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/actor/list", h.listActors)
	g.GET("/actor/:id", h.getActor)
	g.GET("/actor/:id/avatar", h.getAvatar) // ← 新增：头像代理
	g.DELETE("/actor/:id", h.deleteActor)

	g.POST("/actor/aggregate", h.aggregate)
	g.POST("/actor/sync-stashdb", h.syncStashDB)
	g.POST("/actor/push-emby", h.pushEmby)

	g.GET("/actor/config", h.getConfig)
	g.PUT("/actor/config", h.updateConfig)
}

func (h *Handler) listActors(c *gin.Context) {
	page := atoiDefault(c.Query("page"), 1)
	size := atoiDefault(c.Query("size"), 20)
	list, total, err := h.Repo.ListActors(page, size, c.Query("search"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"list": list, "total": total})
}

func (h *Handler) getActor(c *gin.Context) {
	id := uint(atoiDefault(c.Param("id"), 0))
	p, err := h.Repo.GetActorByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "演员不存在"})
		return
	}
	c.JSON(http.StatusOK, p)
}

// getAvatar 头像代理：读取本地头像文件返回给浏览器
func (h *Handler) getAvatar(c *gin.Context) {
	id := uint(atoiDefault(c.Param("id"), 0))
	p, err := h.Repo.GetActorByID(id)
	if err != nil || p.AvatarURL == "" {
		c.Status(http.StatusNotFound)
		return
	}
	c.File(p.AvatarURL)
}

func (h *Handler) deleteActor(c *gin.Context) {
	id := uint(atoiDefault(c.Param("id"), 0))
	if err := h.Repo.DeleteActor(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) aggregate(c *gin.Context) {
	if h.LoadAV == nil || h.LoadAVEN == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "数据源未注入"})
		return
	}
	go func() {
		_ = h.Repo.AggregateNames(h.LoadAV, h.LoadAVEN)
	}()
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) syncStashDB(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"ok": false, "message": "StashDB 同步待实现"})
}

func (h *Handler) pushEmby(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"ok": false, "message": "Emby 推送待实现"})
}

func (h *Handler) getConfig(c *gin.Context) {
	cfg, err := h.Repo.GetEmbyConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	masked := *cfg
	if len(masked.EmbyAPIKey) > 8 {
		masked.EmbyAPIKey = masked.EmbyAPIKey[:4] + "****"
	}
	if len(masked.StashDBKey) > 8 {
		masked.StashDBKey = masked.StashDBKey[:4] + "****"
	}
	c.JSON(http.StatusOK, masked)
}

func (h *Handler) updateConfig(c *gin.Context) {
	var req EmbyActorConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	old, _ := h.Repo.GetEmbyConfig()
	if isMasked(req.EmbyAPIKey) {
		req.EmbyAPIKey = old.EmbyAPIKey
	}
	if isMasked(req.StashDBKey) {
		req.StashDBKey = old.StashDBKey
	}
	if err := h.Repo.UpdateEmbyConfig(&req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func isMasked(s string) bool {
	return s == "" || (len(s) >= 8 && s[4:] == "****")
}

func atoiDefault(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return def
	}
	return n
}
