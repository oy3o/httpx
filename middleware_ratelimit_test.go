package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

type mockLimiter struct {
	allowed bool
}

func (m *mockLimiter) Allow(r *http.Request) bool { return m.allowed }

func TestRateLimit(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})

	t.Run("Allowed", func(t *testing.T) {
		mw := RateLimit(&mockLimiter{allowed: true})
		w := httptest.NewRecorder()
		mw(handler).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		assert.Equal(t, 200, w.Code)
	})

	t.Run("Blocked", func(t *testing.T) {
		mw := RateLimit(&mockLimiter{allowed: false})
		w := httptest.NewRecorder()
		mw(handler).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		assert.Equal(t, 429, w.Code)
		assert.Contains(t, w.Body.String(), CodeTooManyRequests)
	})
}
