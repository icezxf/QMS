package main

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"qmediasync/emby302/config"
	emby302https "qmediasync/emby302/util/https"
	"qmediasync/emby302/util/logs/colors"
	"qmediasync/emby302/web"
	"qmediasync/internal/aven"
	"qmediasync/internal/avscrape"
	"qmediasync/internal/backup"
	"qmediasync/internal/controllers"
	"qmediasync/internal/db"
	"qmediasync/internal/db/database"
	"qmediasync/internal/directoryupload"
	"qmediasync/internal/github"
	"qmediasync/internal/helpers"
	"qmediasync/internal/models"
	"qmediasync/internal/playback"
	"qmediasync/internal/realtime"
	"qmediasync/internal/synccron"
	"qmediasync/internal/syncstrm"
	"qmediasync/internal/v115open"

	"github.com/gin-gonic/gin"
)

var Version string = "v0.0.1"
var PublishDate string = "2025-08-08"
var FANART_API_KEY = ""
var TMDB_ACCESS_TOKEN = ""
var TMDB_API_KEY = ""
var SC_API_KEY = ""
var OAuthRelayEncryptionKey = ""
var Update bool = false

var AppName string = "QMediaSync"
var QMSApp *App
var requestStatWriter *models.RequestStatWriter
var instanceLock *os.File

func parseBuildUnixTime(value string) int64 {
	if value == "" {
		return 0
	}
	if timestamp, err := helpers.ParseRFC3339Unix(value); err == nil {
		return timestamp
	}
	layouts := []string{
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed.Unix()
		}
	}
	return 0
}

type App struct {
	isRelease   bool
	httpServer  *http.Server
	httpsServer *http.Server
	version     string
	publishDate string
}

func (app *App) Start() {
	startEmby302()
	if helpers.IsRelease {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(controllers.Cors())
	setRouter(r)
	app.StartHttpServer(r)
	app.StartHttpsServer(r)
	if runtime.GOOS == "windows" {
		go func() {
			quit := make(chan os.Signal, 1)
			signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
			<-quit
			log.Println("收到 Ctrl+C 信号")
			helpers.ExitChan <- struct{}{}
		}()
		<-helpers.ExitChan
		log.Println("收到停止信号")
		app.Stop()
		close(helpers.ExitChan)
		log.Println("应用程序正常退出")
		return
	} else {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		log.Println("收到停止信号")
		app.Stop()
		log.Println("应用程序正常退出")
	}
}

func (app *App) Stop() {
	realtime.GlobalLifecycle.Shutdown()
	app.shutdownHTTPServers()
	synccron.PauseAllNewSyncQueues()
	models.GlobalDownloadQueue.Stop()
	models.GlobalUploadQueue.Stop()
	directoryupload.StopDirectoryUploadService()
	syncstrm.StopStrmGenerationWorker()
	synccron.GlobalCron.Stop()
	cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
	if err := playback.DefaultManager.Shutdown(cleanupCtx); err != nil {
		helpers.AppLogger.Warnf("等待 115 多端播放清理退出失败：%v", err)
	}
	cancelCleanup()
	if err := v115open.ClosePlaybackClient(); err != nil {
		helpers.AppLogger.Warnf("关闭 115 播放客户端失败：%v", err)
	}
	if requestStatWriter != nil {
		requestStatWriter.Close()
	}
	helpers.CloseLogger()
}

func (app *App) shutdownHTTPServers() {
	if app.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.httpServer.Shutdown(ctx); err != nil {
			log.Println("HTTP Server Shutdown:", err)
		}
	}
	if app.httpsServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.httpsServer.Shutdown(ctx); err != nil {
			log.Println("HTTPS Server Shutdown:", err)
		}
	}
}

func (app *App) StartHttpsServer(r *gin.Engine) {
	certFile := filepath.Join(helpers.RootDir, "config", "server.crt")
	keyFile := filepath.Join(helpers.RootDir, "config", "server.key")
	if !helpers.PathExists(certFile) || !helpers.PathExists(keyFile) {
		return
	}
	go func() {
		sslHost := ""
		if !helpers.IsRelease {
			sslHost = "localhost:12332"
		} else {
			sslHost = helpers.GlobalConfig.HttpsHost
		}
		app.httpsServer = &http.Server{
			Addr:    sslHost,
			Handler: r,
		}
		weberr := app.httpsServer.ListenAndServeTLS(certFile, keyFile)
		if weberr != nil {
			fmt.Println("ListenAndServe error:", weberr)
		}
	}()
}

func (app *App) StartHttpServer(r *gin.Engine) {
	host := helpers.GlobalConfig.HttpHost
	app.httpServer = &http.Server{
		Addr:    host,
		Handler: r,
	}
	go func() {
		weberr := app.httpServer.ListenAndServe()
		if weberr != nil {
			fmt.Println("ListenAndServe error:", weberr)
		}
	}()
}

func (app *App) StartDatabase() error {
	if err := helpers.GlobalConfig.Db.Validate(); err != nil {
		return err
	}
	if helpers.GlobalConfig.Db.Engine == helpers.DbEngineSqlite {
		sqliteFile := filepath.Join(helpers.ConfigDir, helpers.GlobalConfig.Db.SqliteFile)
		helpers.AppLogger.Infof("SQLite 数据库文件路径：%s", sqliteFile)
		db.Db = db.InitSqlite3(sqliteFile)
		models.Migrate()
		if err := models.ResetStaleEmbySyncRunOnStartup(); err != nil {
			return err
		}
		return nil
	}
	dbConfig := &database.Config{
		Host:         helpers.GlobalConfig.Db.PostgresConfig.Host,
		Port:         helpers.GlobalConfig.Db.PostgresConfig.Port,
		User:         helpers.GlobalConfig.Db.PostgresConfig.User,
		Password:     helpers.GlobalConfig.Db.PostgresConfig.Password,
		DBName:       helpers.GlobalConfig.Db.PostgresConfig.Database,
		SSLMode:      "disable",
		MaxOpenConns: helpers.GlobalConfig.Db.PostgresConfig.MaxOpenConns,
		MaxIdleConns: helpers.GlobalConfig.Db.PostgresConfig.MaxIdleConns,
	}
	if helpers.GlobalConfig.Db.PostgresConfig.SSL {
		dbConfig.SSLMode = "require"
	}
	if err := db.ConnectPostgres(dbConfig); err != nil {
		return err
	}
	models.Migrate()
	if err := models.ResetStaleEmbySyncRunOnStartup(); err != nil {
		return err
	}
	return nil
}

func configureInitialAdminSetup() error {
	hasUser, err := models.HasAnyUser()
	if err != nil {
		return err
	}
	token, err := controllers.ConfigureInitialSetup(!hasUser)
	if err != nil {
		return err
	}
	if token != "" {
		helpers.AppLogger.RequiredWarnf(
			"检测到系统尚未创建管理员，请使用以下初始化码完成首次管理员创建：%s",
			token,
		)
		helpers.AppLogger.RequiredWarnf("初始化码只会在本次启动日志中显示，创建管理员成功后立即失效")
	}
	return nil
}

func newApp() {
	if QMSApp != nil {
		log.Println("App 已经初始化，不能再次初始化")
		return
	}
	QMSApp = &App{
		isRelease:   helpers.IsRelease,
		version:     Version,
		publishDate: PublishDate,
	}
}

func initTimeZone() {
	cstZone := time.FixedZone("CST", 8*3600)
	time.Local = cstZone
}

func checkRelease() {
	if helpers.IsRunningInDocker() {
		helpers.IsRelease = true
	}
	arg1 := strings.ToLower(os.Args[0])
	name := strings.ToLower(filepath.Base(arg1))
	helpers.IsRelease = strings.Index(name, "qmediasync") == 0 && !strings.Contains(arg1, "go-build")
}

func getRootDir() string {
	var exPath string = "/app"
	checkRelease()
	if os.Getenv("TRIM_APPDEST") != "" {
		helpers.RootDir = os.Getenv("TRIM_APPDEST")
		return helpers.RootDir
	}
	if helpers.IsRelease {
		ex, err := os.Executable()
		if err != nil {
			panic(err)
		}
		exPath = filepath.Dir(ex)
	} else {
		exPath, _ = os.Getwd()
	}
	helpers.RootDir = exPath
	return exPath
}

func getDataAndConfigDir() error {
	resolvedDir, err := resolveConfigDir("")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(resolvedDir, 0o755); err != nil {
		return err
	}
	lock, err := helpers.AcquireInstanceLock(resolvedDir)
	if err != nil {
		return err
	}
	instanceLock = lock

	var appData string
	var configDir string
	needMk := false
	if runtime.GOOS == "windows" {
		appData := os.Getenv("LOCALAPPDATA")
		if appData == "" {
			appData = os.Getenv("APPDATA")
		}
		oldConfigDir := filepath.Join(appData, AppName, "config")
		configDir = filepath.Join(helpers.RootDir, "config")
		err := os.MkdirAll(configDir, 0755)
		if err != nil {
			fmt.Printf("创建配置目录失败：%v\n", err)
			panic("创建配置目录失败")
		}
		helpers.ConfigDir = configDir
		if helpers.PathExists(oldConfigDir) {
			err := helpers.MoveConfigDir(oldConfigDir, configDir)
			if err != nil {
				return fmt.Errorf("迁移旧配置目录失败：%w", err)
			}
		}
	} else {
		if os.Getenv("TRIM_PKGETC") == "" {
			appData = helpers.RootDir
			configDir = filepath.Join(appData, "config")
			needMk = true
			helpers.ConfigDir = configDir
		} else {
			oldConfigDir := os.Getenv("TRIM_PKGETC")
			configDir = os.Getenv("TRIM_DATA_SHARE_PATHS")
			if configDir == "" {
				configDir = oldConfigDir
				needMk = false
			} else {
				configDir = filepath.Join(configDir, "config")
				needMk = true
				if helpers.PathExists(oldConfigDir) && oldConfigDir != configDir {
					if !helpers.IsDirEmpty(oldConfigDir) {
						err := os.MkdirAll(configDir, 0755)
						if err != nil {
							log.Printf("创建配置目录失败：%v\n", err)
							panic("创建配置目录失败")
						}
						err = helpers.MoveConfigDir(oldConfigDir, configDir)
						if err != nil {
							return fmt.Errorf("迁移旧配置目录失败：%w", err)
						}
						needMk = false
					}
				}
			}
			helpers.ConfigDir = configDir
		}
	}
	if needMk {
		err := os.MkdirAll(configDir, 0755)
		if err != nil {
			log.Printf("创建配置目录失败：%v\n", err)
			panic("创建配置目录失败")
		}
	}
	return nil
}

//go:embed emby302.yaml
//go:embed assets/db_config.html
var embedFiles embed.FS

func startEmby302() {
	dataRoot := helpers.ConfigDir
	data, err := embedFiles.ReadFile("emby302.yaml")
	if err != nil {
		log.Fatal(err)
	}
	if err := config.ReadFromFile(data); err != nil {
		log.Fatal(err)
	}
	if models.GlobalEmbyConfig == nil || models.GlobalEmbyConfig.EmbyUrl == "" {
		helpers.AppLogger.Warnf("Emby 302 未配置 Emby 地址，跳过启动 Emby 302 服务")
		return
	}
	emby302https.ConfigureClient(emby302https.ClientOptions{
		InsecureSkipVerify: helpers.GlobalConfig.Emby302.InsecureSkipVerify,
	})
	if helpers.GlobalConfig.Emby302.InsecureSkipVerify {
		helpers.AppLogger.RequiredWarnf("Emby 302 已开启 insecure_skip_verify，出站 HTTPS 请求将跳过证书校验，存在中间人攻击风险，仅建议在受控内网自签名证书场景临时使用")
	}
	config.C.Emby.Host = models.GlobalEmbyConfig.EmbyUrl
	config.C.Emby.ImagesOriginal = helpers.GlobalConfig.Emby302.ImagesOriginal
	config.C.Emby.EpisodesUnplayPrior = false
	certFile := filepath.Join(dataRoot, "server.crt")
	keyFile := filepath.Join(dataRoot, "server.key")
	if helpers.PathExists(certFile) && helpers.PathExists(keyFile) {
		config.C.Ssl.Enable = true
		config.C.Ssl.SinglePort = false
		config.C.Ssl.Crt = "server.crt"
		config.C.Ssl.Key = "server.key"
	}
	config.BasePath = dataRoot
	config.C.Emby.LocalMediaRoot = "/"
	config.C.VideoPreview.Enable = true
	config.C.VideoPreview.Containers = []string{"strm"}
	go func() {
		if err := web.Listen(); err != nil {
			log.Fatal(colors.ToRed(err.Error()))
		}
	}()
}

func initLogger() {
	logPath := filepath.Join(helpers.ConfigDir, "logs")
	os.MkdirAll(logPath, 0755)
	syncLogPath := filepath.Join(helpers.ConfigDir, helpers.SyncLogDir())
	os.MkdirAll(syncLogPath, 0755)
	logConfig := helpers.LogConfigSnapshot()
	helpers.AppLogger = helpers.NewLogger(logConfig.App, true, true)
	helpers.V115Log = helpers.NewLogger(logConfig.V115, false, true)
	helpers.OpenListLog = helpers.NewLogger(logConfig.OpenList, false, true)
	helpers.TMDBLog = helpers.NewLogger(logConfig.TMDB, false, true)
	helpers.BaiduPanLog = helpers.NewLogger(logConfig.BaiduPan, false, true)
	helpers.WarnUnsafeSensitiveLogIfEnabled()
}

func initOthers() {
	helpers.InitEventBus()
	models.LoadSettings()
	github.InitManager(models.SettingsGlobal.HttpProxy)
	helpers.AppLogger.Infof("已加载配置，准备初始化 115 请求队列，线程数：%d", models.SettingsGlobal.FileDetailThreads)
	qps := models.SettingsGlobal.FileDetailThreads
	if qps <= 0 {
		qps = 2
	}
	v115open.SetGlobalExecutorConfig(qps, qps*60, qps*3600)
	models.LoadScrapeSettings()
	models.InitDQ()
	models.InitUQ()
	models.InitNotificationManager()
	controllers.StartListenTelegramBot()
	models.GetEmbyConfig()
	helpers.SubscribeSync(helpers.V115TokenInValidEvent, models.HandleV115TokenInvalid)
	helpers.SubscribeSync(helpers.SaveOpenListTokenEvent, models.HandleOpenListTokenSaveSync)
	models.FailAllRunningSyncTasks()

	requestStatWriter = models.NewRequestStatWriter()
	v115open.SetGlobalExecutorStatSaver(requestStatWriter.Enqueue)
	synccron.RefreshOAuthAccessToken()

	// 启动同步任务队列管理器
	synccron.InitNewSyncQueueManager()

	// ===== 注册 AV 刮削回调 =====
	synccron.AVScanHandler = func(pathID uint) error {
		scanner := avscrape.NewScanner(db.Db)
		return scanner.Scan(pathID)
	}
	// =============================

	// ===== 注册欧美刮削回调 =====
	synccron.AVENScanHandler = func(pathID uint) error {
		scanner := aven.NewScannerEN(db.Db)
		return scanner.Scan(pathID)
	}
	// ===============================

	models.InitEmbyLibraryRefreshCoordinator()
	syncstrm.InitStrmGenerationWorker()
	directoryupload.InitDirectoryUploadService()
	realtime.GlobalEventHub = realtime.NewEventHub()
	realtime.GlobalSyncTaskHub = realtime.NewSyncTaskHub()
	synccron.InitCron()
	synccron.InitSyncCron()
	synccron.InitScrapeCron()
	synccron.InitTokenCron()
	models.InitBackupService()
	models.ResetScrapePathStatus()
	models.UpdateScrapeMediaStatus(models.ScrapeMediaStatusScraping, models.ScrapeMediaStatusScanned, 0)
	models.UpdateScrapeMediaStatus(models.ScrapeMediaStatusRenaming, models.ScrapeMediaStatusScraped, 0)
	models.UpdateUploadingToPending()
	models.UpdateDownloadingToPending()
	helpers.Subscribe(helpers.BackupCronEevent, func(event helpers.Event) {
		backup.Backup("定时", "定时备份")
	})
	helpers.Subscribe(helpers.StrmSyncCompleteEvent, func(event helpers.Event) {
		scrapePathIds := event.Data.([]uint)
		for _, scrapePathId := range scrapePathIds {
			scrapePath := models.GetScrapePathByID(scrapePathId)
			if scrapePath == nil {
				helpers.AppLogger.Errorf("获取刮削目录失败：%v", scrapePathId)
				continue
			}
			taskObj := &synccron.NewSyncTask{
				ID:           scrapePathId,
				SourcePath:   "",
				SourcePathId: "",
				TargetPath:   "",
				AccountId:    scrapePath.AccountId,
				SourceType:   scrapePath.SourceType,
				IsFile:       false,
				TaskType:     synccron.SyncTaskTypeScrape,
			}
			if err := synccron.AddNewSyncTask(taskObj); err != nil {
				helpers.AppLogger.Errorf("添加刮削任务失败：%v", err)
			} else {
				helpers.AppLogger.Infof("创建刮削任务成功并已添加到执行队列，刮削目录 ID：%d", scrapePathId)
			}
		}
	})
}

// 设置路由
func setRouter(r *gin.Engine) {
	webStatisPath := filepath.Join(helpers.RootDir, "web_statics")
	r.LoadHTMLFiles(filepath.Join(webStatisPath, "index.html"))
	r.StaticFile("/favicon.ico", filepath.Join(webStatisPath, "favicon.ico"))
	r.StaticFS("/assets", http.Dir(filepath.Join(webStatisPath, "assets")))
	r.GET("/", func(c *gin.Context) {
		c.HTML(200, "index.html", gin.H{})
	})
	r.POST("/emby/webhook", controllers.Webhook)
	r.POST("/api/login", controllers.LoginAction)
	r.POST("/api/strm/webhook", controllers.StrmWebhook)
	r.GET("/api/setup/status", controllers.SetupStatusAction)
	r.POST("/api/setup/admin", controllers.CreateInitialAdminAction)
	r.GET("/api/session", controllers.SessionAction)
	r.GET("/115/url/*filename", controllers.Get115UrlByPickCode)
	r.GET("/115/newurl", controllers.Get115UrlByPickCode)
	r.GET("/baidupan/url/*filename", controllers.GetBaiduPanUrlByPickCode)
	r.GET("/openlist/url", controllers.GetOpenListFileUrl)
	r.GET("/proxy-115", controllers.Proxy115)
	r.POST("/api/update-fn-access-path", controllers.UpdateFNPath)

	// ===== 注册 AV 刮削路由 =====
	if err := avscrape.Register(r, db.Db); err != nil {
		helpers.AppLogger.Errorf("注册 AV 刮削路由失败：%v", err)
	}
	// =============================

	// ===== 注册欧美刮削路由 =====
	if err := aven.Register(r, db.Db); err != nil {
		helpers.AppLogger.Errorf("注册欧美刮削路由失败：%v", err)
	}
	// ===============================

	// 需要 JWT 验证的 API 路由
	api := r.Group("/api")
	api.Use(controllers.JWTAuthMiddleware())
	{
		api.GET("/scrape/tmp-image", controllers.ScrapeTmpImage)
		api.GET("/scrape/records/export", controllers.ExportScrapeRecords)
		api.GET("/logs/stream", controllers.LogStream)
		api.GET("/events/stream", controllers.EventStream)
		api.GET("/sync/tasks/:id/stream", controllers.SyncTaskStream)
		api.GET("/logs/old", controllers.GetOldLogs)
		api.GET("/logs/download", controllers.DownloadLogFile)
		api.GET("/path/is-fn-os", controllers.IsFnOS)

		api.GET("/version", func(c *gin.Context) {
			c.JSON(http.StatusOK, map[string]any{
				"version":    Version,
				"build_time": parseBuildUnixTime(PublishDate),
				"date":       PublishDate,
				"isWindows":  runtime.GOOS == "windows",
				"isRelease":  helpers.IsRelease,
			})
		})
		api.POST("/database/delete-all-table", controllers.DeleteAllTabble)
		api.GET("/announce", controllers.GetAnnounce)
		api.POST("/database/repair", controllers.RepairDB)
		api.POST("/auth/115-qrcode-open", controllers.GetLoginQrCodeOpen)
		api.POST("/auth/115-qrcode-status", controllers.GetQrCodeStatus)
		api.GET("/115/status", controllers.Get115Status)
		api.GET("/115/appids", controllers.GetV115AppIDSources)
		api.GET("/115/oauth-url", controllers.GetOAuthUrl)
		api.POST("115/oauth-confirm", controllers.ConfirmOAuthCode)
		api.GET("/115/oauth-status", controllers.GetOAuthStatus)
		api.GET("/115/queue/stats", controllers.GetQueueStats)
		api.POST("/115/queue/rate-limit", controllers.SetQueueRateLimit)
		api.GET("/115/stats/daily", controllers.GetRequestStatsByDay)
		api.GET("/115/stats/hourly", controllers.GetRequestStatsByHour)
		api.POST("/115/stats/clean", controllers.CleanOldRequestStats)
		api.GET("/baidupan/oauth-url", controllers.GetBaiDuPanOAuthUrl)
		api.POST("/baidupan/oauth-confirm", controllers.ConfirmBaiDuPanOAuthCode)
		api.GET("/baidupan/status", controllers.GetBaiDuPanStatus)

		api.GET("/update/last", controllers.GetLastRelease)
		api.POST("/update/to-version", controllers.UpdateToVersion)
		api.GET("/update/progress", controllers.UpdateProgress)
		api.POST("/update/cancel", controllers.CancelUpdate)

		api.GET("/user/info", controllers.GetUserInfo)
		api.POST("/logout", controllers.LogoutAction)
		api.GET("/user/sessions", controllers.ListUserSessions)
		api.DELETE("/user/sessions/:session_id", controllers.RevokeUserSessionAction)
		api.POST("/user/sessions/revoke-others", controllers.RevokeOtherUserSessionsAction)
		api.GET("/user/two-factor/status", controllers.GetTwoFactorStatus)
		api.POST("/user/two-factor/setup", controllers.SetupTwoFactor)
		api.POST("/user/two-factor/enable", controllers.EnableTwoFactor)
		api.POST("/user/two-factor/disable", controllers.DisableTwoFactor)
		api.GET("/path/sort-options", controllers.GetBrowseSortOptions)
		api.GET("/path/list", controllers.GetPathList)
		api.POST("/path/create", controllers.CreateDir)
		api.DELETE("/path", controllers.DeleteDir)
		api.POST("/path/delete-batch", controllers.DeleteFiles)
		api.POST("/path/move", controllers.MoveFiles)
		api.POST("/path/copy", controllers.CopyFiles)
		api.POST("/path/rename", controllers.RenameFile)
		api.GET("/path/files", controllers.GetNetFileList)
		api.POST("/user/change", controllers.ChangePassword)

		api.POST("/setting/http-proxy", controllers.UpdateHttpProxy)
		api.GET("/setting/http-proxy", controllers.GetHttpProxy)
		api.POST("/setting/test-http-proxy", controllers.TestHttpProxy)
		api.GET("/setting/log", controllers.GetLogSetting)
		api.POST("/setting/log", controllers.UpdateLogSetting)
		api.GET("/setting/notification/channels", controllers.GetNotificationChannels)
		api.POST("/setting/notification/channels/telegram", controllers.CreateTelegramChannel)
		api.GET("/setting/notification/channels/telegram/:id", controllers.GetTelegramChannel)
		api.PUT("/setting/notification/channels/telegram", controllers.UpdateTelegramChannel)
		api.POST("/setting/notification/channels/meow", controllers.CreateMeoWChannel)
		api.GET("/setting/notification/channels/meow/:id", controllers.GetMeoWChannel)
		api.PUT("/setting/notification/channels/meow", controllers.UpdateMeoWChannel)
		api.POST("/setting/notification/channels/bark", controllers.CreateBarkChannel)
		api.GET("/setting/notification/channels/bark/:id", controllers.GetBarkChannel)
		api.PUT("/setting/notification/channels/bark", controllers.UpdateBarkChannel)
		api.POST("/setting/notification/channels/serverchan", controllers.CreateServerChanChannel)
		api.GET("/setting/notification/channels/serverchan/:id", controllers.GetServerChanChannel)
		api.PUT("/setting/notification/channels/serverchan", controllers.UpdateServerChanChannel)
		api.POST("/setting/notification/channels/webhook", controllers.CreateCustomWebhookChannel)
		api.GET("/setting/notification/channels/webhook/:id", controllers.GetCustomWebhookChannel)
		api.PUT("/setting/notification/channels/webhook", controllers.UpdateCustomWebhookChannel)
		api.POST("/setting/notification/channels/status", controllers.UpdateChannelStatus)
		api.DELETE("/setting/notification/channels/:id", controllers.DeleteChannel)
		api.GET("/setting/notification/rules", controllers.GetNotificationRules)
		api.PUT("/setting/notification/rules", controllers.UpdateNotificationRule)
		api.POST("/setting/notification/channels/test", controllers.TestChannelConnection)
		api.GET("/setting/strm-config", controllers.GetStrmConfig)
		api.POST("/setting/strm-config", controllers.UpdateStrmConfig)
		api.GET("/setting/cron", controllers.GetCronNextTime)
		api.POST("/cron/validate", controllers.ValidateCron)
		api.POST("/setting/emby/parse", controllers.ParseEmby)
		api.GET("/setting/emby-config", controllers.GetEmbyConfig)
		api.POST("/setting/emby-config", controllers.UpdateEmbyConfig)
		api.POST("/setting/threads", controllers.UpdateThreads)
		api.GET("/setting/threads", controllers.GetThreads)

		api.POST("/emby/sync/start", controllers.StartEmbySync)
		api.GET("/emby/sync/status", controllers.GetEmbySyncStatus)
		api.GET("/emby/libraries", controllers.GetEmbyLibraries)

		api.POST("/sync/start", controllers.StartSync)
		api.GET("/sync/records", controllers.GetSyncRecords)
		api.GET("/sync/task", controllers.GetSyncTask)
		api.GET("/sync/path-list", controllers.GetSyncPathList)
		api.POST("/sync/paths", controllers.CreateSyncPathAggregate)
		api.PUT("/sync/paths/:id", controllers.UpdateSyncPathAggregate)
		api.POST("/sync/path-delete", controllers.DeleteSyncPath)
		api.POST("/sync/path/stop", controllers.StopSyncByPath)
		api.POST("/sync/path/start", controllers.StartSyncByPath)
		api.POST("/sync/path/full-start", controllers.FullStart115Sync)
		api.POST("/sync/delete-records", controllers.DelSyncRecords)
		api.POST("/sync/path/toggle-cron", controllers.ToggleSyncByPath)
		api.GET("/sync/path/:id", controllers.GetSyncPathById)
		api.GET("/sync/path/:id/scrape-paths", controllers.GetRelScrapePath)
		api.POST("/sync/path/scrape-paths", controllers.SaveRelScrapePath)
		api.POST("/sync/manual", controllers.ManualSync)

		api.GET("/directory-upload/rules", controllers.ListDirectoryUploadRules)
		api.POST("/directory-upload/sync-paths/:sync_path_id/scan", controllers.ScanDirectoryUploadSyncPathRules)
		api.GET("/directory-upload/runtime-status", controllers.GetDirectoryUploadRuntimeStatuses)

		api.GET("/account/list", controllers.GetAccountList)
		api.POST("/account/add", controllers.CreateTmpAccount)
		api.POST("/account/authorization/prepare", controllers.PrepareAccountAuthorization)
		api.POST("/account/authorization/cancel", controllers.CancelAccountAuthorization)
		api.POST("/account/update", controllers.UpdateAccountInfo)
		api.POST("/account/delete", controllers.DeleteAccount)
		api.POST("/account/openlist", controllers.CreateOpenListAccount)

		api.POST("/api-keys", controllers.CreateAPIKey)
		api.GET("/api-keys", controllers.ListAPIKeys)
		api.PUT("/api-keys/:id/status", controllers.UpdateAPIKeyStatus)
		api.DELETE("/api-keys/:id", controllers.DeleteAPIKey)

		api.GET("/scrape/movie-genre", controllers.GetMovieGenre)
		api.GET("/scrape/tvshow-genre", controllers.GetTvshowGenre)
		api.GET("/scrape/language", controllers.GetLanguage)
		api.GET("/scrape/countries", controllers.GetCountries)
		api.GET("/scrape/tmdb", controllers.GetTmdbSettings)
		api.POST("/scrape/tmdb", controllers.SaveTmdbSettings)
		api.POST("/scrape/tmdb-test", controllers.TestTmdbSettings)
		api.GET("/scrape/ai-settings", controllers.GetAiSettings)
		api.POST("/scrape/ai-settings", controllers.SaveAiSettings)
		api.POST("/scrape/ai-test", controllers.TestAiSettings)
		api.GET("/scrape/movie-categories", controllers.GetMovieCategories)
		api.GET("/scrape/tvshow-categories", controllers.GetTvshowCategories)
		api.POST("/scrape/movie-categories", controllers.SaveMovieCategory)
		api.POST("/scrape/tvshow-categories", controllers.SaveTvshowCategory)
		api.DELETE("/scrape/movie-categories/:id", controllers.DeleteMovieCategory)
		api.DELETE("/scrape/tvshow-categories/:id", controllers.DeleteTvshowCategory)
		api.GET("/scrape/pathes", controllers.GetScrapePathes)
		api.POST("/scrape/pathes", controllers.SaveScrapePath)
		api.DELETE("/scrape/pathes/:id", controllers.DeleteScrapePath)
		api.GET("/scrape/pathes/:id", controllers.GetScrapePath)
		api.POST("/scrape/pathes/start", controllers.ScanScrapePath)
		api.POST("/scrape/pathes/stop", controllers.StopScrape)
		api.POST("/scrape/pathes/toggle-cron", controllers.ToggleScrapePathCron)
		api.GET("/scrape/records", controllers.GetScrapeRecords)
		api.POST("/scrape/re-scrape", controllers.ReScrape)
		api.POST("/scrape/clear-failed", controllers.ClearFailedScrapeRecords)
		api.POST("/scrape/truncate-all", controllers.TruncateAllScrapeRecords)
		api.DELETE("/scrape/records", controllers.DeleteScrapeMediaFile)
		api.POST("/scrape/finish", controllers.FinishScrapeMediaFile)
		api.POST("/scrape/rename-failed", controllers.RenameFailedScrapeMediaFile)
		api.POST("/scrape/sync-pathes", controllers.SaveScrapeStrmPath)
		api.GET("/scrape/sync-pathes", controllers.GetScrapeStrmPaths)
		api.GET("/scrape/tmdb-search", controllers.TmdbSearch)

		api.GET("/upload/queue", controllers.UploadList)
		api.POST("/upload/queue/clear-pending", controllers.ClearPendingUploadTasks)
		api.POST("/upload/queue/start", controllers.StartUploadQueue)
		api.POST("/upload/queue/stop", controllers.StopUploadQueue)
		api.GET("/upload/queue/status", controllers.UploadQueueStatus)
		api.POST("/upload/queue/clear-success-failed", controllers.ClearUploadSuccessAndFailedTasks)
		api.POST("/upload/queue/retry-failed", controllers.RetryFailedUploadTasks)

		api.GET("/download/queue", controllers.DownloadList)
		api.POST("/download/queue/clear-pending", controllers.ClearPendingDownloadTasks)
		api.POST("/download/queue/start", controllers.StartDownloadQueue)
		api.POST("/download/queue/stop", controllers.StopDownloadQueue)
		api.GET("/download/queue/status", controllers.DownloadQueueStatus)
		api.POST("/download/queue/clear-success-failed", controllers.ClearDownloadSuccessAndFailedTasks)
		api.POST("/download/queue/retry-failed", controllers.RetryFailedDownloadTasks)

		api.GET("/backup/list", controllers.GetBackupList)
		api.GET("/backup/records/:id", controllers.GetBackupRecord)
		api.POST("/backup/create", controllers.CreateBackup)
		api.DELETE("/backup/records/:id", controllers.DeleteBackup)
		api.POST("/backup/restore", controllers.RestoreFromBackup)
		api.POST("/backup/upload-restore", controllers.UploadAndRestore)
		api.GET("/backup/download/:id", controllers.DownloadBackup)
		api.GET("/backup/config", controllers.GetBackupConfig)
		api.PUT("/backup/config", controllers.UpdateBackupConfig)
		api.GET("/backup/status", controllers.GetBackupStatus)
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func checkLegacyDatabaseState() error {
	backupPath := filepath.Join(helpers.ConfigDir, "backups", "migrate.zip")
	if _, err := os.Lstat(backupPath); err == nil {
		return fmt.Errorf("发现遗留迁移包 %s，此版本已不提供自动迁移，请先妥善处理旧数据；迁移包已保留", backupPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("检查旧迁移包失败：%w", err)
	}
	if helpers.HasConfigFile() {
		return nil
	}
	postgresDir := filepath.Join(helpers.ConfigDir, "postgres")
	if _, err := os.Lstat(postgresDir); err == nil {
		return fmt.Errorf("缺少配置文件，但发现旧 PostgreSQL 数据目录 %s，此版本已移除内嵌数据库和自动迁移，请先妥善处理旧数据；目录已保留", postgresDir)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("检查旧 PostgreSQL 数据目录失败：%w", err)
	}
	return nil
}

func initEnv() bool {
	log.Printf("当前版本号：%s，发布日期：%s\n", Version, PublishDate)
	helpers.Version = Version
	helpers.ReleaseDate = PublishDate
	helpers.LoadEnvFromFile(filepath.Join(helpers.RootDir, "config", ".env"))
	helpers.DEFAULT_SC_API_KEY = firstNonEmpty(os.Getenv("SC_API_KEY"), SC_API_KEY)
	helpers.DEFAULT_TMDB_API_KEY = firstNonEmpty(os.Getenv("TMDB_API_KEY"), TMDB_API_KEY)
	helpers.DEFAULT_TMDB_ACCESS_TOKEN = firstNonEmpty(os.Getenv("TMDB_ACCESS_TOKEN"), TMDB_ACCESS_TOKEN)
	helpers.DEFAULT_FANART_API_KEY = firstNonEmpty(os.Getenv("FANART_API_KEY"), FANART_API_KEY)
	helpers.FANART_API_KEY = helpers.DEFAULT_FANART_API_KEY
	helpers.OAuthRelayEncryptionKey = firstNonEmpty(os.Getenv("OAUTH_RELAY_ENCRYPTION_KEY"), OAuthRelayEncryptionKey)
	initTimeZone()
	if err := getDataAndConfigDir(); err != nil {
		log.Printf("初始化配置目录失败：%v", err)
		return false
	}
	log.Printf("当前工作目录：%s\n", helpers.RootDir)
	log.Printf("当前配置文件目录：%s\n", helpers.ConfigDir)
	if err := checkLegacyDatabaseState(); err != nil {
		log.Printf("数据库状态不支持：%v", err)
		return false
	}
	ipv4, _ := helpers.GetLocalIP()
	log.Printf("本机 IPv4 地址：%s\n", ipv4)
	helpers.IsFirstRun = !helpers.HasConfigFile()
	if helpers.IsFirstRun {
		log.Printf("配置文件不存在，启动简单配置服务：%s", helpers.ConfigFilePath())
		StartConfigWebServer()
		return false
	}
	configPath := helpers.ExistingConfigFilePath()
	log.Printf("配置文件存在，加载配置文件：%s", configPath)
	err := helpers.InitConfig()
	if err != nil {
		log.Printf("初始化配置文件失败：%v", err)
		return false
	}
	if err := helpers.InitEncryptionKey(); err != nil {
		log.Printf("初始化本机加密密钥失败：%v\n", err)
		return false
	}
	initLogger()
	newApp()
	helpers.AppLogger.Infof("当前版本号：%s，发布日期：%s", Version, PublishDate)

	if err := QMSApp.StartDatabase(); err != nil {
		helpers.AppLogger.Errorf("数据库启动失败：%v", err)
		return false
	}

	if err := configureInitialAdminSetup(); err != nil {
		helpers.AppLogger.Errorf("初始化管理员创建状态失败：%v", err)
		return false
	}

	db.InitCache()
	initOthers()
	return true
}

func parseParams() {
	var update string
	flag.String("guid", "", "兼容旧部署参数；进程身份由启动环境决定")
	flag.BoolVar(&helpers.IsFnOS, "fnos", false, "是否是飞牛环境")
	flag.StringVar(&update, "update", "", "更新参数")
	registerAdminRecoveryFlags(flag.CommandLine, &adminRecoveryOptions{})
	flag.Parse()
	if helpers.IsFnOS {
		log.Printf("当前环境为飞牛环境\n")
	}
	if update != "" && runtime.GOOS == "windows" {
		Update = true
	}
}

// @title QMediaSync API
// @version 1.0
// @description 媒体同步和刮削系统 API
// @host localhost:8115
// @BasePath /
// @securityDefinitions.apikey JwtAuth
// @in header
// @name Authorization
// @securityDefinitions.apikey ApiKeyAuth
// @in query
// @name api_key
func main() {
	if handled, code := runAdminRecoveryCommand(os.Args[1:]); handled {
		os.Exit(code)
	}
	parseParams()
	getRootDir()
	if Update {
		if err := runUpdateProcess(); err != nil {
			log.Printf("更新失败：%v", err)
			os.Exit(1)
		}
		return
	}
	defer func() {
		if instanceLock != nil {
			if err := instanceLock.Close(); err != nil {
				log.Printf("释放实例锁失败：%v", err)
			}
		}
	}()
	if !initEnv() {
		panic("初始化环境失败")
	}
	if runtime.GOOS == "windows" {
		if helpers.IsRelease {
			go QMSApp.Start()
			helpers.StartApp(func() {
				QMSApp.Stop()
			})
		} else {
			QMSApp.Start()
		}
	} else {
		QMSApp.Start()
	}
}

func runUpdateProcess() error {
	if len(os.Args) < 3 {
		return errors.New("更新参数不足")
	}
	updateDir := os.Args[2]
	parentPID := os.Getppid()
	fmt.Printf("等待父进程退出（PID：%d）…\n", parentPID)
	if err := waitForProcessExit(parentPID); err != nil {
		return fmt.Errorf("等待父进程退出失败：%w", err)
	}

	appPath := filepath.Join(helpers.RootDir, "QMediaSync.exe")
	if err := controllers.InstallReleaseFiles(updateDir, helpers.RootDir, "QMediaSync.exe"); err != nil {
		return errors.Join(err, exec.Command(appPath).Start())
	}
	if err := os.RemoveAll(updateDir); err != nil {
		log.Printf("清理更新目录失败：%v", err)
	}
	if err := exec.Command(appPath).Start(); err != nil {
		return fmt.Errorf("启动新版本失败：%w", err)
	}
	fmt.Println("更新完成，新版本已启动")
	return nil
}

func waitForProcessExit(pid int) error {
	maxWait := 30 * time.Second
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		alive, err := helpers.IsProcessAlive(pid)
		if err != nil {
			return err
		}
		if !alive {
			fmt.Printf("父进程已退出，等待资源释放…\n")
			time.Sleep(2 * time.Second)
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("进程 %d 在 %s 内未退出", pid, maxWait)
}

func isInRestrictedDirectory() (bool, string) {
	if runtime.GOOS != "windows" {
		return false, ""
	}
	exePath, err := os.Executable()
	if err != nil {
		return false, ""
	}
	exeDir := filepath.Dir(exePath)
	driveLetter := strings.ToUpper(string(exeDir[0]))
	log.Printf("应用程序路径：%s，盘符：%s", exePath, driveLetter)
	if driveLetter == "C" {
		return true, "应用程序位于 C 盘，建议将应用程序移动到其他盘符（如 D 盘、E 盘等）以避免权限问题"
	}
	restrictedPaths := []string{
		"Program Files",
		"Program Files (x86)",
		"ProgramData",
		"Windows",
	}
	for _, restrictedPath := range restrictedPaths {
		log.Printf("检查目录：%s，是否包含受限路径：%s", exeDir, restrictedPath)
		if strings.Contains(exeDir, restrictedPath) {
			return true, fmt.Sprintf("应用程序位于受限目录 %s 中，建议将应用程序移动到普通用户目录或其他非系统目录", restrictedPath)
		}
	}
	return false, ""
}

type databaseConfigRequest struct {
	Engine       helpers.DbEngine     `json:"engine"`
	PostgresType helpers.PostgresType `json:"postgresType"`
	Host         string               `json:"host"`
	Port         int                  `json:"port"`
	User         string               `json:"user"`
	Password     string               `json:"password"`
	Database     string               `json:"database"`
	SSL          bool                 `json:"ssl"`
	DropDatabase bool                 `json:"dropDatabase"`
}

func (req databaseConfigRequest) toConfig() (*helpers.Config, error) {
	config := helpers.MakeDefaultConfig()
	config.Db.Engine = req.Engine
	config.Db.PostgresType = req.PostgresType
	if req.Engine == helpers.DbEnginePostgres {
		config.Db.PostgresConfig = helpers.PostgresConfig{
			Host:         req.Host,
			Port:         req.Port,
			User:         req.User,
			Password:     req.Password,
			Database:     req.Database,
			SSL:          req.SSL,
			MaxOpenConns: 25,
			MaxIdleConns: 25,
		}
	}
	if err := config.Db.Validate(); err != nil {
		return nil, err
	}
	return config, nil
}

func StartConfigWebServer() {
	if helpers.IsRelease {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.Default()
	data, err := embedFiles.ReadFile("assets/db_config.html")
	if err != nil {
		log.Fatal(err)
	}
	tmpl := template.Must(template.New("db_config.html").Parse(string(data)))
	r.SetHTMLTemplate(tmpl)

	r.GET("/", func(c *gin.Context) {
		isRestricted, warningMsg := isInRestrictedDirectory()
		c.HTML(200, "db_config.html", gin.H{
			"title":        "数据库配置",
			"isRestricted": isRestricted,
			"warningMsg":   warningMsg,
			"isWindows":    runtime.GOOS == "windows",
		})
	})

	r.POST("/api/config/test-db", func(c *gin.Context) {
		var req struct {
			Host     string `json:"host"`
			Port     int    `json:"port"`
			User     string `json:"user"`
			Password string `json:"password"`
			Database string `json:"database"`
			SSL      bool   `json:"ssl"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"success": false, "error": err.Error()})
			return
		}
		sslMode := "disable"
		if req.SSL {
			sslMode = "require"
		}
		connStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=postgres sslmode=%s",
			req.Host, req.Port, req.User, req.Password, sslMode)
		sqlDB, err := sql.Open("postgres", connStr)
		if err != nil {
			c.JSON(200, gin.H{"success": false, "error": "连接失败：" + err.Error()})
			return
		}
		defer sqlDB.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := sqlDB.PingContext(ctx); err != nil {
			c.JSON(200, gin.H{"success": false, "error": "连接失败：" + err.Error()})
			return
		}
		var dbExists bool
		err = sqlDB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", req.Database).Scan(&dbExists)
		if err != nil {
			c.JSON(200, gin.H{"success": false, "error": "检查数据库失败：" + err.Error()})
			return
		}
		if !dbExists {
			c.JSON(200, gin.H{"success": true, "message": "数据库连接成功", "dbExists": false})
			return
		}
		connStrDb := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			req.Host, req.Port, req.User, req.Password, req.Database, sslMode)
		sqlDBDb, err := sql.Open("postgres", connStrDb)
		if err != nil {
			c.JSON(200, gin.H{"success": true, "message": "数据库连接成功", "dbExists": true, "hasOtherTables": false})
			return
		}
		defer sqlDBDb.Close()
		var tableCount int
		err = sqlDBDb.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name NOT LIKE 'gorr_%'").Scan(&tableCount)
		if err != nil {
			c.JSON(200, gin.H{"success": true, "message": "数据库连接成功", "dbExists": true, "hasOtherTables": false})
			return
		}
		c.JSON(200, gin.H{"success": true, "message": "数据库连接成功", "dbExists": true, "hasOtherTables": tableCount > 0})
	})

	r.POST("/api/config/save", func(c *gin.Context) {
		var req databaseConfigRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		yamlConfig, err := req.toConfig()
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		if err := checkLegacyDatabaseState(); err != nil {
			c.JSON(409, gin.H{"error": err.Error()})
			return
		}
		if req.Engine == helpers.DbEnginePostgres {
			quotedDatabase, err := database.QuotePostgresIdentifier(req.Database)
			if err != nil {
				c.JSON(200, gin.H{"error": "数据库名不合法：" + err.Error()})
				return
			}
			if req.DropDatabase {
				sslMode := "disable"
				if req.SSL {
					sslMode = "require"
				}
				connStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=postgres sslmode=%s",
					req.Host, req.Port, req.User, req.Password, sslMode)
				sqlDB, err := sql.Open("postgres", connStr)
				if err == nil {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					sqlDB.ExecContext(ctx, "DROP DATABASE IF EXISTS "+quotedDatabase)
					sqlDB.Close()
				}
			}
		}
		if err := helpers.SaveConfig(yamlConfig); err != nil {
			c.JSON(500, gin.H{"error": "保存配置失败：" + err.Error()})
			return
		}
		c.JSON(200, gin.H{"success": true, "message": "配置已保存，配置服务已退出。重启后请查看启动日志中的初始化码，并在 Web 页面创建首个管理员。"})
		go func() {
			time.Sleep(1 * time.Second)
			os.Exit(0)
		}()
	})

	fmt.Printf("配置服务已启动，请在浏览器中访问：http://ip:12333\n")
	go func() {
		time.Sleep(2 * time.Second)
		helpers.OpenBrowser("http://127.0.0.1:12333")
	}()
	if err := r.Run(":12333"); err != nil {
		log.Fatalf("启动配置服务失败：%v", err)
	}
}