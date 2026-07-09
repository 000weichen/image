package handler

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"log"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"imgbed/internal/config"
	"imgbed/internal/service"
	"imgbed/internal/storage"
)

// Handler 聚合所有 HTTP 处理器和依赖。
type Handler struct {
	cfg       *config.Config
	imageSvc  *service.ImageService
	albumRepo *service.AlbumRepository
	store     storage.Store
}

// New 创建 Handler。imageSvc 由调用方创建并负责关闭（其内部有后台计数协程）。
func New(cfg *config.Config, db *sql.DB, store storage.Store, imageSvc *service.ImageService) *Handler {
	return &Handler{
		cfg:       cfg,
		imageSvc:  imageSvc,
		albumRepo: service.NewAlbumRepository(db),
		store:     store,
	}
}

// Config 返回前端需要的服务端配置（上传限制、存储后端），避免前端硬编码漂移。
func (h *Handler) Config(c *gin.Context) {
	backend := strings.ToLower(strings.TrimSpace(h.cfg.Storage.Backend))
	if backend == "" {
		backend = "local"
	}
	c.JSON(200, gin.H{
		"max_size_mb":     h.cfg.Limits.MaxSizeMB,
		"allowed_types":   h.cfg.Limits.AllowedTypes,
		"storage_backend": backend,
	})
}

// ImageList 返回图片列表。album=0 表示未分类，album=N 表示指定相册。
func (h *Handler) ImageList(c *gin.Context) {
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	q := strings.TrimSpace(c.Query("q"))
	var albumID *int64
	unassigned := false
	if raw := c.Query("album"); raw != "" {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
			if id == 0 {
				unassigned = true
			} else {
				albumID = &id
			}
		}
	}
	res, err := h.imageSvc.List(c.Request.Context(), page, albumID, unassigned, q)
	if err != nil {
		c.JSON(500, gin.H{"error": "查询失败", "message": err.Error()})
		return
	}
	c.JSON(200, res)
}

// ImageDetail 返回图片详情。
func (h *Handler) ImageDetail(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的图片 ID"})
		return
	}
	img, err := h.imageSvc.Get(c.Request.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(404, gin.H{"error": "图片不存在"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"error": "查询失败", "message": err.Error()})
		return
	}
	c.JSON(200, img)
}

// ImageDelete 删除图片。
func (h *Handler) ImageDelete(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的图片 ID"})
		return
	}
	if err := h.imageSvc.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(404, gin.H{"error": "图片不存在"})
			return
		}
		c.JSON(500, gin.H{"error": "删除失败", "message": err.Error()})
		return
	}
	c.JSON(200, gin.H{"message": "已删除"})
}

// ImageMove 修改图片所属相册。album 为空或 0 表示移出相册。
func (h *Handler) ImageMove(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的图片 ID"})
		return
	}
	var body struct {
		Album string `json:"album"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "参数错误", "message": err.Error()})
		return
	}
	var albumID *int64
	if raw := strings.TrimSpace(body.Album); raw != "" && raw != "0" {
		aid, aerr := resolveAlbumID(c.Request.Context(), h.albumRepo, raw)
		if aerr != nil {
			c.JSON(400, gin.H{"error": "相册不存在"})
			return
		}
		albumID = &aid
	}
	img, err := h.imageSvc.MoveToAlbum(c.Request.Context(), id, albumID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(404, gin.H{"error": "图片不存在"})
			return
		}
		c.JSON(500, gin.H{"error": "移动失败", "message": err.Error()})
		return
	}
	c.JSON(200, img)
}

// ImageAlias 设置或清除图片自定义别名。
func (h *Handler) ImageAlias(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的图片 ID"})
		return
	}
	var body struct {
		Alias string `json:"alias"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "参数错误", "message": err.Error()})
		return
	}
	img, err := h.imageSvc.SetAlias(c.Request.Context(), id, body.Alias)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(404, gin.H{"error": "图片不存在"})
			return
		}
		c.JSON(400, gin.H{"error": "设置别名失败", "message": err.Error()})
		return
	}
	c.JSON(200, img)
}

// ServeImage 通过 /i/*filepath 输出图片，并记录访问量。
// 公开端点：错误详情只写日志，不回传给访客。
func (h *Handler) ServeImage(c *gin.Context) {
	filename, ok := imagePathParam(c)
	if !ok {
		return
	}

	img, err := h.imageSvc.RecordView(c.Request.Context(), filename)
	if errors.Is(err, sql.ErrNoRows) {
		c.AbortWithStatus(404)
		return
	}
	if err != nil {
		log.Printf("查询图片 %s 失败: %v", filename, err)
		c.JSON(500, gin.H{"error": "读取失败"})
		return
	}

	h.writeStoredImage(c, filename, img.MIME)
}

// ServeAlias 通过 /s/:alias 输出图片，并记录访问量。
func (h *Handler) ServeAlias(c *gin.Context) {
	alias := strings.TrimSpace(c.Param("alias"))
	if alias == "" {
		c.AbortWithStatus(404)
		return
	}
	img, err := h.imageSvc.RecordAliasView(c.Request.Context(), alias)
	if errors.Is(err, sql.ErrNoRows) {
		c.AbortWithStatus(404)
		return
	}
	if err != nil {
		log.Printf("查询别名 %s 失败: %v", alias, err)
		c.JSON(500, gin.H{"error": "读取失败"})
		return
	}
	h.writeStoredImage(c, img.Filename, img.MIME)
}

// PreviewImage 输出图片但不记录访问量，用于后台缩略图和详情预览。
func (h *Handler) PreviewImage(c *gin.Context) {
	filename, ok := imagePathParam(c)
	if !ok {
		return
	}

	img, err := h.imageSvc.GetByFilename(c.Request.Context(), filename)
	if errors.Is(err, sql.ErrNoRows) {
		c.AbortWithStatus(404)
		return
	}
	if err != nil {
		log.Printf("查询图片 %s 失败: %v", filename, err)
		c.JSON(500, gin.H{"error": "读取失败"})
		return
	}

	h.writeStoredImage(c, filename, img.MIME)
}

// AlbumList 返回相册列表。
func (h *Handler) AlbumList(c *gin.Context) {
	albums, err := h.albumRepo.List(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": "查询失败", "message": err.Error()})
		return
	}
	c.JSON(200, albums)
}

// AlbumCreate 创建相册。
func (h *Handler) AlbumCreate(c *gin.Context) {
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "参数错误", "message": err.Error()})
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		c.JSON(400, gin.H{"error": "相册名称不能为空"})
		return
	}
	album, err := h.albumRepo.Create(c.Request.Context(), body.Name, body.Description)
	if err != nil {
		c.JSON(500, gin.H{"error": "创建失败", "message": err.Error()})
		return
	}
	c.JSON(200, album)
}

// AlbumDelete 删除相册。
func (h *Handler) AlbumDelete(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的相册 ID"})
		return
	}
	if err := h.albumRepo.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(404, gin.H{"error": "相册不存在"})
			return
		}
		c.JSON(500, gin.H{"error": "删除失败", "message": err.Error()})
		return
	}
	c.JSON(200, gin.H{"message": "已删除"})
}

// AlbumUpdate 修改相册名称和描述。
func (h *Handler) AlbumUpdate(c *gin.Context) {
	id, err := parseID(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的相册 ID"})
		return
	}
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "参数错误", "message": err.Error()})
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		c.JSON(400, gin.H{"error": "相册名称不能为空"})
		return
	}
	album, err := h.albumRepo.Update(c.Request.Context(), id, body.Name, body.Description)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(404, gin.H{"error": "相册不存在"})
			return
		}
		c.JSON(500, gin.H{"error": "修改失败", "message": err.Error()})
		return
	}
	c.JSON(200, album)
}

// Stats 返回全局统计。
func (h *Handler) Stats(c *gin.Context) {
	stats, err := h.imageSvc.Stats(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": "统计失败", "message": err.Error()})
		return
	}
	c.JSON(200, stats)
}

func parseID(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

func resolveAlbumID(ctx context.Context, repo *service.AlbumRepository, raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, sql.ErrNoRows
	}
	if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if _, err := repo.GetByID(ctx, id); err == nil {
			return id, nil
		}
	}
	albums, err := repo.List(ctx)
	if err != nil {
		return 0, err
	}
	for _, a := range albums {
		if a.Name == raw {
			return a.ID, nil
		}
	}
	return 0, sql.ErrNoRows
}

func imagePathParam(c *gin.Context) (string, bool) {
	filename := c.Param("filepath")
	filename = strings.TrimPrefix(filename, "/")
	if strings.Contains(filename, "..") || filename == "" {
		c.AbortWithStatus(404)
		return "", false
	}
	return filename, true
}

func (h *Handler) writeStoredImage(c *gin.Context, filename, mime string) {
	rc, err := h.store.Open(c.Request.Context(), filename)
	if err != nil {
		// 数据库有记录但物理文件缺失：本地存储返回 404，其余记日志返回通用 500。
		if errors.Is(err, fs.ErrNotExist) {
			log.Printf("图片文件缺失: %s", filename)
			c.AbortWithStatus(404)
			return
		}
		log.Printf("打开图片 %s 失败: %v", filename, err)
		c.JSON(500, gin.H{"error": "读取失败"})
		return
	}
	defer rc.Close()

	c.Header("Cache-Control", "public, max-age=31536000")
	c.DataFromReader(200, -1, mime, rc, nil)
}
