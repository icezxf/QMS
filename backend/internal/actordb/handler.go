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
	g.GET("/emby-proxy-config", h.getConfig)
	g.PUT("/emby-proxy-config", h.updateConfig)
	g.POST("/emby-proxy-config/test", h.testConnection)

	g.GET("/emby-proxy-config/script", h.getScript)
	g.PUT("/emby-proxy-config/script", h.saveScript)
	g.DELETE("/emby-proxy-config/script", h.resetScript)
	g.POST("/emby-proxy-config/script/test", h.testScript)

	g.POST("/emby-proxy-scrape/person/:id", h.scrapePerson)

	g.Any("/emby-proxy/*path", h.proxy)
}

// ============ 统一响应格式 ============

func respondOK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": data, "message": "success"})
}

func respondErr(c *gin.Context, msg string) {
	c.JSON(http.StatusOK, gin.H{"code": 400, "data": nil, "message": msg})
}

// ============ 配置 ============

func (h *Handler) getConfig(c *gin.Context) {
	cfg, err := h.Repo.GetConfig()
	if err != nil {
		respondErr(c, err.Error())
		return
	}
	masked := *cfg
	if len(masked.EmbyAPIKey) > 8 { masked.EmbyAPIKey = masked.EmbyAPIKey[:4] + "****" }
	if len(masked.StashDBApiKey) > 8 { masked.StashDBApiKey = masked.StashDBApiKey[:4] + "****" }
	if len(masked.JavStashApiKey) > 8 { masked.JavStashApiKey = masked.JavStashApiKey[:4] + "****" }
	if len(masked.TpdbApiKey) > 8 { masked.TpdbApiKey = masked.TpdbApiKey[:4] + "****" }
	respondOK(c, masked)
}

func (h *Handler) updateConfig(c *gin.Context) {
	var req EmbyActorConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		respondErr(c, err.Error())
		return
	}
	old, _ := h.Repo.GetConfig()
	if req.EmbyAPIKey == "" || strings.Contains(req.EmbyAPIKey, "****") { req.EmbyAPIKey = old.EmbyAPIKey }
	if req.StashDBApiKey == "" || strings.Contains(req.StashDBApiKey, "****") { req.StashDBApiKey = old.StashDBApiKey }
	if req.JavStashApiKey == "" || strings.Contains(req.JavStashApiKey, "****") { req.JavStashApiKey = old.JavStashApiKey }
	if req.TpdbApiKey == "" || strings.Contains(req.TpdbApiKey, "****") { req.TpdbApiKey = old.TpdbApiKey }

	if err := h.Repo.UpdateConfig(&req); err != nil {
		respondErr(c, err.Error())
		return
	}
	respondOK(c, gin.H{"ok": true})
}

func (h *Handler) testConnection(c *gin.Context) {
	var req struct {
		EmbyURL    string `json:"emby_url"`
		EmbyAPIKey string `json:"emby_api_key"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondErr(c, err.Error())
		return
	}
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
		respondOK(c, gin.H{"ok": false, "message": "连接失败: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respondOK(c, gin.H{"ok": false, "message": "Emby 返回 " + resp.Status})
		return
	}
	respondOK(c, gin.H{"ok": true, "message": "连接成功"})
}

// ============ 脚本管理 ============

func (h *Handler) getScript(c *gin.Context) {
	script, isDefault, err := LoadScript()
	if err != nil {
		respondErr(c, err.Error())
		return
	}
	respondOK(c, gin.H{"script": script, "is_default": isDefault})
}

func (h *Handler) saveScript(c *gin.Context) {
	var req struct { Script string `json:"script"` }
	if err := c.ShouldBindJSON(&req); err != nil {
		respondErr(c, err.Error())
		return
	}
	if err := SaveScript(req.Script); err != nil {
		respondErr(c, err.Error())
		return
	}
	respondOK(c, gin.H{"ok": true})
}

func (h *Handler) resetScript(c *gin.Context) {
	if err := ResetScript(); err != nil {
		respondErr(c, err.Error())
		return
	}
	respondOK(c, gin.H{"ok": true, "script": DefaultScrapeScript})
}

func (h *Handler) testScript(c *gin.Context) {
	var req struct {
		Script     string `json:"script"`
		PersonID   string `json:"person_id"`
		PersonName string `json:"person_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondErr(c, err.Error())
		return
	}
	if req.Script == "" { req.Script = DefaultScrapeScript }
	exec, err := h.buildScriptExecutor()
	if err != nil {
		respondErr(c, err.Error())
		return
	}
	vars, results, runErr := exec.Run(c.Request.Context(), req.Script, map[string]any{
		"personId":   req.PersonID,
		"personName": req.PersonName,
	})
	if runErr != nil {
		respondOK(c, gin.H{"ok": false, "error": runErr.Error(), "results": results, "vars": vars})
		return
	}
	respondOK(c, gin.H{"ok": true, "results": results, "vars": vars})
}

// ============ 刮削 ============

func (h *Handler) scrapePerson(c *gin.Context) {
	id := c.Param("id")
	var req struct { Name string `json:"name"` }
	_ = c.ShouldBindJSON(&req)

	exec, err := h.buildScriptExecutor()
	if err != nil {
		respondOK(c, gin.H{"ok": false, "message": err.Error()})
		return
	}

	script, _, err := LoadScript()
	if err != nil {
		respondOK(c, gin.H{"ok": false, "message": "读取脚本失败：" + err.Error()})
		return
	}

	_, results, runErr := exec.Run(c.Request.Context(), script, map[string]any{
		"personId":   id,
		"personName": req.Name,
	})
	if runErr != nil {
		respondOK(c, gin.H{"ok": false, "message": runErr.Error(), "results": results})
		return
	}
	respondOK(c, gin.H{"ok": true, "message": "刮削成功", "results": results})
}

// ============ 通用代理 ============

func (h *Handler) proxy(c *gin.Context) {
	cfg, err := h.Repo.GetConfig()
	if err != nil || cfg.EmbyURL == "" || cfg.EmbyAPIKey == "" {
		respondErr(c, "Emby 未配置")
		return
	}
	path := c.Param("path")
	target := strings.TrimRight(cfg.EmbyURL, "/") + path

	sep := "?"
	if strings.Contains(target, "?") { sep = "&" }
	target += sep + "api_key=" + cfg.EmbyAPIKey

	if qs := c.Request.URL.RawQuery; qs != "" { target += "&" + qs }

	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, target, c.Request.Body)
	if err != nil {
		respondErr(c, err.Error())
		return
	}
	if ct := c.Request.Header.Get("Content-Type"); ct != "" { req.Header.Set("Content-Type", ct) }

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		respondErr(c, err.Error())
		return
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "" { c.Header("Content-Type", ct) }
	if cl := resp.Header.Get("Content-Length"); cl != "" { c.Header("Content-Length", cl) }
	c.Status(resp.StatusCode)
	_, _ = io.Copy(c.Writer, resp.Body)
}

// ============ 工具 ============

func (h *Handler) buildScriptExecutor() (*ScriptExecutor, error) {
	cfg, err := h.Repo.GetConfig()
	if err != nil { return nil, err }
	if cfg.EmbyURL == "" || cfg.EmbyAPIKey == "" { return nil, errConfigMissing }
	
	emby := NewEmbyClient(cfg.EmbyURL, cfg.EmbyAPIKey, "")
	
	stashEndpoint := cfg.StashDBEndpoint
	if stashEndpoint == "" { stashEndpoint = DefaultStashDBEndpoint }
	stash := NewStashBoxClient(stashEndpoint, cfg.StashDBApiKey)
	
	jsEndpoint := cfg.JavStashEndpoint
	if jsEndpoint == "" { jsEndpoint = DefaultJavStashEndpoint }
	javStash := NewStashBoxClient(jsEndpoint, cfg.JavStashApiKey)
	
	tpdb := NewStashBoxClient("https://theporndb.net/graphql", cfg.TpdbApiKey)

	return NewScriptExecutor(emby, stash, javStash, tpdb), nil
}

var errConfigMissing = &configErr{"Emby 未配置"}
type configErr struct{ msg string }
func (e *configErr) Error() string { return e.msg }