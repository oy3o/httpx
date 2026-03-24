package httpx

import (
	"context"
	"net/http/httptest"
	"testing"
)

func BenchmarkHandlerWithTraceIDAndNoVarySearch(b *testing.B) {
    GetTraceID = func(ctx context.Context) string {
        return "trace-12345"
    }
    defer func() {
        GetTraceID = nil
    }()

    handler := NewHandler(func(ctx context.Context, req *struct{}) (string, error) {
        return "ok", nil
    })

    req := httptest.NewRequest("GET", "/", nil)
    b.ResetTimer()
    b.ReportAllocs()
    for i := 0; i < b.N; i++ {
        w := httptest.NewRecorder()
        handler(w, req)
    }
}
