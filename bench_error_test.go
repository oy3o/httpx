package httpx

import (
	"errors"
	"net/http/httptest"
	"testing"
)

func BenchmarkErrorResponse(b *testing.B) {
	err := errors.New("test error")
	req := httptest.NewRequest("GET", "/", nil)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		Error(w, req, err)
	}
}
