# httpx: Minimalist HTTP Extensions for Go

[![Go Report Card](https://goreportcard.com/badge/github.com/oy3o/httpx)](https://goreportcard.com/report/github.com/oy3o/httpx)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

[中文](./README.zh.md) | [English](./README.md)

`httpx` is a set of HTTP extension tools based on Go 1.18+ generics. It is not a heavy web framework, but a **powerful enhancement** for the standard library `net/http`.

It aims to eliminate repetitive JSON decoding, parameter validation, and response encapsulation code in `http.Handler` through **Generics**, while maintaining full compatibility with the standard library.

## Core Concepts

1.  **Type Safety**: Leveraging generics to transform `func(w, r)` into strongly typed `func(ctx, *Req) (*Res, error)`.
2.  **Modern Conventions**: Uses **String Codes** (e.g., "OK", "INVALID_ARGUMENT") for business logic, distinct from HTTP Status Codes.
3.  **Performance at Scale**: Provides a `SelfValidatable` interface to bypass reflection validation; integrates `httpsnoop` for zero-overhead metric capture.
4.  **Production Ready**: Built-in protection against large bodies, memory limits for uploads, and graceful shutdown for long-lived connections.

## Installation

```bash
go get github.com/oy3o/httpx
```

## Quick Start

### 1. Define Request & Response

Use `json`, `form`, or `path` tags to define the structure.

```go
type LoginReq struct {
    // Supports Go 1.22+ PathValue
    TenantID string `path:"tenant_id"` 
    Username string `json:"username" validate:"required,min=3"`
    Password string `json:"password" validate:"required"`
}

type LoginRes struct {
    Token string `json:"token"`
}
```

### 2. Write Business Logic

No need to manipulate `http.ResponseWriter` and `*http.Request`.

```go
import "github.com/oy3o/httpx"

func LoginHandler(ctx context.Context, req *LoginReq) (*LoginRes, error) {
    if req.Username == "admin" && req.Password == "123456" {
        return &LoginRes{Token: "abc-123"}, nil
    }
    
    // Return an error. httpx automatically maps HTTP Status to a Business Code.
    // E.g., 401 -> "UNAUTHORIZED"
    return nil, httpx.ErrUnauthorized
}
```

### 3. Register Routes
Use `httpx.NewRouter` for a better routing experience with Group support, fully compatible with standard `http.ServeMux`.Use `httpx.NewHandler` to convert business functions into standard `http.Handler`s. Use `httpx.NewStreamHandler` to convert streamable business functions into standard `http.Handler`s.

```go
func main() {
    // 1. Create a Router (wraps http.ServeMux)
    r := httpx.NewRouter()

    // 2. Register Routes
    r.Handle("POST /login", httpx.NewHandler(LoginHandler))

    // 3. Use Groups
    api := r.Group("/api/v1")
    
    // 4. Apply Middleware to Group
    // api.Use(httpx.AuthMiddleware) - Coming soon, currently via Group(pattern, middlewares...)
    admin := r.Group("/admin", AdminAuthMiddleware)
    admin.Handle("DELETE /users/{id}", httpx.NewHandler(DeleteUser))

    // 5. Add Global Middleware
    handler := httpx.Chain(r, 
        httpx.Recovery(nil),
        httpx.Logger(nil),
    )

    http.ListenAndServe(":8080", handler)
}
```

## Core Features

### 1. Cooperative Binding

`httpx.Bind` automatically aggregates data from multiple sources:
*   **Path**: Go 1.22 `r.PathValue` (Tag: `path`).
*   **Query**: URL Query parameters (Tag: `form`).
*   **Body**: JSON or Form/Multipart based on `Content-Type`.
*   **Priority**: Path > Body > Query.

### 2. Hybrid Validation (High Performance)

*   **Reflection Mode**: Use struct tags (`validate:"required"`). fast for development.
*   **Interface Mode**: Implement `SelfValidatable`. **Zero reflection overhead**, ideal for hot paths.

```go
// Fast Validation: httpx calls this method first, skipping reflection
func (r *LoginReq) Validate(ctx context.Context) error {
    if len(r.Username) < 3 {
        return errors.New("username too short")
    }
    return nil
}
```

### 3. Semantic Error Handling & Trace Injection

Separates **HTTP Status** from **Business Code**. Automatically injects `X-Trace-ID` into headers and the response body if a Trace provider is configured.

```json
{
    "code": "UNAUTHORIZED",
    "message": "Invalid credentials",
    "trace_id": "a1b2c3d4"
}
```

### 4. Safety & Protection

*   **`WithMaxBodySize(bytes)`**: Limits the request body size. Returns `413 Entity Too Large` if exceeded.
*   **`WithMultipartLimit(bytes)`**: Limits memory usage during file uploads. Excess data spills to disk.

### 5. Smart Cookie Protection (Auto Armor)

`httpx` forces security best practices for Cookies by default, but remains developer-friendly.

*   **Prefix Awareness**: Automatically adds `__Host-` or `__Secure-` prefixes to cookies when possible, providing browser-level protection against **Cookie Tossing** attacks.
*   **Priority Probing**: When reading cookies, it prioritizes the secure variants (`__Host-name` > `__Secure-name` > `name`), ensuring that even if an attacker plants a non-secure cookie, your app reads the secure one.
*   **Nuke Strategy**: `DelCookie` performs a saturation attack, attempting to delete all possible variants of a cookie to ensure it's truly gone.

```go
// Automatically becomes "__Host-session_id" if Secure=true (default) and Path="/"
httpx.SetCookie(w, "session_id", "secret", httpx.WithCookieTTL(24 * time.Hour))

// Safely reads "__Host-session_id" if it exists, ignoring insecure "session_id"
val, err := httpx.GetCookie(r, "session_id")
```

### 6. Custom Response Control (Responder)

The `httpx.Responder` interface allows the business layer to take full control of response writing. This is useful for **redirects**, **file downloads**, or **content negotiation** (e.g., supporting both JSON API and Form submission).

```go
// 1. Define Response struct and implement Responder
type LoginRes struct {
    Token    string
    ReturnTo string
}

func (res *LoginRes) WriteResponse(w http.ResponseWriter, r *http.Request) {
    // Example: Return JSON or Redirect based on Accept header
    if r.Header.Get("Accept") == "application/json" {
        json.NewEncoder(w).Encode(res)
        return
    }
    http.Redirect(w, r, res.ReturnTo, http.StatusFound)
}

// 2. Register using NewResponder
r.Handle("POST /login", httpx.NewResponder(LoginHandler))
```

Additionally, httpx provides built-in primitives:

*   `httpx.Redirect{URL, Code}`: Pure redirect.
*   `httpx.RawBytes{Body, ContentType}`: Write raw bytes.
*   `httpx.NoContent{}`: Returns 204 No Content.

### 7. Middleware Ecosystem

| Middleware | Description |
| :--- | :--- |
| `Chain` | Composes multiple middleware into an onion model. |
| `Recovery` | Captures Panics to prevent service crashes; supports custom Hooks. |
| `Logger` | Based on **httpsnoop**, accurately records status codes and latency. |
| `SecurityHeaders` | Adds `X-Frame-Options`, `X-Content-Type-Options`, `X-XSS-Protection`, etc. |
| `CORS` | Flexible Cross-Origin Resource Sharing configuration. |
| `RateLimit` | Rate limiting interface integration. |
| `Auth` | **Flexible Auth Strategy**. Supports `AuthChain` (try multiple strategies), `FromHeader`, `FromCookie`, `FromQuery`. |
| `ShutdownManager` | Manages graceful shutdown for long-lived connections (WebSocket/SSE). |
| `Router` | Enhanced `ServeMux` with `Group` support and Method+Path handling. |
| `ClientIP` | Middleware to extract real client IP with **Trusted Proxy** support (CIDR). |

### Advanced: Graceful Shutdown for Long Connections

Standard `http.Server.Shutdown` does not terminate hijacked connections (like WebSockets) immediately. `httpx.ShutdownManager` solves this.

```go
// 1. Create Manager
mgr := httpx.NewShutdownManager()

// 2. Wrap Handler
mux.Handle("/ws", mgr.Middleware(websocketHandler))

// 3. Inside Handler
func websocketHandler(w http.ResponseWriter, r *http.Request) {
    // Register cleanup callback
    httpx.RegisterOnShutdown(r.Context(), func() {
        conn.WriteMessage(websocket.CloseMessage, ...)
        conn.Close()
    })
    // ... loop ...
}

// 4. On Server Shutdown
mgr.Shutdown(ctx) // Triggers all registered callbacks
```