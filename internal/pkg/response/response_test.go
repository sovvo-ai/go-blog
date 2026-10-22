package response_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-blog/internal/errs"
	"go-blog/internal/pkg/reqctx"
	"go-blog/internal/pkg/response"
)

func init() { gin.SetMode(gin.TestMode) }

// newCtx 构造带 request_id 的上下文，模拟经过中间件链之后的场景。
func newCtx(rec *httptest.ResponseRecorder) *gin.Context {
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Set(reqctx.GinRequestID, "req-fixed-0001")
	return c
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) response.Envelope {
	t.Helper()
	var env response.Envelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	return env
}

func TestOK_Envelope(t *testing.T) {
	rec := httptest.NewRecorder()
	response.OK(newCtx(rec), gin.H{"id": "1001"})

	assert.Equal(t, http.StatusOK, rec.Code)
	env := decode(t, rec)
	assert.Equal(t, 0, env.Code)
	assert.Equal(t, "ok", env.Message)
	assert.Equal(t, "req-fixed-0001", env.RequestID)
	assert.NotNil(t, env.Data)
}

func TestCreated_Returns201(t *testing.T) {
	rec := httptest.NewRecorder()
	response.Created(newCtx(rec), gin.H{"id": "1"})

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, 0, decode(t, rec).Code)
}

func TestNoContent_EmptyBody(t *testing.T) {
	rec := httptest.NewRecorder()
	c := newCtx(rec)
	response.NoContent(c)

	// gin 的 Writer 延迟写出状态码，测试中需读 Writer.Status() 而不是 rec.Code
	assert.Equal(t, http.StatusNoContent, c.Writer.Status())
	assert.Empty(t, rec.Body.String(), "204 必须无响应体，不包裹 envelope")
}

func TestError_BusinessErrorMapping(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantHTTP int
		wantCode int
	}{
		{"参数错误", errs.BadRequest("标题不能为空"), http.StatusBadRequest, 10001},
		{"未登录", errs.Unauthorized("请先登录"), http.StatusUnauthorized, 10002},
		{"无权限", errs.Forbidden("无权访问"), http.StatusForbidden, 10003},
		{"不存在", errs.NotFound("文章不存在"), http.StatusNotFound, 10004},
		{"已删除", errs.Gone(30006, "文章已删除"), http.StatusGone, 30006},
		{"冲突", errs.Conflict(30004, "内容已被修改"), http.StatusConflict, 30004},
		{"限流", errs.TooManyRequests("请求过于频繁"), http.StatusTooManyRequests, 10005},
		{"内部错误", errs.Internal(errors.New("db down")), http.StatusInternalServerError, 11001},
		{"依赖不可用", errs.Unavailable(errs.CodeCacheUnavailable, "缓存不可用", nil), http.StatusServiceUnavailable, 11003},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			response.Error(newCtx(rec), tc.err)

			assert.Equal(t, tc.wantHTTP, rec.Code)
			env := decode(t, rec)
			assert.Equal(t, tc.wantCode, env.Code)
			assert.Nil(t, env.Data, "失败响应 data 必须为 null")
			assert.NotEmpty(t, env.RequestID)
		})
	}
}

func TestError_NonBusinessErrorIsMasked(t *testing.T) {
	rec := httptest.NewRecorder()
	response.Error(newCtx(rec), errors.New("sql: connection refused to 10.0.0.5"))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	env := decode(t, rec)
	assert.Equal(t, 11001, env.Code)
	assert.NotContains(t, env.Message, "10.0.0.5", "内部细节不得返回给客户端")
}

func TestError_AbortsHandlerChain(t *testing.T) {
	rec := httptest.NewRecorder()
	c := newCtx(rec)
	response.Error(c, errs.BadRequest("x"))

	assert.True(t, c.IsAborted())
}
