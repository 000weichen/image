package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"imgbed/internal/auth"
	"imgbed/internal/config"
	"imgbed/internal/db"
	"imgbed/internal/handler"
	"imgbed/internal/service"
	"imgbed/internal/storage"
)

// 显式列出前端资源，避免误把 web/ 下的杂项文件打进二进制并公开服务。
//
//go:embed web/index.html web/app.js web/style.css
var webFS embed.FS

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "\n❌ %v\n", err)
		waitEnterIfInteractive()
		os.Exit(1)
	}
}

// run 承载全部启动与关闭逻辑，出错时经由 defer 完成清理后再退出。
func run() error {
	configPath := os.Getenv("IMGBED_CONFIG")
	if configPath == "" {
		configPath = "config.yaml"
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	database, err := db.Open(cfg.Database.Path)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	defer database.Close()

	store, err := storage.New(cfg.Storage)
	if err != nil {
		return fmt.Errorf("初始化存储失败: %w", err)
	}

	repo := service.NewRepository(database)
	imageSvc := service.NewImageService(cfg, store, repo)
	// defer 后进先出：先落盘访问计数，再关数据库。
	defer imageSvc.Close()

	backfillCtx, cancelBackfill := context.WithTimeout(context.Background(), 30*time.Second)
	if updated, err := service.BackfillDimensions(backfillCtx, repo, store); err != nil {
		log.Printf("图片尺寸回填部分失败: %v", err)
	} else if updated > 0 {
		log.Printf("已回填 %d 张图片尺寸", updated)
	}
	cancelBackfill()

	h := handler.New(cfg, database, store, imageSvc)

	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.Default()
	r.Use(func(c *gin.Context) {
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; "+
				"script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		c.Next()
	})
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 前端静态资源：剥离 web/ 前缀后用 NoRoute 兜底，避免与图片路由冲突
	webSub, err := fs.Sub(webFS, "web")
	if err != nil {
		return fmt.Errorf("嵌入前端资源失败: %w", err)
	}
	fileServer := http.FileServer(http.FS(webSub))

	// 首页
	r.GET("/", func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache")
		fileServer.ServeHTTP(c.Writer, c.Request)
	})

	// 图片访问：公开，用于访问统计（filename 含子目录如 ab/hash.jpg）
	r.GET(cfg.ImageURLPath()+"/*filepath", h.ServeImage)
	// 自定义别名访问：公开，用于访问统计。
	r.GET("/s/:alias", h.ServeAlias)
	// 后台预览：公开但不增加访问统计。
	r.GET("/preview/*filepath", h.PreviewImage)

	// API 受 Token 保护
	api := r.Group("/api", auth.Middleware(cfg.Auth.Token))
	{
		api.GET("/config", h.Config)
		api.POST("/upload", h.Upload)
		api.GET("/images", h.ImageList)
		api.GET("/images/:id", h.ImageDetail)
		api.PATCH("/images/:id", h.ImageMove)
		api.PATCH("/images/:id/alias", h.ImageAlias)
		api.DELETE("/images/:id", h.ImageDelete)

		api.GET("/albums", h.AlbumList)
		api.POST("/albums", h.AlbumCreate)
		api.PUT("/albums/:id", h.AlbumUpdate)
		api.DELETE("/albums/:id", h.AlbumDelete)

		api.GET("/stats", h.Stats)
	}

	// 静态资源（style.css, app.js 等）走 NoRoute 兜底
	r.NoRoute(func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache")
		fileServer.ServeHTTP(c.Writer, c.Request)
	})

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("图床服务已启动: http://localhost%s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		return fmt.Errorf("启动失败: %w（可能原因: 端口 %d 已被其他程序占用，请先关闭旧实例再运行）", err, cfg.Server.Port)
	case <-quit:
	}

	log.Println("正在关闭服务...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("关闭服务出错: %v", err)
	}
	log.Println("服务已关闭")
	return nil
}

// waitEnterIfInteractive 在交互式终端（如 Windows 双击运行）下等待回车，
// 让用户来得及看到错误信息；stdin 非终端（systemd/docker/管道）时直接返回。
func waitEnterIfInteractive() {
	st, err := os.Stdin.Stat()
	if err != nil || st.Mode()&os.ModeCharDevice == 0 {
		return
	}
	fmt.Fprint(os.Stderr, "   按回车键退出...")
	_, _ = os.Stdin.Read(make([]byte, 1))
}
