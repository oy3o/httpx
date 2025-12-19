package httpx

import (
	"net/http"
)

// Limiter 接口定义了限流器的行为。
// Allow 应该非阻塞地返回是否允许请求。
type Limiter interface {
	Allow(r *http.Request) bool
}

// RateLimit 返回一个限流中间件。
func RateLimit(limiter Limiter, Errors ...ErrorFunc) Middleware {
	var errorFunc ErrorFunc
	if len(Errors) > 0 {
		errorFunc = Errors[0]
	} else {
		errorFunc = Error
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow(r) {
				errorFunc(w, r, ErrTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
