package handler

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go-blog/internal/dto"
	"go-blog/internal/errs"
	"go-blog/internal/middleware"
	"go-blog/internal/pkg/response"
	"go-blog/internal/service"
	"go-blog/internal/storage"
)

// MediaHandler 媒体上传与访问。
//
// 本地驱动下 /media/*key 是公开桶对象的读取入口；chat/ 前缀是私有
// 语义，强制验签后才能读取（API_SPEC 4.7）。生产用 MinIO/S3 时
// 该路由可以下线，改由桶或 CDN 直出。
type MediaHandler struct {
	store storage.Store
	svc   *service.MediaService
}

// NewMediaHandler 构造 MediaHandler。
func NewMediaHandler(store storage.Store, svc *service.MediaService) *MediaHandler {
	return &MediaHandler{store: store, svc: svc}
}

// Serve 处理 GET /media/*key。
//
// gin 的通配路由不能与参数路由共存，因此 GET /media/{id}/url
// 也由本方法分发（路径形如 /media/123/url）。
func (h *MediaHandler) Serve(c *gin.Context) {
	key := strings.TrimPrefix(c.Param("key"), "/")

	// /media/{id}/url：换取访问地址（聊天图片在此做成员校验）
	if strings.HasSuffix(key, "/url") {
		if id := parseUintPath(strings.TrimSuffix(key, "/url")); id > 0 {
			h.mediaURLByID(c, id)
			return
		}
	}

	if key == "" {
		response.Error(c, errs.NotFound("对象不存在"))
		return
	}

	// chat 前缀：私有对象，必须持有效签名且未过期
	if strings.HasPrefix(key, "chat/") {
		exp, _ := strconv.ParseInt(c.Query("exp"), 10, 64)
		if !h.svc.VerifyChatSignature(key, exp, c.Query("sig")) {
			response.Error(c, errs.Forbidden("签名无效或已过期"))
			return
		}
	}

	data, contentType, err := h.store.Get(c.Request.Context(), key)
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrNotExist):
			response.Error(c, errs.NotFound("对象不存在"))
		case errors.Is(err, storage.ErrInvalidKey):
			response.Error(c, errs.NotFound("对象不存在"))
		default:
			response.Error(c, errs.Unavailable(errs.CodeStorageUnavailable, "存储服务暂时不可用", err))
		}
		return
	}

	if strings.HasPrefix(key, "chat/") {
		// 签名 URL 有效期 1 小时，不能给一年 immutable
		c.Header("Cache-Control", "private, max-age=3600")
	} else {
		// 对象 Key 含随机后缀，内容不会变，可长缓存
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
	}
	c.Data(http.StatusOK, contentType, data)
}

// Upload 处理 POST /uploads/images。
func (h *MediaHandler) Upload(c *gin.Context) {
	fh, err := c.FormFile("file")
	if err != nil {
		response.Error(c, errs.BadRequest("缺少文件"))
		return
	}
	f, err := fh.Open()
	if err != nil {
		response.Error(c, errs.BadRequest("文件读取失败"))
		return
	}
	defer f.Close()

	// 先按通用上限拦截，实际上限（chat 5MB）在服务层按 purpose 二次校验
	data, err := io.ReadAll(io.LimitReader(f, h.svc.MaxImageBytes()+1))
	if err != nil {
		response.Error(c, errs.BadRequest("文件读取失败"))
		return
	}
	if int64(len(data)) > h.svc.MaxImageBytes() {
		response.Error(c, errs.BadRequest("文件超出大小限制"))
		return
	}

	purpose := c.DefaultPostForm("purpose", "article")
	res, err := h.svc.UploadImage(c.Request.Context(), middleware.UIDOf(c), purpose, fh.Filename, data)
	if err != nil {
		response.Error(c, err)
		return
	}
	variants := map[string]string{}
	for k, v := range res.Variants {
		variants[k] = v
	}
	response.Created(c, dto.ImageUploadData{
		MediaId:  uidString(res.MediaID),
		Url:      res.URL,
		Width:    res.Width,
		Height:   res.Height,
		Variants: variants,
	})
}

// mediaURLByID 处理 GET /media/{id}/url 的业务部分。
// 该路径经公开的 /media/*key 分发进来，登录态要在这里显式校验。
func (h *MediaHandler) mediaURLByID(c *gin.Context, id uint64) {
	uid := middleware.UIDOf(c)
	if uid == 0 {
		response.Error(c, errs.Unauthorized("请先登录"))
		return
	}
	url, expiresAt, err := h.svc.MediaAccess(c.Request.Context(), uid, id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto.MediaUrlData{Url: url, ExpiresAt: expiresAt})
}
