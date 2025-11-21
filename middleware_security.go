package httpx

import (
	"net/http"
)

// SecurityHeaders 返回一个中间件，用于添加通用的安全响应头。
// 包括:
// - X-Frame-Options: DENY (防止点击劫持)
// - X-Content-Type-Options: nosniff (防止 MIME 嗅探)
// - X-XSS-Protection: 1; mode=block (开启 XSS 过滤)
// - Referrer-Policy: strict-origin-when-cross-origin (控制 Referrer 泄露)
func SecurityHeaders() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()

			// 防止页面被嵌入 iframe (Clickjacking protection)
			if h.Get("X-Frame-Options") == "" {
				h.Set("X-Frame-Options", "DENY")
			}

			// 防止浏览器推断 MIME 类型 (MIME sniffing protection)
			if h.Get("X-Content-Type-Options") == "" {
				h.Set("X-Content-Type-Options", "nosniff")
			}

			// 启用浏览器内置的 XSS 过滤器 (XSS protection)
			if h.Get("X-XSS-Protection") == "" {
				h.Set("X-XSS-Protection", "1; mode=block")
			}

			// 控制 Referrer 信息的传递
			if h.Get("Referrer-Policy") == "" {
				h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			}

			next.ServeHTTP(w, r)
		})
	}
}
