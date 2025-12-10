package httpx

import (
	"context"
	"net"
	"net/http"
	"strings"
)

// clientIPKey is the context key for the client IP.
type clientIPKey struct{}

// NewClientIPMiddleware creates a middleware that extracts the real client IP.
// trustedProxiesCIDR is a list of CIDR strings (e.g., "10.0.0.0/8", "127.0.0.1/32").
// If trustedProxiesCIDR is nil or empty, it defaults to trusting NO proxies (only RemoteAddr) for security,
// OR you can decide to trust all if you explicitly pass specific value (not implemented here for safety).
// NOTE: To trust all (e.g. dev mode), pass "0.0.0.0/0".
func NewClientIPMiddleware(trustedProxiesCIDR []string) func(http.Handler) http.Handler {
	var trustedProxies []*net.IPNet
	for _, cidr := range trustedProxiesCIDR {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err == nil {
			trustedProxies = append(trustedProxies, ipNet)
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var ip string

			// Get immediate peer IP (RemoteAddr)
			remoteIPStr, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				remoteIPStr = r.RemoteAddr
			}
			remoteIP := net.ParseIP(remoteIPStr)

			// Check if the immediate peer is a trusted proxy
			isTrusted := false
			if remoteIP != nil {
				for _, proxyNet := range trustedProxies {
					if proxyNet.Contains(remoteIP) {
						isTrusted = true
						break
					}
				}
			}

			if isTrusted {
				// 1. Check X-Forwarded-For
				if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
					// XFF: client, proxy1, proxy2
					// We trust the chain provided by our trusted proxy.
					// Real client is usually the first one.
					if idx := strings.Index(xff, ","); idx != -1 {
						ip = strings.TrimSpace(xff[:idx])
					} else {
						ip = strings.TrimSpace(xff)
					}
				}

				// 2. Check X-Real-IP
				if ip == "" {
					if xri := r.Header.Get("X-Real-IP"); xri != "" {
						ip = strings.TrimSpace(xri)
					}
				}
			}

			// 3. Fallback to RemoteAddr (Untrusted peer, or headers empty)
			if ip == "" {
				ip = remoteIPStr
			}

			// Clean up
			ip = strings.TrimPrefix(ip, "[")
			ip = strings.TrimSuffix(ip, "]")

			ctx := context.WithValue(r.Context(), clientIPKey{}, ip)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ClientIP retrieves the client IP from the context.
// It returns an empty string if the middleware was not applied.
func ClientIP(ctx context.Context) string {
	if ip, ok := ctx.Value(clientIPKey{}).(string); ok {
		return ip
	}
	return ""
}
