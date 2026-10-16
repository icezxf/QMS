package actordb

import (
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	Repo *Repo
}

func NewHandler(repo *Repo) *Handler { return &Handler{Repo: repo} }

func (h *Handler) RegisterRoutes(g *gin.RouterGroup) {
	// 配置读写
	g.GET("/emby-proxy/config", h.getConfig)
	g.PUT("/emby-proxy/config", h.updateConfig)
	g.POST("/emby-proxy/test", h.testConnection)

	// 通用代理（放最后，会吃掉 /emby-proxy/ 后面所有路径）
	g.Any("/emby-proxy/*path", h.proxy)
}

// ============ 配置 ============

func (h *Handler) getConfig(c *gin.Context) {
	cfg, err := h.Repo.GetConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	masked := *cfg
	if len(masked.EmbyAPIKey) > 8 {
		masked.EmbyAPIKey = masked.EmbyAPIKey[:4] + "****"
	}
	c.JSON(http.StatusOK, masked)
}

func (h *Handler) updateConfig(c *gin.Context) {
	var req EmbyActorConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	old, _ := h.Repo.GetConfig()
	// 前端传了掩码或空值，保留原值
	if req.EmbyAPIKey == "" || strings.Contains(req.EmbyAPIKey, "****") {
		req.EmbyAPIKey = old.EmbyAPIKey
	}
	if err := h.Repo.UpdateConfig(&req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) testConnection(c *gin.Context) {
	var req struct {
		EmbyURL    string `json:"emby_url"`
		EmbyAPIKey string `json:"emby_api_key"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// 前端传掩码或空值则用已存的 key
	if req.EmbyAPIKey == "" || strings.Contains(req.EmbyAPIKey, "****") {
		old, _ := h.Repo.GetConfig()
		req.EmbyAPIKey = old.EmbyAPIKey
	}
	if req.EmbyURL == "" {
		old, _ := h.Repo.GetConfig()
		req.EmbyURL = old.EmbyURL
	}
	url := strings.TrimRight(req.EmbyURL, "/") + "/emby/System/Info?api_key=" + req.EmbyAPIKey
	resp, err := http.Get(url)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": "连接失败: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": "Emby 返回 " + resp.Status})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "连接成功"})
}

// ============ 通用代理 ============

// proxy 把所有 /emby-proxy/*path 请求透传到 Emby。
// 例：/emby-proxy/emby/Users/xxx/Views → {emby}/emby/Users/xxx/Views?api_key=xxx
// 例：/emby-proxy/api/v1/people/{id}   → {emby}/api/v1/people/{id}?api_key=xxx
func (h *Handler) proxy(c *gin.Context) {
	cfg, err := h.Repo.GetConfig()
	if err != nil || cfg.EmbyURL == "" || cfg.EmbyAPIKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Emby 未配置"})
		return
	}

	// 拼目标 URL
	path := c.Param("path") // 例如 /emby/Users/xxx/Views
	target := strings.TrimRight(cfg.EmbyURL, "/") + path

	// 拼 api_key
	sep := "?"
	if strings.Contains(target, "?") {
		sep = "&"
	}
	target += sep + "api_key=" + cfg.EmbyAPIKey

	// 保留原请求的 query string（如果有）
	if qs := c.Request.URL.RawQuery; qs != "" {
		target += "&" + qs
	}

	// 构造转发请求
	req, err := http.NewRequestWithContext(
		c.Request.Context(),
		c.Request.Method,
		target,
		c.Request.Body,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if ct := c.Request.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	// 回传响应
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		c.Header("Content-Type", ct)
	}
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		c.Header("Content-Length", cl)
	}
	c.Status(resp.StatusCode)
	_, _ = io.Copy(c.Writer, resp.Body)
}