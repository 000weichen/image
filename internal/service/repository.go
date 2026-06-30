package service

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"strings"

	"imgbed/internal/model"
)

// 本文件集中所有对 SQLite 的直接访问。实际项目中若变大可拆成 repository 包。

var dbInstance *sql.DB

// SetDB 在 main 中注入全局 DB 句柄。
func SetDB(db *sql.DB) {
	dbInstance = db
}

// insertImage 将图片记录插入数据库；若 hash 冲突则返回已有记录。
func insertImage(ctx context.Context, img *model.Image) (*model.Image, error) {
	// 先检查 hash 是否已存在
	existing, err := getImageByHash(ctx, img.Hash)
	if err == nil && existing != nil {
		return existing, nil
	}

	var albumID sql.NullInt64
	if img.AlbumID != nil {
		albumID.Int64 = *img.AlbumID
		albumID.Valid = true
	}

	res, err := dbInstance.ExecContext(ctx,
		`INSERT INTO images (hash, original_name, filename, size, mime, width, height, alias, album_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		img.Hash, img.OriginalName, img.Filename, img.Size, img.MIME, img.Width, img.Height, nullString(img.Alias), albumID,
	)
	if err != nil {
		if existing, lookupErr := getImageByHash(ctx, img.Hash); lookupErr == nil && existing != nil {
			return existing, nil
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	img.ID = id
	return img, nil
}

func getImageByHash(ctx context.Context, hash string) (*model.Image, error) {
	row := dbInstance.QueryRowContext(ctx,
		`SELECT id, hash, original_name, filename, size, mime, width, height, alias, album_id, views, last_accessed_at, created_at
		 FROM images WHERE hash = ?`, hash)
	return scanImage(row)
}

func getImageByID(ctx context.Context, id int64) (*model.Image, error) {
	row := dbInstance.QueryRowContext(ctx,
		`SELECT id, hash, original_name, filename, size, mime, width, height, alias, album_id, views, last_accessed_at, created_at
		 FROM images WHERE id = ?`, id)
	return scanImage(row)
}

func getImageByFilename(ctx context.Context, filename string) (*model.Image, error) {
	row := dbInstance.QueryRowContext(ctx,
		`SELECT id, hash, original_name, filename, size, mime, width, height, alias, album_id, views, last_accessed_at, created_at
		 FROM images WHERE filename = ?`, filename)
	return scanImage(row)
}

func getImageByAlias(ctx context.Context, alias string) (*model.Image, error) {
	row := dbInstance.QueryRowContext(ctx,
		`SELECT id, hash, original_name, filename, size, mime, width, height, alias, album_id, views, last_accessed_at, created_at
		 FROM images WHERE alias = ?`, alias)
	return scanImage(row)
}

func scanImage(row *sql.Row) (*model.Image, error) {
	var img model.Image
	var width, height sql.NullInt32
	var alias sql.NullString
	var albumID sql.NullInt64
	var last sql.NullString
	err := row.Scan(&img.ID, &img.Hash, &img.OriginalName, &img.Filename, &img.Size, &img.MIME,
		&width, &height, &alias, &albumID, &img.Views, &last, &img.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, err
	}
	if width.Valid {
		img.Width = int(width.Int32)
	}
	if height.Valid {
		img.Height = int(height.Int32)
	}
	if alias.Valid {
		img.Alias = alias.String
	}
	if albumID.Valid {
		img.AlbumID = &albumID.Int64
	}
	if last.Valid {
		img.LastAccessedAt = last.String
	}
	return &img, nil
}

func incrementViews(ctx context.Context, id int64) error {
	_, err := dbInstance.ExecContext(ctx,
		`UPDATE images SET views = views + 1, last_accessed_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	return err
}

func deleteImageByID(ctx context.Context, id int64) error {
	res, err := dbInstance.ExecContext(ctx, `DELETE FROM images WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func updateImageAlbum(ctx context.Context, id int64, albumID *int64) error {
	var val any
	if albumID != nil {
		val = *albumID
	}
	res, err := dbInstance.ExecContext(ctx, `UPDATE images SET album_id = ? WHERE id = ?`, val, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, getErr := getImageByID(ctx, id); getErr != nil {
			return sql.ErrNoRows
		}
	}
	return nil
}

func countImages(ctx context.Context, albumID *int64, unassigned bool, q string) (int64, error) {
	var args []any
	where := []string{"1=1"}
	if unassigned {
		where = append(where, "album_id IS NULL")
	} else if albumID != nil {
		where = append(where, "album_id = ?")
		args = append(args, *albumID)
	}
	if strings.TrimSpace(q) != "" {
		where = append(where, "(original_name LIKE ? OR hash LIKE ? OR alias LIKE ?)")
		args = append(args, "%"+q+"%", "%"+q+"%", "%"+q+"%")
	}
	var total int64
	err := dbInstance.QueryRowContext(ctx, "SELECT COUNT(*) FROM images WHERE "+strings.Join(where, " AND "), args...).Scan(&total)
	return total, err
}

func listImages(ctx context.Context, page, pageSize int, albumID *int64, unassigned bool, q string) ([]model.Image, error) {
	var args []any
	where := []string{"1=1"}
	if unassigned {
		where = append(where, "album_id IS NULL")
	} else if albumID != nil {
		where = append(where, "album_id = ?")
		args = append(args, *albumID)
	}
	if strings.TrimSpace(q) != "" {
		where = append(where, "(original_name LIKE ? OR hash LIKE ? OR alias LIKE ?)")
		args = append(args, "%"+q+"%", "%"+q+"%", "%"+q+"%")
	}
	offset := (page - 1) * pageSize
	args = append(args, pageSize, offset)

	rows, err := dbInstance.QueryContext(ctx,
		"SELECT id, hash, original_name, filename, size, mime, width, height, alias, album_id, views, last_accessed_at, created_at FROM images WHERE "+
			strings.Join(where, " AND ")+" ORDER BY created_at DESC LIMIT ? OFFSET ?", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	images := make([]model.Image, 0)
	for rows.Next() {
		var img model.Image
		var width, height sql.NullInt32
		var alias sql.NullString
		var aid sql.NullInt64
		var last sql.NullString
		if err := rows.Scan(&img.ID, &img.Hash, &img.OriginalName, &img.Filename, &img.Size, &img.MIME,
			&width, &height, &alias, &aid, &img.Views, &last, &img.CreatedAt); err != nil {
			return nil, err
		}
		if width.Valid {
			img.Width = int(width.Int32)
		}
		if height.Valid {
			img.Height = int(height.Int32)
		}
		if alias.Valid {
			img.Alias = alias.String
		}
		if aid.Valid {
			img.AlbumID = &aid.Int64
		}
		if last.Valid {
			img.LastAccessedAt = last.String
		}
		images = append(images, img)
	}
	return images, rows.Err()
}

func listImagesMissingDimensions(ctx context.Context) ([]model.Image, error) {
	rows, err := dbInstance.QueryContext(ctx,
		`SELECT id, filename FROM images WHERE COALESCE(width, 0) = 0 OR COALESCE(height, 0) = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	images := make([]model.Image, 0)
	for rows.Next() {
		var img model.Image
		if err := rows.Scan(&img.ID, &img.Filename); err != nil {
			return nil, err
		}
		images = append(images, img)
	}
	return images, rows.Err()
}

func updateImageDimensions(ctx context.Context, id int64, width, height int) error {
	_, err := dbInstance.ExecContext(ctx, `UPDATE images SET width = ?, height = ? WHERE id = ?`, width, height, id)
	return err
}

func updateImageAlias(ctx context.Context, id int64, alias string) error {
	res, err := dbInstance.ExecContext(ctx, `UPDATE images SET alias = ? WHERE id = ?`, nullString(alias), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func nullString(s string) sql.NullString {
	s = strings.TrimSpace(s)
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// AlbumRepository 专辑相关数据访问。
type AlbumRepository struct{}

func (r *AlbumRepository) Create(ctx context.Context, name, desc string) (*model.Album, error) {
	res, err := dbInstance.ExecContext(ctx,
		`INSERT INTO albums (name, description) VALUES (?, ?)`, name, desc)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return r.GetByID(ctx, id)
}

func (r *AlbumRepository) GetByID(ctx context.Context, id int64) (*model.Album, error) {
	var a model.Album
	var desc sql.NullString
	err := dbInstance.QueryRowContext(ctx,
		`SELECT id, name, description, created_at FROM albums WHERE id = ?`, id).Scan(&a.ID, &a.Name, &desc, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	if desc.Valid {
		a.Description = desc.String
	}
	return &a, nil
}

func (r *AlbumRepository) Delete(ctx context.Context, id int64) error {
	res, err := dbInstance.ExecContext(ctx, `DELETE FROM albums WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *AlbumRepository) Update(ctx context.Context, id int64, name, desc string) (*model.Album, error) {
	res, err := dbInstance.ExecContext(ctx, `UPDATE albums SET name = ?, description = ? WHERE id = ?`, name, desc, id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, sql.ErrNoRows
	}
	return r.GetByID(ctx, id)
}

func (r *AlbumRepository) List(ctx context.Context) ([]model.Album, error) {
	rows, err := dbInstance.QueryContext(ctx,
		`SELECT a.id, a.name, a.description, a.created_at, COUNT(i.id)
		 FROM albums a
		 LEFT JOIN images i ON i.album_id = a.id
		 GROUP BY a.id
		 ORDER BY a.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	albums := make([]model.Album, 0)
	for rows.Next() {
		var a model.Album
		var desc sql.NullString
		if err := rows.Scan(&a.ID, &a.Name, &desc, &a.CreatedAt, &a.ImageCount); err != nil {
			return nil, err
		}
		if desc.Valid {
			a.Description = desc.String
		}
		albums = append(albums, a)
	}
	return albums, rows.Err()
}

func (r *AlbumRepository) CountImages(ctx context.Context, id int64) (int64, error) {
	var n int64
	err := dbInstance.QueryRowContext(ctx, `SELECT COUNT(*) FROM images WHERE album_id = ?`, id).Scan(&n)
	return n, err
}

func Stats(ctx context.Context) (*model.StatsResponse, error) {
	var s model.StatsResponse
	row := dbInstance.QueryRowContext(ctx,
		`SELECT COALESCE(COUNT(*),0), COALESCE(SUM(size),0), COALESCE(SUM(views),0) FROM images`)
	if err := row.Scan(&s.TotalImages, &s.TotalSize, &s.TotalViews); err != nil {
		return nil, err
	}

	rows, err := dbInstance.QueryContext(ctx,
		`SELECT a.id, a.name, COUNT(i.id) as cnt
		 FROM albums a
		 LEFT JOIN images i ON i.album_id = a.id
		 GROUP BY a.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	s.Albums = make([]model.AlbumCount, 0)
	for rows.Next() {
		var ac model.AlbumCount
		if err := rows.Scan(&ac.ID, &ac.Name, &ac.ImageCount); err != nil {
			return nil, err
		}
		s.Albums = append(s.Albums, ac)
	}

	var unassigned int64
	if err := dbInstance.QueryRowContext(ctx, `SELECT COUNT(*) FROM images WHERE album_id IS NULL`).Scan(&unassigned); err != nil {
		return nil, err
	}
	s.Unassigned = unassigned
	return &s, nil
}

// Helper for service tests (currently unused but useful).
func readerToBytes(r io.Reader) ([]byte, error) {
	var buf bytes.Buffer
	_, err := io.Copy(&buf, r)
	return buf.Bytes(), err
}
