package service

import (
	"context"
	"database/sql"
	"log"
	"strings"

	"imgbed/internal/model"
)

// 本文件集中所有对 SQLite 的直接访问。实际项目中若变大可拆成 repository 包。

const imageColumns = `id, hash, original_name, filename, size, mime, width, height, alias, album_id, views, last_accessed_at, created_at`

// Repository 封装数据库访问，替代全局变量。
type Repository struct {
	db *sql.DB
}

// NewRepository 创建新的 Repository 实例。
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// insertImage 将图片记录插入数据库并返回完整记录（含 created_at 等库生成字段）；
// 若 hash 唯一约束冲突（并发上传同一文件）则返回已有记录。
func (r *Repository) insertImage(ctx context.Context, img *model.Image) (*model.Image, error) {
	var albumID sql.NullInt64
	if img.AlbumID != nil {
		albumID.Int64 = *img.AlbumID
		albumID.Valid = true
	}

	alias := strings.TrimSpace(img.Alias)
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO images (hash, original_name, filename, size, mime, width, height, alias, album_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		img.Hash, img.OriginalName, img.Filename, img.Size, img.MIME, img.Width, img.Height,
		sql.NullString{String: alias, Valid: alias != ""}, albumID,
	)
	if err != nil {
		if existing, lookupErr := r.getImageByHash(ctx, img.Hash); lookupErr == nil && existing != nil {
			return existing, nil
		}
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return r.getImageByID(ctx, id)
}

func (r *Repository) getImageByHash(ctx context.Context, hash string) (*model.Image, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+imageColumns+` FROM images WHERE hash = ?`, hash)
	return scanImage(row)
}

func (r *Repository) getImageByID(ctx context.Context, id int64) (*model.Image, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+imageColumns+` FROM images WHERE id = ?`, id)
	return scanImage(row)
}

func (r *Repository) getImageByFilename(ctx context.Context, filename string) (*model.Image, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+imageColumns+` FROM images WHERE filename = ?`, filename)
	return scanImage(row)
}

func (r *Repository) getImageByAlias(ctx context.Context, alias string) (*model.Image, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+imageColumns+` FROM images WHERE alias = ?`, alias)
	return scanImage(row)
}

// rowScanner 同时兼容 *sql.Row 与 *sql.Rows。
type rowScanner interface {
	Scan(dest ...any) error
}

func scanImage(row rowScanner) (*model.Image, error) {
	var img model.Image
	var width, height sql.NullInt32
	var alias sql.NullString
	var albumID sql.NullInt64
	var last sql.NullString
	err := row.Scan(&img.ID, &img.Hash, &img.OriginalName, &img.Filename, &img.Size, &img.MIME,
		&width, &height, &alias, &albumID, &img.Views, &last, &img.CreatedAt)
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

// addViewsBatch 在单个事务里把内存聚合的访问计数写回数据库。
// 已删除的图片 id 更新不到行，静默忽略。
func (r *Repository) addViewsBatch(ctx context.Context, counts map[int64]int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx,
		`UPDATE images SET views = views + ?, last_accessed_at = CURRENT_TIMESTAMP WHERE id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for id, n := range counts {
		if _, err := stmt.ExecContext(ctx, n, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repository) deleteImageByID(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM images WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *Repository) updateImageAlbum(ctx context.Context, id int64, albumID *int64) error {
	var val any
	if albumID != nil {
		val = *albumID
	}
	res, err := r.db.ExecContext(ctx, `UPDATE images SET album_id = ? WHERE id = ?`, val, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, getErr := r.getImageByID(ctx, id); getErr != nil {
			return sql.ErrNoRows
		}
	}
	return nil
}

// buildImageFilter 组装列表/计数共用的 WHERE 条件。
// LIKE 转义 %、_ 与 \，用户搜索词按字面匹配。
func buildImageFilter(albumID *int64, unassigned bool, q string) (string, []any) {
	var args []any
	where := []string{"1=1"}
	if unassigned {
		where = append(where, "album_id IS NULL")
	} else if albumID != nil {
		where = append(where, "album_id = ?")
		args = append(args, *albumID)
	}
	if kw := strings.TrimSpace(q); kw != "" {
		kw = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(kw)
		like := "%" + kw + "%"
		where = append(where, `(original_name LIKE ? ESCAPE '\' OR hash LIKE ? ESCAPE '\' OR alias LIKE ? ESCAPE '\')`)
		args = append(args, like, like, like)
	}
	return strings.Join(where, " AND "), args
}

func (r *Repository) countImages(ctx context.Context, albumID *int64, unassigned bool, q string) (int64, error) {
	where, args := buildImageFilter(albumID, unassigned, q)
	var total int64
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM images WHERE "+where, args...).Scan(&total)
	return total, err
}

func (r *Repository) listImages(ctx context.Context, page, pageSize int, albumID *int64, unassigned bool, q string) ([]model.Image, error) {
	where, args := buildImageFilter(albumID, unassigned, q)
	offset := (page - 1) * pageSize
	args = append(args, pageSize, offset)

	rows, err := r.db.QueryContext(ctx,
		"SELECT "+imageColumns+" FROM images WHERE "+where+
			" ORDER BY created_at DESC LIMIT ? OFFSET ?", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	images := make([]model.Image, 0)
	for rows.Next() {
		img, err := scanImage(rows)
		if err != nil {
			return nil, err
		}
		images = append(images, *img)
	}
	return images, rows.Err()
}

func (r *Repository) listImagesMissingDimensions(ctx context.Context) ([]model.Image, error) {
	rows, err := r.db.QueryContext(ctx,
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

func (r *Repository) updateImageDimensions(ctx context.Context, id int64, width, height int) error {
	_, err := r.db.ExecContext(ctx, `UPDATE images SET width = ?, height = ? WHERE id = ?`, width, height, id)
	return err
}

func (r *Repository) updateImageAlias(ctx context.Context, id int64, alias string) error {
	alias = strings.TrimSpace(alias)
	res, err := r.db.ExecContext(ctx, `UPDATE images SET alias = ? WHERE id = ?`,
		sql.NullString{String: alias, Valid: alias != ""}, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// AlbumRepository 专辑相关数据访问。
type AlbumRepository struct {
	db *sql.DB
}

// NewAlbumRepository 创建新的 AlbumRepository 实例。
func NewAlbumRepository(db *sql.DB) *AlbumRepository {
	return &AlbumRepository{db: db}
}

func (r *AlbumRepository) Create(ctx context.Context, name, desc string) (*model.Album, error) {
	res, err := r.db.ExecContext(ctx,
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
	err := r.db.QueryRowContext(ctx,
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
	res, err := r.db.ExecContext(ctx, `DELETE FROM albums WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *AlbumRepository) Update(ctx context.Context, id int64, name, desc string) (*model.Album, error) {
	res, err := r.db.ExecContext(ctx, `UPDATE albums SET name = ?, description = ? WHERE id = ?`, name, desc, id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, sql.ErrNoRows
	}
	return r.GetByID(ctx, id)
}

func (r *AlbumRepository) List(ctx context.Context) ([]model.Album, error) {
	rows, err := r.db.QueryContext(ctx,
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
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM images WHERE album_id = ?`, id).Scan(&n)
	return n, err
}

func (r *Repository) Stats(ctx context.Context) (*model.StatsResponse, error) {
	var s model.StatsResponse
	row := r.db.QueryRowContext(ctx,
		`SELECT COALESCE(COUNT(*),0), COALESCE(SUM(size),0), COALESCE(SUM(views),0) FROM images`)
	if err := row.Scan(&s.TotalImages, &s.TotalSize, &s.TotalViews); err != nil {
		return nil, err
	}

	rows, err := r.db.QueryContext(ctx,
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
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM images WHERE album_id IS NULL`).Scan(&unassigned); err != nil {
		return nil, err
	}
	s.Unassigned = unassigned

	// 获取最近 7 天的每日统计；失败不影响核心统计，但要留日志可查。
	dailyRows, err := r.db.QueryContext(ctx, `
		SELECT DATE(created_at) as date, COUNT(*) as count, COALESCE(SUM(size), 0) as size
		FROM images
		WHERE created_at >= DATE('now', '-7 days')
		GROUP BY DATE(created_at)
		ORDER BY date DESC
	`)
	if err != nil {
		log.Printf("查询每日统计失败: %v", err)
	} else {
		defer dailyRows.Close()
		s.DailyStats = make([]model.DailyStat, 0)
		for dailyRows.Next() {
			var ds model.DailyStat
			if err := dailyRows.Scan(&ds.Date, &ds.Count, &ds.Size); err != nil {
				log.Printf("读取每日统计失败: %v", err)
				break
			}
			s.DailyStats = append(s.DailyStats, ds)
		}
		if err := dailyRows.Err(); err != nil {
			log.Printf("遍历每日统计失败: %v", err)
		}
	}

	// 获取热门图片 Top 10
	popularRows, err := r.db.QueryContext(ctx, `
		SELECT id, original_name, filename, views
		FROM images
		WHERE views > 0
		ORDER BY views DESC
		LIMIT 10
	`)
	if err != nil {
		log.Printf("查询热门图片失败: %v", err)
	} else {
		defer popularRows.Close()
		s.PopularImages = make([]model.PopularImage, 0)
		for popularRows.Next() {
			var p model.PopularImage
			if err := popularRows.Scan(&p.ID, &p.OriginalName, &p.Filename, &p.Views); err != nil {
				log.Printf("读取热门图片失败: %v", err)
				break
			}
			s.PopularImages = append(s.PopularImages, p)
		}
		if err := popularRows.Err(); err != nil {
			log.Printf("遍历热门图片失败: %v", err)
		}
	}

	return &s, nil
}
