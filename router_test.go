package httpx

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRouter_Handle(t *testing.T) {
	router := NewRouter()
	router.HandleFunc("GET /hello", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello, World!"))
	})

	req := httptest.NewRequest("GET", "/hello", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Hello, World!", w.Body.String())
}

func TestRouter_Group(t *testing.T) {
	router := NewRouter()
	api := router.Group("/api/v1")

	api.HandleFunc("GET /users", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Users List"))
	})

	req := httptest.NewRequest("GET", "/api/v1/users", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Users List", w.Body.String())
}

func TestRouter_Middleware(t *testing.T) {
	middleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Middleware", "Applied")
			next.ServeHTTP(w, r)
		})
	}

	router := NewRouter()
	group := router.Group("/api", middleware)

	group.HandleFunc("GET /test", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Applied", w.Header().Get("X-Middleware"))
	assert.Equal(t, "OK", w.Body.String())
}

func TestRouter_GroupNesting(t *testing.T) {
	router := NewRouter()
	v1 := router.Group("/v1")
	users := v1.Group("/users")

	users.HandleFunc("GET /list", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("User List"))
	})

	req := httptest.NewRequest("GET", "/v1/users/list", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "User List", w.Body.String())
}

func TestRouter_MiddlewareOrder(t *testing.T) {
	mw1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("1"))
			next.ServeHTTP(w, r)
			w.Write([]byte("1"))
		})
	}
	mw2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("2"))
			next.ServeHTTP(w, r)
			w.Write([]byte("2"))
		})
	}

	router := NewRouter()
	// Middleware applied to group should execute in order
	group := router.Group("/api", mw1, mw2)

	group.HandleFunc("GET /test", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("H"))
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Expect 1 -> 2 -> H -> 2 -> 1
	assert.Equal(t, "12H21", w.Body.String())
}

func TestRouter_Handle_WithPatternVariables(t *testing.T) {
	// Determine the Go version in environment or check Go 1.22 capabilities
	// For simplicity, we assume we are in the dev environment supporting Go 1.22 path values.

	router := NewRouter()
	router.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		fmt.Fprintf(w, "User ID: %s", id)
	})

	req := httptest.NewRequest("GET", "/users/123", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "User ID: 123", w.Body.String())
}

func TestRouter_Group_WithPatternVariables(t *testing.T) {
	router := NewRouter()
	api := router.Group("/api")

	// When using strip prefix (Group behavior), the pattern matching for path values
	// applies to the remaining path in the SUB-router.
	// E.g. /api/users/123 -> subrouter sees /users/123

	api.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		fmt.Fprintf(w, "User ID: %s", id)
	})

	req := httptest.NewRequest("GET", "/api/users/456", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "User ID: 456", w.Body.String())
}

func TestRouter_Handle_MethodMatching(t *testing.T) {
	router := NewRouter()
	router.HandleFunc("POST /submit", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Submitted"))
	})

	// GET should 405
	reqGet := httptest.NewRequest("GET", "/submit", nil)
	wGet := httptest.NewRecorder()
	router.ServeHTTP(wGet, reqGet)
	assert.Equal(t, http.StatusMethodNotAllowed, wGet.Code)

	// POST should 200
	reqPost := httptest.NewRequest("POST", "/submit", nil)
	wPost := httptest.NewRecorder()
	router.ServeHTTP(wPost, reqPost)
	assert.Equal(t, http.StatusOK, wPost.Code)
	assert.Equal(t, "Submitted", wPost.Body.String())
}
