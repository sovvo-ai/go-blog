package errs

import (
	"errors"
	"fmt"
	"net/http"
)

// BusinessError 是 service 层统一返回的错误类型（TECH_DESIGN 5.4）。
// Cause 只用于日志，绝不返回给客户端。
type BusinessError struct {
	Code    int    // 业务码
	Message string // 可直接展示给用户的中文提示
	HTTP    int    // HTTP 状态码
	Cause   error  // 内部原因
	Data    any    // 附加到响应 data 的结构化信息（如字段级校验明细）
}

// Error 实现 error 接口。输出包含内部原因，便于日志定位。
func (e *BusinessError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%d] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

// Unwrap 支持 errors.Is / errors.As 穿透到内部原因。
func (e *BusinessError) Unwrap() error { return e.Cause }

// WithCause 返回附带内部原因的副本，不修改原对象（避免共享错误被污染）。
func (e *BusinessError) WithCause(cause error) *BusinessError {
	cp := *e
	cp.Cause = cause
	return &cp
}

// WithData 返回附带结构化数据的副本，例如字段级校验错误。
func (e *BusinessError) WithData(data any) *BusinessError {
	cp := *e
	cp.Data = data
	return &cp
}

// WithMessage 返回替换用户可见文案的副本。
func (e *BusinessError) WithMessage(msg string) *BusinessError {
	cp := *e
	cp.Message = msg
	return &cp
}

// New 构造自定义业务错误。仅在既有构造函数无法表达时使用。
func New(code, httpStatus int, msg string) *BusinessError {
	return &BusinessError{Code: code, Message: msg, HTTP: httpStatus}
}

// BadRequest 参数校验失败，400 / 10001。
func BadRequest(msg string) *BusinessError {
	return New(CodeInvalidParam, http.StatusBadRequest, msg)
}

// Unauthorized 未登录或凭证失效，401 / 10002。
func Unauthorized(msg string) *BusinessError {
	return New(CodeUnauthorized, http.StatusUnauthorized, msg)
}

// Forbidden 已登录但无权限，403 / 10003。
func Forbidden(msg string) *BusinessError {
	return New(CodeForbidden, http.StatusForbidden, msg)
}

// NotFound 资源不存在或无权查看，404 / 10004。
// PRD 2.2.1：草稿等非公开内容对无权者一律返回 404，不泄露存在性。
func NotFound(msg string) *BusinessError {
	return New(CodeNotFound, http.StatusNotFound, msg)
}

// Gone 资源已永久删除，410。业务码由调用方指定（文章用 30006）。
func Gone(code int, msg string) *BusinessError {
	return New(code, http.StatusGone, msg)
}

// Conflict 状态冲突，409。业务码由调用方指定（如 30003 slug 冲突、30004 乐观锁）。
func Conflict(code int, msg string) *BusinessError {
	return New(code, http.StatusConflict, msg)
}

// TooManyRequests 触发限流，429 / 10005。
func TooManyRequests(msg string) *BusinessError {
	return New(CodeTooManyRequests, http.StatusTooManyRequests, msg)
}

// Internal 未预期的内部错误，500 / 11001。
// 客户端只看到通用文案，真实原因写入日志。
func Internal(cause error) *BusinessError {
	return &BusinessError{
		Code:    CodeInternal,
		Message: "服务器内部错误",
		HTTP:    http.StatusInternalServerError,
		Cause:   cause,
	}
}

// Unavailable 上游依赖不可用，503。业务码用 11002–11005。
func Unavailable(code int, msg string, cause error) *BusinessError {
	return &BusinessError{Code: code, Message: msg, HTTP: http.StatusServiceUnavailable, Cause: cause}
}

// NotReady 依赖未就绪（用于就绪探针），503 / 11006。
func NotReady(msg string, cause error) *BusinessError {
	return &BusinessError{Code: CodeNotReady, Message: msg, HTTP: http.StatusServiceUnavailable, Cause: cause}
}

// From 把任意 error 归一化为 *BusinessError。
// 非业务错误一律视为内部错误，避免把底层实现细节暴露给客户端。
func From(err error) *BusinessError {
	if err == nil {
		return nil
	}
	var be *BusinessError
	if errors.As(err, &be) {
		return be
	}
	return Internal(err)
}
