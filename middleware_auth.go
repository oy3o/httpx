package httpx

import (
	"context"
	"net/http"
	"strings"
)

// AuthValidator 定义验证回调函数签名。
// 返回的 any 将被注入到 Context 中（例如 User 对象）。
type AuthValidator func(ctx context.Context, token string) (any, error)

type contextKey string

const IdentityKey contextKey = "identity"

// GetIdentity 从 Context 中获取身份信息。
func GetIdentity(ctx context.Context) any {
	return ctx.Value(IdentityKey)
}

// AuthBearer Bearer Token 认证中间件。
// realm: 认证域名称，例如 "MyAPI"。如果为空，默认为 "Restricted"。
func AuthBearer(validator AuthValidator, realm string) Middleware {
	if realm == "" {
		realm = "Restricted"
	}
	// 提前组装 Header，避免每次请求都进行字符串拼接
	// WWW-Authenticate 头通常只在 Header 认证失败时需要严格遵循格式
	// 但为了统一，我们保持原样
	authHeader := `Bearer realm="` + realm + `"`
	errHeader := `Bearer realm="` + realm + `", error="invalid_token"`

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var token string

			// 1. 尝试从 Header 获取
			auth := r.Header.Get("Authorization")
			if auth != "" {
				const prefix = "Bearer "
				if len(auth) < len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
					w.Header().Set("WWW-Authenticate", authHeader)
					Error(w, r, &HttpError{
						HttpCode: http.StatusUnauthorized,
						BizCode:  "Unauthorized",
						Msg:      "invalid bearer format",
					})
					return
				}
				token = auth[len(prefix):]
			} else {
				// 2. 尝试从 Query 获取
				// Query 参数通常直接包含 token，没有 "Bearer " 前缀
				token = r.URL.Query().Get("access_token")
			}

			if token == "" {
				w.Header().Set("WWW-Authenticate", authHeader)
				Error(w, r, &HttpError{
					HttpCode: http.StatusUnauthorized,
					BizCode:  "Unauthorized",
					Msg:      "token missing",
				})
				return
			}

			identity, err := validator(r.Context(), token)
			if err != nil {
				w.Header().Set("WWW-Authenticate", errHeader)
				Error(w, r, &HttpError{
					HttpCode: http.StatusUnauthorized,
					BizCode:  "Unauthorized",
					Msg:      "invalid token",
				})
				return
			}

			ctx := context.WithValue(r.Context(), IdentityKey, identity)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// BasicValidator 定义 Basic Auth 验证回调。
type BasicValidator func(ctx context.Context, user, pass string) (any, error)

// AuthBasic Basic Auth 认证中间件。
func AuthBasic(validator BasicValidator, realm string) Middleware {
	if realm == "" {
		realm = "Restricted"
	}
	authHeader := `Basic realm="` + realm + `"`

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, pass, ok := r.BasicAuth()
			if !ok {
				w.Header().Set("WWW-Authenticate", authHeader)
				Error(w, r, &HttpError{
					HttpCode: http.StatusUnauthorized,
					BizCode:  "Unauthorized",
					Msg:      "invalid basic format",
				})
				return
			}

			identity, err := validator(r.Context(), user, pass)
			if err != nil {
				w.Header().Set("WWW-Authenticate", authHeader)
				Error(w, r, &HttpError{
					HttpCode: http.StatusUnauthorized,
					BizCode:  "Unauthorized",
					Msg:      "invalid basic format",
				})
				return
			}

			ctx := context.WithValue(r.Context(), IdentityKey, identity)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
