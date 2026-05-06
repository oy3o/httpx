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

	// Pre-calculate HSTS header value to avoid string formatting and allocation on every request
	var hstsValueStr string
	if cfg.HSTSMaxAgeSeconds > 0 {
		val := fmt.Sprintf("max-age=%d", cfg.HSTSMaxAgeSeconds)
		if cfg.HSTSIncludeSubdomains {
			val += "; includeSubDomains"
		}
		hstsValueStr = val
	}

	var cspValueStr string
	if cfg.CSP != "" {
		cspValueStr = cfg.CSP
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()

			// ⚡ Bolt: Allocate a new slice per request to prevent data races and global state mutation while bypassing CanonicalMIMEHeaderKey formatting overhead.
			// 1. 避免了内部对 key 的 CanonicalMIMEHeaderKey 格式化调用
			// 注意: map key 必须是 Canonicalized 的格式

			// 防止页面被嵌入 iframe (Clickjacking protection)
			if len(h["X-Frame-Options"]) == 0 {
				h["X-Frame-Options"] = []string{"DENY"}
			}

			// 防止浏览器推断 MIME 类型 (MIME sniffing protection)
			if len(h["X-Content-Type-Options"]) == 0 {
				h["X-Content-Type-Options"] = []string{"nosniff"}
			}

			// 启用浏览器内置的 XSS 过滤器 (XSS protection)
			if len(h["X-Xss-Protection"]) == 0 {
				h["X-Xss-Protection"] = []string{"1; mode=block"}
			}

			// 控制 Referrer 信息的传递
			if len(h["Referrer-Policy"]) == 0 {
				h["Referrer-Policy"] = []string{"strict-origin-when-cross-origin"}
			}

			// 2. HSTS (仅在 HTTPS 下生效)
			if hstsValueStr != "" {
				h["Strict-Transport-Security"] = []string{hstsValueStr}
			}

			// 3. CSP
			if cspValueStr != "" {
				h["Content-Security-Policy"] = []string{cspValueStr}
			}

			next.ServeHTTP(w, r)
		})
	}
}
