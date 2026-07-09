package storage

import (
	"context"
	"fmt"
	"io"

	"imgbed/internal/config"
)

// Store 抽象图片的物理存储。本地磁盘与 S3 兼容存储实现同一接口，
// 业务层无需关心后端细节。
type Store interface {
	// Save 将 reader 的内容写入 key，mime 作为对象内容类型（S3 模式下生效）。
	Save(ctx context.Context, key string, r io.Reader, mime string) error
	// Open 打开 key 对应对象供读取，调用方负责 Close。
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete 删除 key 对应对象，对象不存在视为成功。
	Delete(ctx context.Context, key string) error
}

// New 根据配置返回对应后端。backend 为 "s3" 时走 S3，其余均走本地。
func New(cfg config.StorageConfig) (Store, error) {
	switch cfg.Backend {
	case "s3":
		return NewS3(cfg.S3)
	case "local", "":
		return NewLocal(cfg.Local)
	default:
		return nil, fmt.Errorf("未知 storage.backend: %q", cfg.Backend)
	}
}
