package errs

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConstructors_HTTPMapping(t *testing.T) {
	cases := []struct {
		name     string
		err      *BusinessError
		wantCode int
		wantHTTP int
	}{
		{"参数错误", BadRequest("参数错误"), CodeInvalidParam, http.StatusBadRequest},
		{"未登录", Unauthorized("请先登录"), CodeUnauthorized, http.StatusUnauthorized},
		{"无权限", Forbidden("无权操作"), CodeForbidden, http.StatusForbidden},
		{"不存在", NotFound("资源不存在"), CodeNotFound, http.StatusNotFound},
		{"已删除", Gone(30006, "文章已删除"), 30006, http.StatusGone},
		{"冲突", Conflict(30004, "内容已被修改"), 30004, http.StatusConflict},
		{"限流", TooManyRequests("请求过于频繁"), CodeTooManyRequests, http.StatusTooManyRequests},
		{"内部错误", Internal(errors.New("db down")), CodeInternal, http.StatusInternalServerError},
		{"依赖不可用", Unavailable(CodeCacheUnavailable, "缓存不可用", nil), CodeCacheUnavailable, http.StatusServiceUnavailable},
		{"未就绪", NotReady("依赖 mysql 不可用", nil), CodeNotReady, http.StatusServiceUnavailable},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantCode, tc.err.Code)
			assert.Equal(t, tc.wantHTTP, tc.err.HTTP)
			assert.NotEmpty(t, tc.err.Message)
		})
	}
}

func TestFrom_NonBusinessErrorBecomesInternal(t *testing.T) {
	cause := errors.New("connection refused")
	be := From(cause)

	require.NotNil(t, be)
	assert.Equal(t, CodeInternal, be.Code)
	assert.Equal(t, http.StatusInternalServerError, be.HTTP)
	// 原始错误必须保留在 Cause 中供日志使用，但文案不能泄露给客户端
	assert.Equal(t, cause, be.Cause)
	assert.NotContains(t, be.Message, "connection refused")
}

func TestFrom_BusinessErrorPassThrough(t *testing.T) {
	orig := BadRequest("标题不能为空")
	be := From(orig)

	assert.Equal(t, orig.Code, be.Code)
	assert.Equal(t, orig.Message, be.Message)
}

func TestFrom_WrappedBusinessError(t *testing.T) {
	orig := NotFound("文章不存在")
	wrapped := errors.Join(errors.New("层叠上下文"), orig)

	be := From(wrapped)
	assert.Equal(t, CodeNotFound, be.Code, "包装后仍应识别为业务错误")
}

func TestWithCause_DoesNotMutateOriginal(t *testing.T) {
	base := BadRequest("参数错误")
	cause := errors.New("字段 tags 长度超限")
	withCause := base.WithCause(cause)

	assert.Nil(t, base.Cause, "原对象不应被修改，避免共享错误被污染")
	require.NotNil(t, withCause.Cause)
	assert.Equal(t, CodeInvalidParam, withCause.Code)
	assert.ErrorIs(t, withCause, cause, "WithCause 后仍可用 errors.Is 命中原错误")
}

func TestWithMessage_DoesNotMutateOriginal(t *testing.T) {
	base := NotFound("资源不存在")
	replaced := base.WithMessage("文章不存在")

	assert.Equal(t, "资源不存在", base.Message)
	assert.Equal(t, "文章不存在", replaced.Message)
	assert.Equal(t, base.Code, replaced.Code)
}
