package storage

import (
	"context"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"imgbed/internal/config"
)

// S3Store 通过 minio-go 访问任意 S3 兼容对象存储
// （MinIO / AWS S3 / 阿里云 OSS / 腾讯云 COS / 七牛云 等）。
type S3Store struct {
	client *minio.Client
	bucket string
}

func NewS3(cfg config.S3Config) (*S3Store, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" {
		return nil, fmt.Errorf("S3 配置不完整：endpoint 与 bucket 不能为空")
	}
	cli, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("初始化 S3 客户端: %w", err)
	}
	return &S3Store{client: cli, bucket: cfg.Bucket}, nil
}

func (s *S3Store) Save(ctx context.Context, key string, r io.Reader, mime string) error {
	// size=-1 表示未知大小，minio 会以分段上传方式处理任意长度流。
	_, err := s.client.PutObject(ctx, s.bucket, key, r, -1, minio.PutObjectOptions{
		ContentType: mime,
	})
	if err != nil {
		return fmt.Errorf("S3 PutObject: %w", err)
	}
	return nil
}

func (s *S3Store) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("S3 GetObject: %w", err)
	}
	// S3 的错误是惰性的，调用 Stat 触发一次以早暴露不存在的情形。
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		return nil, fmt.Errorf("S3 Stat: %w", err)
	}
	return obj, nil
}

func (s *S3Store) Delete(ctx context.Context, key string) error {
	err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("S3 RemoveObject: %w", err)
	}
	return nil
}

func (s *S3Store) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err == nil {
		return true, nil
	}
	resp := minio.ToErrorResponse(err)
	if resp.Code == "NoSuchKey" {
		return false, nil
	}
	return false, fmt.Errorf("S3 Stat: %w", err)
}
