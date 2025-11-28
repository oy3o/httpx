package httpx

import (
	"context"
	"fmt"
	"net/http"

	"github.com/felixge/httpsnoop"
)

// Middleware 标准中间件定义
type Middleware func(http.Handler) http.Handler

// Chain 组合多个中间件
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// Recovery 捕获 Panic 防止服务崩溃
func Recovery(panicHook func(ctx context.Context, err interface{})) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					if panicHook != nil {
						panicHook(r.Context(), err)
					}
					w.WriteHeader(http.StatusInternalServerError)
					Error(w, r, &HttpError{
						HttpCode: 500,
						BizCode:  "PANIC",
						Msg:      fmt.Sprintf("panic: %v", err),
					})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// LogFunc 定义日志回调签名
type LogFunc func(r *http.Request, metrics httpsnoop.Metrics)

// Logger 使用 httpsnoop 包装 ResponseWriter
func Logger(logFunc LogFunc) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// httpsnoop 会自动检测 w 是否支持 Flusher/Hijacker 并自动包装
			m := httpsnoop.CaptureMetrics(next, w, r)

			if logFunc != nil {
				logFunc(r, m)
			}
		})
	}
}
