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

// 预先分配静态的安全响应头值，避免每次请求都在 w.Header().Set 中分配新的 []string
var (
	secValXFrameOptions        = []string{"DENY"}
	secValXContentTypeOptions  = []string{"nosniff"}
	secValXXSSProtection       = []string{"1; mode=block"}
	secValReferrerPolicy       = []string{"strict-origin-when-cross-origin"}
)

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
	var hstsValue []string
	if cfg.HSTSMaxAgeSeconds > 0 {
		val := fmt.Sprintf("max-age=%d", cfg.HSTSMaxAgeSeconds)
		if cfg.HSTSIncludeSubdomains {
			val += "; includeSubDomains"
		}
		hstsValue = []string{val}
	}

	var cspValue []string
	if cfg.CSP != "" {
		cspValue = []string{cfg.CSP}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()

			// ⚡ Bolt: 使用直接 map 赋值代替 w.Header().Set()
			// 1. 避免了内部对 key 的 CanonicalMIMEHeaderKey 格式化调用
			// 2. 避免了每次 Set() 都会创建新的 []string{value} 的内存分配
			// 注意: map key 必须是 Canonicalized 的格式

			// 防止页面被嵌入 iframe (Clickjacking protection)
			if len(h["X-Frame-Options"]) == 0 {
				h["X-Frame-Options"] = secValXFrameOptions
			}

			// 防止浏览器推断 MIME 类型 (MIME sniffing protection)
			if len(h["X-Content-Type-Options"]) == 0 {
				h["X-Content-Type-Options"] = secValXContentTypeOptions
			}

			// 启用浏览器内置的 XSS 过滤器 (XSS protection)
			if len(h["X-Xss-Protection"]) == 0 {
				h["X-Xss-Protection"] = secValXXSSProtection
			}

			// 控制 Referrer 信息的传递
			if len(h["Referrer-Policy"]) == 0 {
				h["Referrer-Policy"] = secValReferrerPolicy
			}

			// 2. HSTS (仅在 HTTPS 下生效)
			if hstsValue != nil {
				h["Strict-Transport-Security"] = hstsValue
			}

			// 3. CSP
			if cspValue != nil {
				h["Content-Security-Policy"] = cspValue
			}

			next.ServeHTTP(w, r)
		})
	}
}
