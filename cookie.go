package httpx

import (
	"net/http"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Core Logic: 透明的前缀管理
// ---------------------------------------------------------------------------

// resolveWriteName 根据最终的 Cookie 属性决定写入时的实际名称。
// 优先级: __Host- > __Secure- > 原名
func resolveWriteName(name string, c *http.Cookie) string {
	// 如果业务层已经手动加了前缀，就不再画蛇添足
	if strings.HasPrefix(name, "__Host-") || strings.HasPrefix(name, "__Secure-") {
		return name
	}

	// 只有 Secure 的 Cookie 才有资格加前缀
	if !c.Secure {
		return name
	}

	// __Host- 要求: Secure=true, Path="/", Domain="" (即不指定，由浏览器默认当前Host)
	if c.Path == "/" && c.Domain == "" {
		return "__Host-" + name
	}

	// __Secure- 要求: Secure=true
	return "__Secure-" + name
}

// ---------------------------------------------------------------------------
// GetCookie: 智能读取 (Priority Probing)
// ---------------------------------------------------------------------------

// GetCookie 透明地获取 Cookie 值。
// 它会按照安全优先级依次尝试：__Host-name -> __Secure-name -> name。
// 这意味着如果存在安全的变体，我们将优先读取它，忽略不安全的同名 Cookie（防御 Cookie Tossing）。
func GetCookie(r *http.Request, name string) (string, error) {
	// 1. 尝试 __Host- (最安全)
	if c, err := r.Cookie("__Host-" + name); err == nil {
		return c.Value, nil
	}

	// 2. 尝试 __Secure-
	if c, err := r.Cookie("__Secure-" + name); err == nil {
		return c.Value, nil
	}

	// 3. 尝试 原名
	// 注意：如果我们在生产环境强制了 __Host-，这里可能读到的是攻击者注入的普通 Cookie。
	// 但由于我们 SetCookie 时总是优先写入带前缀的，且 GetCookie 优先读带前缀的，
	// 所以只要我们成功写入过安全 Cookie，攻击者的 Cookie 就会被屏蔽。
	if c, err := r.Cookie(name); err == nil {
		return c.Value, nil
	}

	return "", http.ErrNoCookie
}

// ---------------------------------------------------------------------------
// SetCookie: 智能写入 (Auto Armor)
// ---------------------------------------------------------------------------

// SetCookie 写入 Cookie。
// 除非显式使用 WithInsecure() 或设置了 Domain，否则它会自动添加 __Host- 前缀。
func SetCookie(w http.ResponseWriter, name string, value string, opts ...CookieOption) {
	// 默认配置：最安全的基准
	c := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,                 // 默认开启
		SameSite: http.SameSiteLaxMode, // 默认 Lax
	}

	for _, opt := range opts {
		opt(c)
	}

	// 关键时刻：根据最终配置决定披什么甲
	c.Name = resolveWriteName(name, c)

	http.SetCookie(w, c)
}

// CookieOption用于配置 Cookie 的属性。
type CookieOption func(*http.Cookie)

// WithCookieTTL 设置存活时间。
func WithCookieTTL(duration time.Duration) CookieOption {
	return func(c *http.Cookie) {
		c.MaxAge = int(duration.Seconds())
		if duration > 0 {
			c.Expires = time.Now().Add(duration)
		}
	}
}

// WithCookieDomain 设置域名。
// 注意：通常为了安全性，不设置域名（默认当前Host）是最好的，除非你需要子域共享。
func WithCookieDomain(domain string) CookieOption {
	return func(c *http.Cookie) {
		c.Domain = domain
	}
}

// WithCookiePath 设置路径，默认为 "/"。
func WithCookiePath(path string) CookieOption {
	return func(c *http.Cookie) {
		c.Path = path
	}
}

// WithSameSiteStrict 启用最严格的防 CSRF 模式。
// 在此模式下，任何跨站跳转（甚至是点击链接）都不会携带 Cookie。
// 适用于高敏感操作（如修改密码）或 API 接口。
func WithSameSiteStrict() CookieOption {
	return func(c *http.Cookie) {
		c.SameSite = http.SameSiteStrictMode
	}
}

// WithSameSiteNone 允许跨站携带（必须配合 Secure）。
// 仅用于第三方嵌入场景（如 iframe 中的支付窗口），极不推荐用于主鉴权。
func WithSameSiteNone() CookieOption {
	return func(c *http.Cookie) {
		c.SameSite = http.SameSiteNoneMode
		c.Secure = true // 浏览器强制要求
	}
}

// WithExposed 允许 JS 读取（移除 HttpOnly）。
// 警告：仅用于非敏感数据（如 UI 主题设置、用户偏好），绝不可用于 Token。
func WithExposed() CookieOption {
	return func(c *http.Cookie) {
		c.HttpOnly = false
	}
}

// WithInsecure 允许 HTTP 传输。
// 仅用于本地 localhost 开发调试。
func WithInsecure() CookieOption {
	return func(c *http.Cookie) {
		c.Secure = false
	}
}

// ---------------------------------------------------------------------------
// DelCookie: 饱和式打击 (Nuke Strategy)
// ---------------------------------------------------------------------------

// DelCookie 删除 Cookie。
// 为了防止残留，它会尝试删除该名字的所有可能变体（__Host-, __Secure-, 原名）。
func DelCookie(w http.ResponseWriter, name string, opts ...CookieOption) {
	// 构造一个用于过期的模板
	baseCookie := &http.Cookie{
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0), // 1970-01-01
	}

	for _, opt := range opts {
		opt(baseCookie)
	}

	// 变体列表
	variants := []string{
		"__Host-" + name,
		"__Secure-" + name,
		name,
	}

	for _, vName := range variants {
		// 复制一份配置，避免修改 baseCookie
		c := *baseCookie
		c.Name = vName
		c.Value = ""

		// 只有 Secure 的 Cookie 才能带前缀，所以如果要删除带前缀的，必须保证 Secure=true
		// 这里我们强制为 true 以便能发出 Set-Cookie: __Host-xxx=...; Secure
		// 浏览器只有看到 Secure 标记才会允许操作带前缀的 Cookie
		if strings.HasPrefix(vName, "__") {
			c.Secure = true
		}

		http.SetCookie(w, &c)
	}
}
