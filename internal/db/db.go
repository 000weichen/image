package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// schema 是建表语句；幂等，重复执行不会破坏已有数据。
const schema = `
CREATE TABLE IF NOT EXISTS albums (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT UNIQUE NOT NULL,
  description TEXT,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS images (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  hash TEXT UNIQUE NOT NULL,
  original_name TEXT NOT NULL,
  filename TEXT NOT NULL,
  size INTEGER NOT NULL,
  mime TEXT NOT NULL,
  width INTEGER,
  height INTEGER,
  alias TEXT,
  album_id INTEGER,
  views INTEGER DEFAULT 0,
  last_accessed_at DATETIME,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (album_id) REFERENCES albums(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_images_album_id ON images(album_id);
CREATE INDEX IF NOT EXISTS idx_images_created_at ON images(created_at);
CREATE INDEX IF NOT EXISTS idx_images_filename ON images(filename);
`

// Open 打开（不存在则创建）SQLite 数据库文件，启用 WAL 与外键，并建表。
func Open(path string) (*sql.DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("解析数据库路径: %w", err)
	}
	// SQLite 只创建文件不创建目录，这里兜底保证父目录存在。
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return nil, fmt.Errorf("创建数据库目录: %w", err)
	}
	// journal_mode=WAL 提升并发；foreign_keys=on 强制外键级联；busy_timeout 规避写锁争用。
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)&_pragma=busy_timeout(5000)", abs)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库: %w", err)
	}
	// SQLite 单写多读，适度提升连接数以支持并发读取。
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(time.Hour)

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("建表: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	hasAlias, err := columnExists(db, "images", "alias")
	if err != nil {
		return fmt.Errorf("检查 alias 列: %w", err)
	}
	if !hasAlias {
		if _, err := db.Exec(`ALTER TABLE images ADD COLUMN alias TEXT`); err != nil {
			return fmt.Errorf("新增 alias 列: %w", err)
		}
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_images_alias ON images(alias) WHERE alias IS NOT NULL AND alias != ''`); err != nil {
		return fmt.Errorf("创建 alias 索引: %w", err)
	}
	return nil
}

func columnExists(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
