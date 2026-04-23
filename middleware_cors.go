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
// 注意：为了安全，默认禁用了 AllowCredentials。
// 如果需要携带 Cookie/Auth 头，请手动配置 CORS 并指定具体的 AllowedOrigins。
func DefaultCORS() Middleware {
	return CORS(CORSOptions{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowedHeaders: []string{"Content-Type", "Authorization", "X-Requested-With"},
		// 默认为 false。配合 "*" Origin 使用 true 是不安全的且被浏览器禁止。
		AllowCredentials: false,
		MaxAge:           86400,
	})
}

// CORS 跨域资源共享中间件。
func CORS(opts CORSOptions) Middleware {
	// Pre-calculate allowed methods and headers to avoid allocation on every preflight request
	allowedMethodsSlice := []string{strings.Join(opts.AllowedMethods, ", ")}
	allowedHeadersSlice := []string{strings.Join(opts.AllowedHeaders, ", ")}
	var exposedHeadersSlice []string
	if len(opts.ExposedHeaders) > 0 {
		exposedHeadersSlice = []string{strings.Join(opts.ExposedHeaders, ", ")}
	}
	allowCredentialsSlice := []string{"true"}
	allowOriginStarSlice := []string{"*"}
	varyOriginSlice := []string{"Origin"}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				// 非跨域请求，直接跳过
				next.ServeHTTP(w, r)
				return
			}

			// Origin 匹配逻辑
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

			h := w.Header()
			// ⚡ Bolt: 使用直接 map 赋值代替 w.Header().Set()
			// 分配新的 []string 以避免多个请求之间的数据竞争和共享状态污染，
			// 但通过跳过 textproto.CanonicalMIMEHeaderKey 格式化调用，仍然能显著节省 CPU 和内存开销。

			// 如果配置了 AllowCredentials，则必须回显具体的 Origin，不能是 "*"
			varyByOrigin := false
			if opts.AllowCredentials {
				h["Access-Control-Allow-Origin"] = []string{origin}
				h["Access-Control-Allow-Credentials"] = allowCredentialsSlice
				varyByOrigin = true
			} else {
				// 如果没有 Credentials，可以使用配置的值（可能是 "*"）
				// 为了简化，如果有 "*" 匹配，直接返回 "*"
				// 否则返回具体的 origin
				val := origin
				for _, o := range opts.AllowedOrigins {
					if o == "*" {
						val = "*"
						break
					}
				}
				if val == "*" {
					h["Access-Control-Allow-Origin"] = allowOriginStarSlice
				} else {
					h["Access-Control-Allow-Origin"] = []string{val}
				}
				varyByOrigin = val != "*"
			}

			if varyByOrigin {
				if len(h["Vary"]) == 0 {
					h["Vary"] = varyOriginSlice
				} else {
					h["Vary"] = append(h["Vary"], "Origin")
				}
			}

			// 处理 Preflight OPTIONS 请求
			if r.Method == http.MethodOptions {
				h["Access-Control-Allow-Methods"] = allowedMethodsSlice
				h["Access-Control-Allow-Headers"] = allowedHeadersSlice
				if exposedHeadersSlice != nil {
					h["Access-Control-Expose-Headers"] = exposedHeadersSlice
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
