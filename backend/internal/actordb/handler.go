package actordb

import (
	"fmt"
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
	// ⚠️ 静态路径必须放前面，否则会被 /actor/:id 吃掉
	g.GET("/actor/list", h.listActors)
	g.GET("/actor/config", h.getConfig)
	g.PUT("/actor/config", h.updateConfig)

	g.GET("/actor/emby/libraries", h.listEmbyLibraries)
	g.GET("/actor/emby/persons", h.listEmbyPersons)
	g.GET("/actor/emby/person/:id/image", h.getEmbyPersonImage)
	g.GET("/actor/emby/person/:id", h.getEmbyPersonDetail)
	g.POST("/actor/emby/test", h.testEmbyConnection)

	g.POST("/actor/aggregate", h.aggregate)
	g.POST("/actor/sync-stashdb", h.syncStashDB)
	g.POST("/actor/push-emby", h.pushEmby)

	// ⚠️ 动态路径放最后
	g.GET("/actor/:id/avatar", h.getAvatar)
	g.GET("/actor/:id", h.getActor)
	g.DELETE("/actor/:id", h.deleteActor)
}

// ============ 演员库 ============

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

// ============ 聚合 / 同步 ============

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

// ============ 配置 ============

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

// ============ Emby 演员浏览 ============

func (h *Handler) getEmbyClient() (*EmbyClient, error) {
	cfg, err := h.Repo.GetEmbyConfig()
	if err != nil {
		return nil, err
	}
	if cfg.EmbyURL == "" || cfg.EmbyAPIKey == "" {
		return nil, fmt.Errorf("Emby 未配置")
	}
	return NewEmbyClient(cfg.EmbyURL, cfg.EmbyAPIKey, cfg.EmbyUser), nil
}

func (h *Handler) listEmbyLibraries(c *gin.Context) {
	cli, err := h.getEmbyClient()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	libs, err := cli.ListLibraries(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"list": libs})
}

func (h *Handler) listEmbyPersons(c *gin.Context) {
	cli, err := h.getEmbyClient()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	libID := c.Query("library_id")
	if libID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 library_id"})
		return
	}
	persons, err := cli.ListPersonsInLibrary(c.Request.Context(), libID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"list": persons, "total": len(persons)})
}

func (h *Handler) getEmbyPersonDetail(c *gin.Context) {
	cli, err := h.getEmbyClient()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	detail, err := cli.GetPersonDetail(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, detail)
}

func (h *Handler) getEmbyPersonImage(c *gin.Context) {
	cli, err := h.getEmbyClient()
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	data, ct, err := cli.FetchImage(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	if ct == "" {
		ct = "image/jpeg"
	}
	c.Data(http.StatusOK, ct, data)
}

func (h *Handler) testEmbyConnection(c *gin.Context) {
	var req struct {
		EmbyURL    string `json:"emby_url"`
		EmbyAPIKey string `json:"emby_api_key"`
		EmbyUser   string `json:"emby_user"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	cli := NewEmbyClient(req.EmbyURL, req.EmbyAPIKey, req.EmbyUser)

	if err := cli.TestConnection(c.Request.Context()); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": "连接失败: " + err.Error()})
		return
	}
	userID, err := cli.ResolveUserID(c.Request.Context(), req.EmbyUser)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "连接成功，用户 ID = " + userID})
}

// ============ utils ============

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