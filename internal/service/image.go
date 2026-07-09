package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"
	"unicode"

	// 注册标准库未默认启用的 webp/bmp 解码器。
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"

	"imgbed/internal/config"
	"imgbed/internal/model"
	"imgbed/internal/storage"
)

const defaultPageSize = 20

// ImageService 封装图片相关的业务逻辑。
type ImageService struct {
	cfg   *config.Config
	store storage.Store
	repo  *Repository
	views *viewCounter
}

// NewImageService 创建图片服务，并启动访问计数的后台刷写协程；
// 进程退出前调用 Close 落盘剩余计数。
func NewImageService(cfg *config.Config, store storage.Store, repo *Repository) *ImageService {
	return &ImageService{cfg: cfg, store: store, repo: repo, views: newViewCounter(repo)}
}

// Close 停止后台协程并把未落库的访问计数写回数据库。
func (s *ImageService) Close() {
	s.views.Close()
}

// UploadRequest 表示一次上传所需参数。
type UploadRequest struct {
	Filename string
	Data     []byte
	AlbumID  *int64
}

var mimeToExt = map[string]string{
	"image/jpeg": "jpg",
	"image/png":  "png",
	"image/gif":  "gif",
	"image/webp": "webp",
	"image/bmp":  "bmp",
}

// Save 处理图片上传：校验、去重、保存、入库。
func (s *ImageService) Save(ctx context.Context, req *UploadRequest) (*model.Image, error) {
	if len(req.Data) == 0 {
		return nil, fmt.Errorf("文件为空")
	}
	if int64(len(req.Data)) > s.cfg.MaxBytes() {
		return nil, fmt.Errorf("文件超过大小限制 %d MB", s.cfg.Limits.MaxSizeMB)
	}

	// 使用 http.DetectContentType 做初步类型识别，再校验白名单。
	mime := detectMIME(req.Data)
	allowed := s.cfg.Allowed()
	if !allowed[mime] {
		return nil, fmt.Errorf("不支持的图片类型: %s", mime)
	}

	// 取尺寸；若失败只记录 0,0，不影响保存。
	width, height := dimensions(req.Data)

	// 先查 hash 是否已存在（去重）。
	hash := sha256.Sum256(req.Data)
	sha := hex.EncodeToString(hash[:])
	if existing, err := s.repo.getImageByHash(ctx, sha); err == nil && existing != nil {
		return s.applyUploadAlbum(ctx, existing, req.AlbumID)
	}

	// hash 不存在，保存物理文件。
	ext, ok := mimeToExt[mime]
	if !ok {
		ext = "bin"
	}
	filename := fmt.Sprintf("%s/%s.%s", sha[:2], sha, ext)
	if err := s.store.Save(ctx, filename, bytes.NewReader(req.Data), mime); err != nil {
		return nil, fmt.Errorf("保存图片: %w", err)
	}

	// 入库。
	img := &model.Image{
		Hash:         sha,
		OriginalName: req.Filename,
		Filename:     filename,
		Size:         int64(len(req.Data)),
		MIME:         mime,
		Width:        width,
		Height:       height,
		AlbumID:      req.AlbumID,
	}
	inserted, err := s.repo.insertImage(ctx, img)
	if err != nil {
		// 数据库写入失败时回滚存储，避免脏数据。
		_ = s.store.Delete(ctx, filename)
		return nil, fmt.Errorf("入库失败: %w", err)
	}
	return s.applyUploadAlbum(ctx, inserted, req.AlbumID)
}

// Delete 删除图片及其物理文件。
func (s *ImageService) Delete(ctx context.Context, id int64) error {
	img, err := s.repo.getImageByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.deleteImageByID(ctx, id); err != nil {
		return err
	}
	// 数据库删除成功后，再删除物理对象，即使失败也视为已删除记录。
	_ = s.store.Delete(ctx, img.Filename)
	return nil
}

// Get 获取图片详情。
func (s *ImageService) Get(ctx context.Context, id int64) (*model.Image, error) {
	img, err := s.repo.getImageByID(ctx, id)
	if err != nil {
		return nil, err
	}
	s.fillURLs(img)
	return img, nil
}

// MoveToAlbum 将图片移动到指定相册。albumID 为 nil 时移出相册（设为未分类）。
func (s *ImageService) MoveToAlbum(ctx context.Context, id int64, albumID *int64) (*model.Image, error) {
	if err := s.repo.updateImageAlbum(ctx, id, albumID); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// RecordView 记录一次访问并返回图片记录。
// 计数只写入内存缓冲，由后台协程批量落库，请求路径上没有数据库写操作。
func (s *ImageService) RecordView(ctx context.Context, filename string) (*model.Image, error) {
	img, err := s.repo.getImageByFilename(ctx, filename)
	if err != nil {
		return nil, err
	}
	s.views.add(img.ID)
	s.fillURLs(img)
	return img, nil
}

// RecordAliasView 通过自定义别名访问图片，并记录访问量。
func (s *ImageService) RecordAliasView(ctx context.Context, alias string) (*model.Image, error) {
	img, err := s.repo.getImageByAlias(ctx, alias)
	if err != nil {
		return nil, err
	}
	s.views.add(img.ID)
	s.fillURLs(img)
	return img, nil
}

// GetByFilename 获取图片记录但不增加访问计数，用于后台预览和缩略图。
func (s *ImageService) GetByFilename(ctx context.Context, filename string) (*model.Image, error) {
	img, err := s.repo.getImageByFilename(ctx, filename)
	if err != nil {
		return nil, err
	}
	s.fillURLs(img)
	return img, nil
}

// List 分页列出图片。unassigned 为 true 时仅返回未分类图片。
func (s *ImageService) List(ctx context.Context, page int, albumID *int64, unassigned bool, q string) (*model.ListResponse, error) {
	if page < 1 {
		page = 1
	}
	total, err := s.repo.countImages(ctx, albumID, unassigned, q)
	if err != nil {
		return nil, err
	}
	images, err := s.repo.listImages(ctx, page, defaultPageSize, albumID, unassigned, q)
	if err != nil {
		return nil, err
	}
	for i := range images {
		s.fillURLs(&images[i])
	}
	return &model.ListResponse{
		Images:   images,
		Total:    total,
		Page:     page,
		PageSize: defaultPageSize,
	}, nil
}

// BackfillDimensions 回填旧图片记录缺失的宽高信息。
func BackfillDimensions(ctx context.Context, repo *Repository, store storage.Store) (int, error) {
	images, err := repo.listImagesMissingDimensions(ctx)
	if err != nil {
		return 0, err
	}

	var firstErr error
	updated := 0
	for _, img := range images {
		rc, err := store.Open(ctx, img.Filename)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("打开 %s: %w", img.Filename, err)
			}
			continue
		}

		var buf bytes.Buffer
		if _, err := io.Copy(&buf, rc); err != nil {
			rc.Close()
			if firstErr == nil {
				firstErr = fmt.Errorf("读取 %s: %w", img.Filename, err)
			}
			continue
		}
		data := buf.Bytes()
		if closeErr := rc.Close(); closeErr != nil && firstErr == nil {
			firstErr = fmt.Errorf("关闭 %s: %w", img.Filename, closeErr)
		}

		width, height := dimensions(data)
		if width == 0 || height == 0 {
			continue
		}
		if err := repo.updateImageDimensions(ctx, img.ID, width, height); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("更新 %s: %w", img.Filename, err)
			}
			continue
		}
		updated++
	}
	return updated, firstErr
}

func (s *ImageService) url(filename string) string {
	if strings.EqualFold(s.cfg.Storage.Backend, "s3") {
		if publicURL := strings.TrimSpace(s.cfg.Storage.S3.PublicURL); publicURL != "" {
			return fmt.Sprintf("%s/%s", strings.TrimRight(publicURL, "/"), filename)
		}
	}
	base := strings.TrimRight(s.cfg.Server.BaseURL, "/")
	return fmt.Sprintf("%s%s/%s", base, s.cfg.ImageURLPath(), filename)
}

func (s *ImageService) aliasURL(alias string) string {
	if alias == "" {
		return ""
	}
	base := strings.TrimRight(s.cfg.Server.BaseURL, "/")
	return fmt.Sprintf("%s/s/%s", base, alias)
}

func (s *ImageService) fillURLs(img *model.Image) {
	img.URL = s.url(img.Filename)
	img.AliasURL = s.aliasURL(img.Alias)
}

// Stats 返回全局统计，并为热门图片补全公开访问 URL。
func (s *ImageService) Stats(ctx context.Context) (*model.StatsResponse, error) {
	stats, err := s.repo.Stats(ctx)
	if err != nil {
		return nil, err
	}
	for i := range stats.PopularImages {
		stats.PopularImages[i].URL = s.url(stats.PopularImages[i].Filename)
	}
	return stats, nil
}

// SetAlias 设置或清除图片自定义别名。
func (s *ImageService) SetAlias(ctx context.Context, id int64, raw string) (*model.Image, error) {
	alias, err := normalizeAlias(raw)
	if err != nil {
		return nil, err
	}
	if alias != "" {
		existing, err := s.repo.getImageByAlias(ctx, alias)
		if err == nil && existing.ID != id {
			return nil, fmt.Errorf("别名已被使用")
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	if err := s.repo.updateImageAlias(ctx, id, alias); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

func (s *ImageService) applyUploadAlbum(ctx context.Context, img *model.Image, albumID *int64) (*model.Image, error) {
	if albumID != nil {
		if err := s.repo.updateImageAlbum(ctx, img.ID, albumID); err != nil {
			return nil, err
		}
		var err error
		img, err = s.repo.getImageByID(ctx, img.ID)
		if err != nil {
			return nil, err
		}
	}
	s.fillURLs(img)
	return img, nil
}

func normalizeAlias(raw string) (string, error) {
	alias := strings.TrimSpace(raw)
	alias = strings.TrimPrefix(alias, "/")
	alias = strings.TrimPrefix(alias, "s/")
	alias = strings.TrimPrefix(alias, "/s/")
	if alias == "" {
		return "", nil
	}
	if len(alias) > 120 {
		return "", fmt.Errorf("别名不能超过 120 个字符")
	}
	if strings.Contains(alias, "..") || strings.ContainsAny(alias, `/\?#%`) {
		return "", fmt.Errorf("别名只能包含字母、数字、点、短横线和下划线")
	}
	for _, r := range alias {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_' {
			continue
		}
		return "", fmt.Errorf("别名只能包含字母、数字、点、短横线和下划线")
	}
	return alias, nil
}

// detectMIME 使用标准库探测 MIME 类型。
func detectMIME(data []byte) string {
	return http.DetectContentType(data)
}

// dimensions 返回图片宽高。
func dimensions(data []byte) (int, int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}
