package model

// Image 对应 images 表的一条记录。
type Image struct {
	ID             int64  `json:"id"`
	Hash           string `json:"hash"`
	OriginalName   string `json:"original_name"`
	Filename       string `json:"filename"`
	Size           int64  `json:"size"`
	MIME           string `json:"mime"`
	Width          int    `json:"width"`
	Height         int    `json:"height"`
	Alias          string `json:"alias,omitempty"`
	AlbumID        *int64 `json:"album_id,omitempty"`
	Views          int64  `json:"views"`
	LastAccessedAt string `json:"last_accessed_at,omitempty"`
	CreatedAt      string `json:"created_at"`
	// URL 仅在响应中按需填充，不对应数据库列。
	URL      string `json:"url,omitempty"`
	AliasURL string `json:"alias_url,omitempty"`
}

// Album 对应 albums 表的一条记录。
type Album struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"created_at"`
	ImageCount  int64  `json:"image_count,omitempty"`
}

// StatsResponse 是 GET /api/stats 的返回。
type StatsResponse struct {
	TotalImages int64        `json:"total_images"`
	TotalSize   int64        `json:"total_size"`
	TotalViews  int64        `json:"total_views"`
	Unassigned  int64        `json:"unassigned"`
	Albums      []AlbumCount `json:"albums"`
	DailyStats  []DailyStat  `json:"daily_stats,omitempty"`
	PopularImages []PopularImage `json:"popular_images,omitempty"`
}

type AlbumCount struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	ImageCount int64  `json:"image_count"`
}

// ListResponse 是图片列表分页返回。
type ListResponse struct {
	Images   []Image `json:"images"`
	Total    int64   `json:"total"`
	Page     int     `json:"page"`
	PageSize int     `json:"page_size"`
}

// DailyStat 每日统计数据。
type DailyStat struct {
	Date  string `json:"date"`  // 格式：YYYY-MM-DD
	Count int64  `json:"count"` // 当天上传数量
	Size  int64  `json:"size"`  // 当天上传总大小
}

// PopularImage 热门图片。
type PopularImage struct {
	ID           int64  `json:"id"`
	OriginalName string `json:"original_name"`
	Filename     string `json:"filename"`
	Views        int64  `json:"views"`
	URL          string `json:"url"`
}
