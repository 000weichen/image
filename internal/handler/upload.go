package handler

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"imgbed/internal/service"
)

func (h *Handler) Upload(c *gin.Context) {
	maxBytes := h.cfg.MaxBytes()
	// multipart 编码有边界与头部开销，body 限制留 1MB 余量；
	// 文件本体大小在读取后精确校验。
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes+1<<20)
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		if isBodyTooLarge(err) {
			h.tooLarge(c)
			return
		}
		c.JSON(400, gin.H{"error": "读取文件失败", "message": err.Error()})
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		if isBodyTooLarge(err) {
			h.tooLarge(c)
			return
		}
		c.JSON(400, gin.H{"error": "读取文件内容失败", "message": err.Error()})
		return
	}
	if int64(len(data)) > maxBytes {
		h.tooLarge(c)
		return
	}

	var albumID *int64
	if raw := strings.TrimSpace(c.PostForm("album")); raw != "" {
		// 允许传相册名称或数字 id
		id, aerr := resolveAlbumID(c.Request.Context(), h.albumRepo, raw)
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

func (h *Handler) tooLarge(c *gin.Context) {
	c.JSON(413, gin.H{"error": "文件过大", "message": fmt.Sprintf("超过 %d MB 大小限制", h.cfg.Limits.MaxSizeMB)})
}

func isBodyTooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr) || strings.Contains(err.Error(), "request body too large")
}
