package httpx

import (
	"net/http"
	"strings"
)

// Router wraps http.ServeMux to provide grouping and middleware capabilities.
type Router struct {
	mux         *http.ServeMux
	middlewares []func(http.Handler) http.Handler
}

// NewRouter creates a new Router instance.
func NewRouter() *Router {
	return &Router{
		mux: http.NewServeMux(),
	}
}

// With returns a new Router that shares the same underlying ServeMux but applies additional middleware.
// Handlers registered on the returned Router are registered on the original ServeMux.
func (r *Router) With(middleware ...func(http.Handler) http.Handler) *Router {
	mws := make([]func(http.Handler) http.Handler, len(r.middlewares)+len(middleware))
	copy(mws, r.middlewares)
	copy(mws[len(r.middlewares):], middleware)

	return &Router{
		mux:         r.mux,
		middlewares: mws,
	}
}

// Group creates a sub-router mounted at the specified pattern configuration.
// The pattern should be a path prefix like "/api/v1".
// Accessing the sub-router via the pattern will strip the pattern prefix.
func (r *Router) Group(pattern string, middleware ...func(http.Handler) http.Handler) *Router {
	// Normalize pattern to be a path prefix
	// If pattern is "POST /api", we probably just want "/api" for the mount?
	// But Group usually implies path prefix.

	// Remove trailing slash for consistency in stripping
	prefix := strings.TrimSuffix(pattern, "/")
	// Mount needs trailing slash to match subtree
	mountPattern := prefix + "/"

	subRouter := NewRouter()
	subRouter.middlewares = middleware

	// Register with StripPrefix
	// Use r.Handle so that parent middlewares are applied to this group
	r.Handle(mountPattern, http.StripPrefix(prefix, subRouter))

	return subRouter
}

// Handle registers the handler for the given pattern.
// It applies the router's middleware chain to the handler.
func (r *Router) Handle(pattern string, handler http.Handler) {
	// Apply middlewares in reverse order (Chain behavior: m1(m2(h)))
	final := handler
	for i := len(r.middlewares) - 1; i >= 0; i-- {
		final = r.middlewares[i](final)
	}
	r.mux.Handle(pattern, final)
}

// HandleFunc registers the handler function for the given pattern.
func (r *Router) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	r.Handle(pattern, http.HandlerFunc(handler))
}

// ServeHTTP satisfies http.Handler.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}
