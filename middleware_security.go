package httpx

import (
	"fmt"
	"net/http"
)

// SecurityConfig 定义安全头配置
type SecurityConfig struct {
	// HSTSMaxAgeSeconds 启用 Strict-Transport-Security。0 表示禁用。
	// 生产环境建议设置为 31536000 (1年)。
	HSTSMaxAgeSeconds int

	// HSTSIncludeSubdomains 是否包含子域名
	HSTSIncludeSubdomains bool

	// CSP Content-Security-Policy 值。
	// 例如: "default-src 'self'"
	CSP string
}

// SecurityHeaders 返回一个中间件，用于添加通用的安全响应头。
// 包括:
// - X-Frame-Options: DENY (防止点击劫持)
// - X-Content-Type-Options: nosniff (防止 MIME 嗅探)
// - X-XSS-Protection: 1; mode=block (开启 XSS 过滤)
// - Referrer-Policy: strict-origin-when-cross-origin (控制 Referrer 泄露)
func SecurityHeaders(cfgs ...SecurityConfig) Middleware {
	var cfg SecurityConfig
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
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

			// 2. HSTS (仅在 HTTPS 下生效)
			if cfg.HSTSMaxAgeSeconds > 0 {
				val := fmt.Sprintf("max-age=%d", cfg.HSTSMaxAgeSeconds)
				if cfg.HSTSIncludeSubdomains {
					val += "; includeSubDomains"
				}
				h.Set("Strict-Transport-Security", val)
			}

			// 3. CSP
			if cfg.CSP != "" {
				h.Set("Content-Security-Policy", cfg.CSP)
			}

			next.ServeHTTP(w, r)
		})
	}
}
