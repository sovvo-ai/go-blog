package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	"go-blog/internal/errs"
	"go-blog/internal/pkg/response"
)

// Recovery 捕获 panic，记录堆栈并返回统一的 500 响应。
//
// 堆栈只写日志，绝不返回给客户端：堆栈会泄露文件路径与内部结构。
func Recovery(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}

			// 客户端主动断开时不必输出 500，只记录一条 warn。
			if err, ok := rec.(error); ok && err.Error() == "broken pipe" {
				log.Warn("client_disconnected",
					slog.String("path", c.Request.URL.Path),
				)
				c.Abort()
				return
			}

			log.Error("panic_recovered",
				slog.String("method", c.Request.Method),
				slog.String("path", c.Request.URL.Path),
				slog.String("panic", fmt.Sprint(rec)),
				slog.String("stack", string(debug.Stack())),
			)

			if c.Writer.Written() {
				// 响应已开始写出，无法再改状态码，只能中止。
				c.Abort()
				return
			}
			response.Error(c, errs.New(errs.CodeInternal, http.StatusInternalServerError, "服务器内部错误"))
		}()

		c.Next()
	}
}
