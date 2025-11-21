package httpx

import (
	"net/http"
	"strings"
)

// CORSOptions 定义 CORS 配置
type CORSOptions struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
	AllowCredentials bool
	MaxAge           int
}

// DefaultCORS 返回一个宽容的 CORS 中间件（开发环境常用）。
func DefaultCORS() Middleware {
	return CORS(CORSOptions{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowedHeaders:   []string{"Content-Type", "Authorization", "X-Requested-With"},
		AllowCredentials: true,
		MaxAge:           86400,
	})
}

// CORS 跨域资源共享中间件。
func CORS(opts CORSOptions) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				// 非跨域请求，直接跳过
				next.ServeHTTP(w, r)
				return
			}

			// 简单的 Origin 匹配逻辑
			allowed := false
			for _, o := range opts.AllowedOrigins {
				if o == "*" || o == origin {
					allowed = true
					break
				}
			}

			if !allowed {
				next.ServeHTTP(w, r)
				return
			}

			// 设置 CORS Headers
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			if opts.AllowCredentials {
				h.Set("Access-Control-Allow-Credentials", "true")
			}

			// 处理 Preflight OPTIONS 请求
			if r.Method == http.MethodOptions {
				h.Set("Access-Control-Allow-Methods", strings.Join(opts.AllowedMethods, ", "))
				h.Set("Access-Control-Allow-Headers", strings.Join(opts.AllowedHeaders, ", "))
				if len(opts.ExposedHeaders) > 0 {
					h.Set("Access-Control-Expose-Headers", strings.Join(opts.ExposedHeaders, ", "))
				}
				// 204 No Content
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
