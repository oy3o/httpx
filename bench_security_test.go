package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func BenchmarkSecurityHeaders(b *testing.B) {
	mw := SecurityHeaders(SecurityConfig{
		HSTSMaxAgeSeconds:     31536000,
		HSTSIncludeSubdomains: true,
		CSP:                   "default-src 'self'",
	})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest("GET", "/", nil)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}
}
