package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config 是图床的完整配置，对应 config.yaml。
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Auth     AuthConfig     `yaml:"auth"`
	Storage  StorageConfig  `yaml:"storage"`
	Database DatabaseConfig `yaml:"database"`
	Limits   LimitsConfig   `yaml:"limits"`
}

type ServerConfig struct {
	Port    int    `yaml:"port"`
	BaseURL string `yaml:"base_url"`
}

type AuthConfig struct {
	Token string `yaml:"token"`
}

type StorageConfig struct {
	Backend string      `yaml:"backend"`
	Local   LocalConfig `yaml:"local"`
	S3      S3Config    `yaml:"s3"`
}

type LocalConfig struct {
	Dir     string `yaml:"dir"`
	URLPath string `yaml:"url_path"`
}

type S3Config struct {
	Endpoint  string `yaml:"endpoint"`
	Region    string `yaml:"region"`
	Bucket    string `yaml:"bucket"`
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
	UseSSL    bool   `yaml:"use_ssl"`
	PublicURL string `yaml:"public_url"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type LimitsConfig struct {
	MaxSizeMB    int      `yaml:"max_size_mb"`
	AllowedTypes []string `yaml:"allowed_types"`
}

// defaultConfig 返回内置默认值，等同于 config.example.yaml 的内容。
func defaultConfig() *Config {
	return &Config{
		Server:   ServerConfig{Port: 8080, BaseURL: "http://localhost:8080"},
		Storage:  StorageConfig{Backend: "local", Local: LocalConfig{Dir: "./uploads", URLPath: "/i"}},
		Database: DatabaseConfig{Path: "./data/imgbed.db"},
		Limits:   LimitsConfig{MaxSizeMB: 20, AllowedTypes: []string{"image/jpeg", "image/png", "image/gif", "image/webp", "image/bmp"}},
	}
}

// randomToken 生成 32 字节(64 个十六进制字符)的随机 Token。
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ensureDirs 创建运行期需要的目录。
func ensureDirs(c *Config) error {
	for _, p := range []string{c.Database.Path, c.Storage.Local.Dir} {
		if p == "" {
			continue
		}
		dir := filepath.Dir(p)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建目录 %s: %w", dir, err)
		}
	}
	// 本地存储目录本身
	if c.Storage.Local.Dir != "" {
		if err := os.MkdirAll(c.Storage.Local.Dir, 0o755); err != nil {
			return fmt.Errorf("创建存储目录 %s: %w", c.Storage.Local.Dir, err)
		}
	}
	return nil
}

// Load 读取 config.yaml；不存在时自动生成一份带随机 Token 的副本。
func Load(path string) (*Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg := defaultConfig()
		tok, terr := randomToken()
		if terr != nil {
			return nil, fmt.Errorf("生成 Token: %w", terr)
		}
		cfg.Auth.Token = tok
		if werr := write(path, cfg); werr != nil {
			return nil, fmt.Errorf("写入默认配置 %s: %w", path, werr)
		}
		fmt.Printf("已生成默认配置 %s，管理员 Token: %s\n", path, tok)
		if err := ensureDirs(cfg); err != nil {
			return nil, err
		}
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置 %s: %w", path, err)
	}
	cfg := defaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("解析配置 %s: %w", path, err)
	}

	// 配置缺失关键字段时补全：Token 为空则生成并写回。
	if cfg.Auth.Token == "" {
		tok, terr := randomToken()
		if terr != nil {
			return nil, fmt.Errorf("生成 Token: %w", terr)
		}
		cfg.Auth.Token = tok
		if werr := write(path, cfg); werr != nil {
			return nil, fmt.Errorf("写回配置 %s: %w", path, werr)
		}
		fmt.Printf("已为 %s 生成管理员 Token: %s\n", path, tok)
	}

	if err := ensureDirs(cfg); err != nil {
		return nil, err
	}
	normalizeLimits(cfg)
	return cfg, nil
}

// normalizeLimits 在加载时归一化上传限制，让后续读取无需兜底逻辑。
func normalizeLimits(c *Config) {
	if c.Limits.MaxSizeMB <= 0 {
		c.Limits.MaxSizeMB = 20
	}
	if len(c.Limits.AllowedTypes) == 0 {
		c.Limits.AllowedTypes = defaultConfig().Limits.AllowedTypes
	}
}

func write(path string, cfg *Config) error {
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}

// MaxBytes 返回单文件大小上限（字节）。上限在 Load 时已归一化。
func (c *Config) MaxBytes() int64 {
	return int64(c.Limits.MaxSizeMB) * 1024 * 1024
}

// Allowed 返回允许类型的集合，便于 O(1) 查找。
func (c *Config) Allowed() map[string]bool {
	m := make(map[string]bool, len(c.Limits.AllowedTypes))
	for _, t := range c.Limits.AllowedTypes {
		m[t] = true
	}
	return m
}

// ImageURLPath 返回公开图片访问路径前缀。
func (c *Config) ImageURLPath() string {
	p := strings.TrimSpace(c.Storage.Local.URLPath)
	if p == "" {
		return "/i"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	p = strings.TrimRight(p, "/")
	if p == "" {
		return "/i"
	}
	return p
}
