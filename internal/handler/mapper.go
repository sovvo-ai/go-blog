package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"go-blog/internal/authz"
	"go-blog/internal/dto"
	"go-blog/internal/errs"
	"go-blog/internal/model"
	"go-blog/internal/pkg/response"
)

// toUserFull 把模型转换为契约中的 UserFull。
func toUserFull(u *model.User) dto.UserFull {
	return dto.UserFull{
		Id:        strconv.FormatUint(u.ID, 10),
		Username:  u.Username,
		Email:     openapiEmail(u.Email),
		Nickname:  u.DisplayName(),
		AvatarUrl: u.AvatarURL,
		Bio:       u.Bio,
		Role:      dto.Role(authz.Role(u.Role).String()),
		Status:    dto.UserStatus(statusName(u.Status)),
		// 契约要求 RFC3339 UTC；数据库以本地时区存储，输出前统一转换
		CreatedAt: u.CreatedAt.UTC(),
	}
}

// toUserPublic 转换为公开主页结构：不含 email、status、登录记录。
func toUserPublic(u *model.User, articleCount int64) dto.UserPublic {
	return dto.UserPublic{
		Id:           strconv.FormatUint(u.ID, 10),
		Username:     u.Username,
		Nickname:     u.DisplayName(),
		AvatarUrl:    u.AvatarURL,
		Bio:          u.Bio,
		ArticleCount: int(articleCount),
		CreatedAt:    u.CreatedAt.UTC(),
	}
}

// openapiEmail 把字符串转成生成代码中的 Email 类型。
func openapiEmail(s string) openapi_types.Email { return openapi_types.Email(s) }

func statusName(status uint8) string {
	switch status {
	case model.StatusPendingVerify:
		return "pending_verify"
	case model.StatusActive:
		return "active"
	case model.StatusDisabled:
		return "disabled"
	default:
		return "disabled"
	}
}

// uidString 把用户 ID 转成契约要求的字符串形式，避免前端精度丢失。
func uidString(uid uint64) string { return strconv.FormatUint(uid, 10) }

// ---- 路径与查询参数解析（全 handler 共用）----

// parseUintPath 解析路径/查询参数中的无符号 ID，非法返回 0。
func parseUintPath(raw string) uint64 {
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// parsePathID 解析路径 ID 并在非法时直接响应 400。
func parsePathID(c *gin.Context) (uint64, bool) {
	id := parseUintPath(c.Param("id"))
	if id == 0 {
		response.Error(c, errs.BadRequest("无效的 ID"))
		return 0, false
	}
	return id, true
}

// parseOptionalUint8 解析可选的枚举型查询参数（role/status 等）。
func parseOptionalUint8(raw string) (*uint8, bool) {
	if raw == "" {
		return nil, false
	}
	v := parseUintPath(raw)
	if v > 255 {
		return nil, false
	}
	b := uint8(v)
	return &b, true
}

// nullString 空串返回 nil，供可空游标字段使用。
func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ---- 管理端列表的状态筛选解析 ----
// 契约里 status 是状态名（openapi.yaml 四个管理端列表均为 string），
// 库内是 uint8；这里统一做名→值映射，并保留数字写法兼容旧调用。

func parseUserStatusFilter(raw string) (*uint8, bool) {
	switch values := map[string]uint8{
		"pending_verify": model.StatusPendingVerify,
		"active":         model.StatusActive,
		"disabled":       model.StatusDisabled,
	}; {
	case raw == "":
		return nil, false
	default:
		if v, ok := values[raw]; ok {
			return &v, true
		}
		return parseOptionalUint8(raw)
	}
}

func parseArticleStatusFilter(raw string) (*uint8, bool) {
	values := map[string]uint8{
		"draft":     model.ArticleDraft,
		"pending":   model.ArticlePending,
		"published": model.ArticlePublished,
		"rejected":  model.ArticleRejected,
		"offline":   model.ArticleOffline,
		"archived":  model.ArticleArchived,
		"deleted":   model.ArticleDeleted,
	}
	if raw == "" {
		return nil, false
	}
	if v, ok := values[raw]; ok {
		return &v, true
	}
	return parseOptionalUint8(raw)
}

func parseCommentStatusFilter(raw string) (*uint8, bool) {
	values := map[string]uint8{
		"pending":  model.CommentPending,
		"approved": model.CommentApproved,
		"rejected": model.CommentRejected,
		"deleted":  model.CommentDeleted,
	}
	if raw == "" {
		return nil, false
	}
	if v, ok := values[raw]; ok {
		return &v, true
	}
	return parseOptionalUint8(raw)
}

func parseReportStatusFilter(raw string) (*uint8, bool) {
	values := map[string]uint8{
		"pending":  model.ReportPending,
		"accepted": model.ReportAccepted,
		"rejected": model.ReportRejected,
	}
	if raw == "" {
		return nil, false
	}
	if v, ok := values[raw]; ok {
		return &v, true
	}
	return parseOptionalUint8(raw)
}
