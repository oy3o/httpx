package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultCORS(t *testing.T) {
	mw := DefaultCORS()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	// DefaultCORS 开启了 AllowCredentials: true
	// 因此 Access-Control-Allow-Origin 必须是具体的 Origin，而不能是 *
	// 尽管配置的 AllowedOrigins 是 ["*"]，但在实现中会反射 Request 的 Origin
	requestOrigin := "https://any.com"

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Origin", requestOrigin)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	assert.Equal(t, requestOrigin, w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
}

func TestCORS_Detailed(t *testing.T) {
	mw := CORS(CORSOptions{
		AllowedOrigins:   []string{"http://example.com"},
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
		ExposedHeaders:   []string{"X-Trace-ID"},
	})

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	t.Run("Origin Match", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Origin", "http://example.com")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		assert.Equal(t, "http://example.com", w.Header().Get("Access-Control-Allow-Origin"))
		assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
	})

	t.Run("Origin Mismatch", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Origin", "http://evil.com")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("Preflight Options", func(t *testing.T) {
		r := httptest.NewRequest("OPTIONS", "/", nil)
		r.Header.Set("Origin", "http://example.com")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Equal(t, "GET, POST", w.Header().Get("Access-Control-Allow-Methods"))
		assert.Equal(t, "Content-Type", w.Header().Get("Access-Control-Allow-Headers"))
		assert.Equal(t, "X-Trace-ID", w.Header().Get("Access-Control-Expose-Headers"))
	})
}
