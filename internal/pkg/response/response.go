// Package response 负责统一响应封装（API_SPEC 1.4）。
//
// 成功与失败都包裹为 {code, message, data, request_id}，
// 唯一例外是 204 No Content：按 HTTP 语义返回空响应体，不包裹。
package response

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go-blog/internal/errs"
	"go-blog/internal/pkg/reqctx"
)

// Envelope 是所有 JSON 响应的统一结构。
type Envelope struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	Data      any    `json:"data"`
	RequestID string `json:"request_id"`
}

// OK 返回 200 与业务数据。
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Envelope{
		Code:      errs.CodeOK,
		Message:   "ok",
		Data:      data,
		RequestID: requestID(c),
	})
}

// Created 返回 201 与新建资源的信息。
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, Envelope{
		Code:      errs.CodeOK,
		Message:   "ok",
		Data:      data,
		RequestID: requestID(c),
	})
}

// Accepted 返回 202：请求已受理，实际动作在后台执行（如全量重建索引）。
func Accepted(c *gin.Context, data any) {
	c.JSON(http.StatusAccepted, Envelope{
		Code:      errs.CodeOK,
		Message:   "ok",
		Data:      data,
		RequestID: requestID(c),
	})
}

// NoContent 返回 204，响应体为空。
func NoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

// Error 把任意 error 归一化为业务错误后输出。
// 非业务错误会被收敛为 500 / 11001，不泄露内部细节。
func Error(c *gin.Context, err error) {
	be := errs.From(err)
	if be == nil {
		OK(c, nil)
		return
	}
	c.Set("biz_code", be.Code)
	c.AbortWithStatusJSON(be.HTTP, Envelope{
		Code:      be.Code,
		Message:   be.Message,
		Data:      be.Data,
		RequestID: requestID(c),
	})
}

func requestID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	return c.GetString(reqctx.GinRequestID)
}
