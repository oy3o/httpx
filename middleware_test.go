package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChain(t *testing.T) {
	// Create middleware that append strings to a header
	mw1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("X-Trace", "1")
			next.ServeHTTP(w, r)
		})
	}
	mw2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("X-Trace", "2")
			next.ServeHTTP(w, r)
		})
	}

	finalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Chain them: 1 -> 2 -> Final
	chain := Chain(finalHandler, mw1, mw2)

	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	chain.ServeHTTP(w, r)

	// Verify execution order (Middleware applied in reverse order in Chain func to maintain 1->2 flow)
	// If implementation is: for i := len(mws) - 1; i >= 0; i-- { h = mws[i](h) }
	// Then Chain(h, mw1, mw2) becomes mw1(mw2(h))
	// So mw1 runs first, then mw2.

	// Verify headers
	// Header().Add appends.
	trace := w.Header().Values("X-Trace")
	assert.Equal(t, []string{"1", "2"}, trace)
}

func TestRecovery(t *testing.T) {
	// Handler that panics
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("oops")
	})

	loggerCalled := false
	logger := func(ctx context.Context, err error) {
		loggerCalled = true
		assert.Equal(t, "panic: oops", err.Error())
	}

	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	// Wrap with Recovery
	h := Recovery(WithHook(logger))(panicHandler)

	// Should not panic the test
	assert.NotPanics(t, func() {
		h.ServeHTTP(w, r)
	})

	assert.True(t, loggerCalled)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
