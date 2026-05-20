package httpx

import (
	"context"
	"net/http"
	"testing"
)

type dummyResponseWriter struct{}
func (d dummyResponseWriter) Header() http.Header { return http.Header{} }
func (d dummyResponseWriter) Write([]byte) (int, error) { return 0, nil }
func (d dummyResponseWriter) WriteHeader(statusCode int) {}

func BenchmarkClientIPNoProxies(b *testing.B) {
	mw := NewClientIPMiddleware(nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req, _ := http.NewRequestWithContext(context.Background(), "GET", "/", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := dummyResponseWriter{}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		handler.ServeHTTP(w, req)
	}
}

func BenchmarkClientIPWithProxies(b *testing.B) {
	mw := NewClientIPMiddleware([]string{"10.0.0.0/8"})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req, _ := http.NewRequestWithContext(context.Background(), "GET", "/", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := dummyResponseWriter{}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		handler.ServeHTTP(w, req)
	}
}
