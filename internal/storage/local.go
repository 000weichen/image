package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"imgbed/internal/config"
)

// LocalStore 把图片写入本地磁盘目录。
type LocalStore struct {
	dir string
}

func NewLocal(cfg config.LocalConfig) (*LocalStore, error) {
	dir := cfg.Dir
	if dir == "" {
		dir = "./uploads"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建本地存储目录: %w", err)
	}
	return &LocalStore{dir: dir}, nil
}

// path 将存储 key 转为安全的本地路径，拒绝包含 .. 的越权访问。
func (s *LocalStore) path(key string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(key))
	if strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, string(filepath.Separator)+"..") {
		return "", fmt.Errorf("非法存储 key: %q", key)
	}
	return filepath.Join(s.dir, cleaned), nil
}

func (s *LocalStore) Save(ctx context.Context, key string, r io.Reader, mime string) error {
	p, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("创建子目录: %w", err)
	}
	f, err := os.Create(p)
	if err != nil {
		return fmt.Errorf("创建文件: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("写入文件: %w", err)
	}
	return nil
}

func (s *LocalStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	p, err := s.path(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (s *LocalStore) Delete(ctx context.Context, key string) error {
	p, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
