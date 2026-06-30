package handler

import (
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"imgbed/internal/service"
)

func (h *Handler) Upload(c *gin.Context) {
	// 限制请求体大小
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.cfg.MaxBytes())
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "读取文件失败", "message": err.Error()})
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		c.JSON(400, gin.H{"error": "读取文件内容失败", "message": err.Error()})
		return
	}
	if int64(len(data)) > h.cfg.MaxBytes() {
		c.JSON(413, gin.H{"error": "文件过大", "message": "超过大小限制"})
		return
	}

	var albumID *int64
	if raw := strings.TrimSpace(c.PostForm("album")); raw != "" {
		// 允许传相册名称或数字 id
		id, aerr := h.albums.ResolveID(c.Request.Context(), raw)
		if aerr != nil {
			c.JSON(400, gin.H{"error": "相册不存在"})
			return
		}
		albumID = &id
	}

	img, err := h.imageSvc.Save(c.Request.Context(), &service.UploadRequest{
		Filename: header.Filename,
		Data:     data,
		AlbumID:  albumID,
	})
	if err != nil {
		c.JSON(500, gin.H{"error": "上传失败", "message": err.Error()})
		return
	}

	c.JSON(200, img)
}
