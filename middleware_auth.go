package httpx

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// ErrNoCredentials 是一个特定的信号错误。
// 当策略返回此错误时，AuthChain 会忽略它并尝试下一个策略。
var ErrNoCredentials = errors.New("no credentials found")

type (
	IdentityKey  struct{}
	AuthErrorKey struct{}
)

// GetIdentity 从 Context 中获取身份信息。
func GetIdentity(ctx context.Context) any {
	return ctx.Value(IdentityKey{})
}

func GetAuthError(ctx context.Context) error {
	return ctx.Value(AuthErrorKey{}).(error)
}

// AuthStrategy 定义从请求中提取身份的原子策略。
type AuthStrategy func(w http.ResponseWriter, r *http.Request) (any, error)

// Auth 通用的认证中间件构造器。
// 它负责通用的“管道工作”：调用策略 -> 处理错误 -> 注入Context -> 继续执行。
func Auth(strategy AuthStrategy, Errors ...ErrorFunc) Middleware {
	var errorFunc ErrorFunc
	if len(Errors) > 0 {
		errorFunc = Errors[0]
	} else {
		errorFunc = Error
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. 执行策略
			identity, err := strategy(w, r)
			// 如果没有返回一个 http 错误, 我们忽略它, 通过要求身份的中间件进行拦截, 这里仅尝试识别身份
			if err != nil {
				if _, ok := err.(*HttpError); ok {
					// 错误处理委托给 httpx.Error
					errorFunc(w, r, err)
					return
				} else {
					// 传递给 challengeWith
					ctx := context.WithValue(r.Context(), AuthErrorKey{}, err)
					r = r.WithContext(ctx)
				}
			}

			// 2. 注入身份
			if identity != nil {
				ctx := context.WithValue(r.Context(), IdentityKey{}, identity)
				r = r.WithContext(ctx)
			}

			next.ServeHTTP(w, r)
		})
	}
}

// AuthRequired 认证中间件，要求必须提供身份。
func AuthRequired(challengeWith http.HandlerFunc) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if GetIdentity(r.Context()) == nil {
				challengeWith(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// AuthChain 职责链模式：按顺序尝试多种认证策略。
// 逻辑：
// 1. 如果策略成功 -> 立即返回身份。
// 2. 如果策略返回 ErrNoCredentials -> 继续尝试下一个。
// 3. 如果策略返回其他错误（如 Token 过期/签名错误） -> 立即终止并报错（不进行降级）。
// 4. 如果所有策略都未命中 -> 返回 ErrNoCredentials。
func AuthChain(strategies ...AuthStrategy) AuthStrategy {
	return func(w http.ResponseWriter, r *http.Request) (any, error) {
		for _, strategy := range strategies {
			identity, err := strategy(w, r)
			if err == nil {
				return identity, nil
			}
			// 只有在“未找到凭证”时才继续，验证失败是致命错误
			if !errors.Is(err, ErrNoCredentials) {
				return nil, err
			}
		}
		return nil, ErrNoCredentials
	}
}

// WithAuthChallenge 装饰器：为 ErrNoCredentials 错误附加 Challenge Header。
// 当策略链彻底失败（即客户端完全未提供凭证）时，此装饰器将错误转换为
// 带有 WWW-Authenticate 的 401 错误。
func WithAuthChallenge(strategy AuthStrategy, headerKey, headerVal string) AuthStrategy {
	return func(w http.ResponseWriter, r *http.Request) (any, error) {
		identity, err := strategy(w, r)
		if err != nil {
			// 如果是“未提供凭证”，则告诉客户端应该提供什么
			if w != nil && errors.Is(err, ErrNoCredentials) {
				if headerKey == "" {
					headerKey = "WWW-Authenticate"
				}
				if headerVal == "" {
					headerVal = "Bearer realm=\"oidc\", error=\"invalid_token\""
				}
				w.Header().Set(headerKey, headerVal)

				return nil, &HttpError{
					HttpCode: http.StatusUnauthorized,
					BizCode:  "Unauthorized",
					Msg:      "authentication required",
				}
			}
			// 如果是具体的验证错误，通常策略内部已经设置了具体的 Challenge (例如 invalid_token)
			return nil, err
		}
		return identity, nil
	}
}

// ---------------------------------------------------------------------------
// 常用策略原子 (Strategy Primitives)
// ---------------------------------------------------------------------------

// FromHeader 创建一个从 Header 提取凭证的策略。
// scheme: 认证前缀，如 "Bearer", "Basic", "DPoP"。不区分大小写。
func FromHeader(scheme string, validator func(context.Context, string) (any, error)) AuthStrategy {
	prefix := scheme + " "
	return func(w http.ResponseWriter, r *http.Request) (any, error) {
		auth := r.Header.Get("Authorization")
		if len(auth) < len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
			return nil, ErrNoCredentials
		}
		return validator(r.Context(), auth[len(prefix):])
	}
}

// FromCookie 创建一个从 Cookie 提取凭证的策略。
// 它透明地使用 httpx.GetCookie，自动支持 __Host- 和 __Secure- 前缀防御。
func FromCookie(name string, validator func(context.Context, string) (any, error)) AuthStrategy {
	return func(w http.ResponseWriter, r *http.Request) (any, error) {
		// 这里是关键：我们不使用 r.Cookie(name)，而是使用智能的 GetCookie
		val, err := GetCookie(r, name)
		if err != nil {
			return nil, ErrNoCredentials
		}
		return validator(r.Context(), val)
	}
}

// FromQuery 创建一个从 URL Query 提取凭证的策略。
func FromQuery(param string, validator func(context.Context, string) (any, error)) AuthStrategy {
	return func(w http.ResponseWriter, r *http.Request) (any, error) {
		val := r.URL.Query().Get(param)
		if val == "" {
			return nil, ErrNoCredentials
		}
		return validator(r.Context(), val)
	}
}
