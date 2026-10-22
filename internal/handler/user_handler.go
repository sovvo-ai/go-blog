package handler

import (
	"io"
	"strconv"

	"github.com/gin-gonic/gin"

	"go-blog/internal/dto"
	"go-blog/internal/errs"
	"go-blog/internal/middleware"
	"go-blog/internal/pkg/authcookie"
	"go-blog/internal/pkg/response"
	"go-blog/internal/service"
)

// UserHandler 处理个人资料相关接口。
type UserHandler struct {
	users  *service.UserService
	media  *service.MediaService
	cookie *authcookie.Manager
}

// NewUserHandler 构造 UserHandler。
func NewUserHandler(users *service.UserService, media *service.MediaService, cookie *authcookie.Manager) *UserHandler {
	return &UserHandler{users: users, media: media, cookie: cookie}
}

// Me 处理 GET /users/me。
func (h *UserHandler) Me(c *gin.Context) {
	u, err := h.users.Profile(c.Request.Context(), middleware.UIDOf(c))
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, toUserFull(u))
}

// UpdateProfile 处理 PATCH /users/me。
func (h *UserHandler) UpdateProfile(c *gin.Context) {
	var req dto.UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errs.BadRequest("请求参数格式不正确"))
		return
	}
	if req.Nickname == nil && req.Bio == nil {
		response.Error(c, errs.BadRequest("没有需要修改的内容"))
		return
	}

	u, err := h.users.UpdateProfile(c.Request.Context(), middleware.UIDOf(c), req.Nickname, req.Bio)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, toUserFull(u))
}

// ChangePassword 处理 PUT /users/me/password。
// 成功后会吊销全部会话，因此同时清除当前浏览器 Cookie。
func (h *UserHandler) ChangePassword(c *gin.Context) {
	var req dto.ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errs.BadRequest("请求参数格式不正确"))
		return
	}

	if err := h.users.ChangePassword(c.Request.Context(), middleware.UIDOf(c), req.OldPassword, req.NewPassword); err != nil {
		response.Error(c, err)
		return
	}

	h.cookie.Clear(c.Writer)
	response.NoContent(c)
}

// UpdateAvatar 处理 PUT /users/me/avatar（multipart，字段 file）。
func (h *UserHandler) UpdateAvatar(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		response.Error(c, errs.BadRequest("缺少文件字段 file").WithData(fieldErrorsData("file", "请选择图片")))
		return
	}
	// 先按声明的尺寸挡一次，避免把超大文件整个读进内存
	maxBytes := int64(h.media.MaxImageBytes())
	if fileHeader.Size > maxBytes {
		response.Error(c, errs.BadRequest("图片超过大小限制").WithData(fieldErrorsData("file", "图片超过大小限制")))
		return
	}

	f, err := fileHeader.Open()
	if err != nil {
		response.Error(c, errs.BadRequest("无法读取上传文件"))
		return
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		response.Error(c, errs.BadRequest("读取上传文件失败"))
		return
	}

	res, err := h.media.UploadAvatar(c.Request.Context(), middleware.UIDOf(c), fileHeader.Filename, data)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto.AvatarData{
		AvatarUrl: res.URL,
		MediaId:   uidString(res.MediaID),
	})
}

// PublicProfile 处理 GET /users/{id}。
func (h *UserHandler) PublicProfile(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Error(c, errs.NotFound("用户不存在"))
		return
	}

	u, count, err := h.users.PublicProfile(c.Request.Context(), id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, toUserPublic(u, count))
}

// fieldErrorsData 构造字段级校验明细。
func fieldErrorsData(field, msg string) map[string]any {
	return map[string]any{"errors": []map[string]string{{"field": field, "message": msg}}}
}
