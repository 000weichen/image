package service

import (
	"context"
	"database/sql"
	"strconv"
	"strings"

	"imgbed/internal/model"
)

// AlbumService 封装相册业务。
type AlbumService struct{}

func NewAlbumService() *AlbumService {
	return &AlbumService{}
}

func (s *AlbumService) Create(ctx context.Context, name, desc string) (*model.Album, error) {
	repo := &AlbumRepository{}
	return repo.Create(ctx, name, desc)
}

func (s *AlbumService) Delete(ctx context.Context, id int64) error {
	repo := &AlbumRepository{}
	return repo.Delete(ctx, id)
}

func (s *AlbumService) Update(ctx context.Context, id int64, name, desc string) (*model.Album, error) {
	repo := &AlbumRepository{}
	return repo.Update(ctx, id, name, desc)
}

func (s *AlbumService) List(ctx context.Context) ([]model.Album, error) {
	repo := &AlbumRepository{}
	return repo.List(ctx)
}

// ResolveID 把相册名称或数字字符串解析成相册 ID。不存在返回错误。
func (s *AlbumService) ResolveID(ctx context.Context, raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, sql.ErrNoRows
	}
	// 先按数字 id 处理
	if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
		repo := &AlbumRepository{}
		if _, err := repo.GetByID(ctx, id); err == nil {
			return id, nil
		}
	}
	// 再按名称匹配
	albums, err := s.List(ctx)
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
