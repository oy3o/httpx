# httpx: Minimalist HTTP Extensions for Go

[![Go Report Card](https://goreportcard.com/badge/github.com/oy3o/httpx)](https://goreportcard.com/report/github.com/oy3o/httpx)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

[中文](./README.zh.md) | [English](./README.md)

`httpx` 是一套基于 Go 1.18+ 泛型的 HTTP 扩展工具包。它不是一个繁重的 Web 框架，而是标准库 `net/http` 的**强力补丁**。

它旨在通过 **泛型（Generics）** 消除 `http.Handler` 中重复的 JSON 解码、参数验证和响应封装代码，同时保持与标准库的完全兼容。

## 核心理念

1.  **类型安全 (Type Safety)**: 利用泛型将 `func(w, r)` 转换为强类型的 `func(ctx, *Req) (*Res, error)`。
2.  **现代约定 (Modern Conventions)**: 采用 **字符串业务码** (如 "OK", "INVALID_ARGUMENT")，与 HTTP 状态码分离，对前端更友好。
3.  **极致性能 (Performance at Scale)**: 提供 `SelfValidatable` 接口以绕过反射校验；集成 `httpsnoop` 实现零开销的指标捕获。
4.  **生产级防护**: 内置大包体拦截、文件上传内存限制以及长连接的优雅退出机制。

## 安装

```bash
go get github.com/oy3o/httpx
```

## 快速开始

### 1. 定义请求与响应

支持使用 `json`, `form`, `path` 等标签。

```go
type LoginReq struct {
    // 支持 Go 1.22+ PathValue
    TenantID string `path:"tenant_id"`
    Username string `json:"username" validate:"required,min=3"`
    Password string `json:"password" validate:"required"`
}

type LoginRes struct {
    Token string `json:"token"`
}
```

### 2. 编写业务逻辑

不再需要操作 `http.ResponseWriter` 和 `*http.Request`。

```go
import "github.com/oy3o/httpx"

func LoginHandler(ctx context.Context, req *LoginReq) (*LoginRes, error) {
    if req.Username == "admin" && req.Password == "123456" {
        return &LoginRes{Token: "abc-123"}, nil
    }
    
    // 返回错误，httpx 会自动推断业务码
    return nil, httpx.ErrUnauthorized
}
```

### 3. 注册路由
使用 `httpx.NewRouter` 获得更好的路由体验（支持 Group），同时保持与标准库 `http.ServeMux` 的完全兼容。使用 `httpx.NewHandler` 将业务函数转换为标准的 `http.Handler`。使用 `httpx.NewStreamHandler` 将流式响应业务函数转换为标准的 `http.Handler`。

```go
func main() {
    // 1. 创建 Router (封装了 http.ServeMux)
    r := httpx.NewRouter()

    // 2. 注册路由
    r.Handle("POST /login", httpx.NewHandler(LoginHandler))

    // 3. 使用路由组 (Group)
    api := r.Group("/api/v1")
    
    // 4. 为组添加中间件
    // admin组的所有请求都会经过 AdminAuthMiddleware
    // 使用 With 可以在不改变路径的情况下添加中间件
    // 例如: v1.With(AuthMiddleware).Handle(...)
    admin := r.Group("/admin", AdminAuthMiddleware)
    admin.Handle("DELETE /users/{id}", httpx.NewHandler(DeleteUser))

    // 5. 添加全局中间件
    handler := httpx.Chain(r, 
        httpx.Recovery(nil),
        httpx.Logger(nil),
    )

    http.ListenAndServe(":8080", handler)
}
```

## 核心特性

### 1. 协同参数绑定 (Cooperative Binding)

`httpx.Bind` 自动聚合多种数据源：
*   **Path**: 适配 Go 1.22 `r.PathValue` (Tag: `path`).
*   **Query**: URL Query 参数 (Tag: `form`).
*   **Body**: 根据 `Content-Type` 自动选择 JSON 或 Form/Multipart 解析。
*   **优先级**: Path > Body > Query。

### 2. 双模验证 (Hybrid Validation)

*   **反射模式**: 使用 struct tag (`validate:"required"`)。开发快，但有反射开销。
*   **接口模式**: 实现 `SelfValidatable` 接口。**完全无反射**，适合热点接口。

```go
// 极速验证：httpx 会优先调用此方法，跳过反射
func (r *LoginReq) Validate(ctx context.Context) error {
    if len(r.Username) < 3 {
        return errors.New("username too short")
    }
    return nil
}
```

### 3. 语义化错误与 Trace 注入

分离 **传输状态** (HTTP Status) 与 **业务状态** (String Code)。若配置了 Trace Provider，会自动在 Header 和 Response Body 中注入 `trace_id`。

```json
{
    "code": "UNAUTHORIZED",
    "message": "Invalid credentials",
    "trace_id": "a1b2c3d4"
}
```

### 4. 安全与防护 (Safety)

*   **`WithMaxBodySize(bytes)`**: 限制 Request Body 大小。超过限制返回 `413 Entity Too Large`，并切断连接，防止内存耗尽攻击。
*   **`WithMultipartLimit(bytes)`**: 限制文件上传时的内存占用，超限部分自动落盘。

### 5. 智能 Cookie 防护 (Auto Armor)

`httpx` 默认强制使用 Cookie 安全最佳实践，自动屏蔽各类 Cookie 攻击，同时对开发者保持透明。

*   **前缀感知 (Prefix Awareness)**: 自动为 Cookie 添加 `__Host-` 或 `__Secure-` 前缀，提供浏览器级的 **Cookie Tossing** 攻击防御。
*   **优先级探测 (Priority Probing)**: 读取 Cookie 时，优先读取安全变体 (`__Host-name` > `__Secure-name` > `name`)，确保即使攻击者植入了不安全的同名 Cookie，系统也会优先读取安全的那个。
*   **饱和式清除 (Nuke Strategy)**: `DelCookie` 会尝试删除所有可能的变体，确保 Cookie 被彻底清除。

```go
// 如果 Secure=true (默认) 且 Path="/"，自动转换为 "__Host-session_id"
httpx.SetCookie(w, "session_id", "secret", httpx.WithCookieTTL(24 * time.Hour))

// 安全地读取 "__Host-session_id"，自动安全降级但不妥协
val, err := httpx.GetCookie(r, "session_id")
```

### 6. 自定义响应 (Responder)

`httpx.Responder` 接口允许业务层完全接管响应写入逻辑，适用于 **重定向**、**文件下载** 或 **动态内容协商**（如同时支持 JSON API 和 Form 表单提交）。

```go
// 1. 定义响应结构并实现 Responder 接口
type LoginRes struct {
    Token    string
    ReturnTo string // 跳转地址
}

func (res *LoginRes) WriteResponse(w http.ResponseWriter, r *http.Request) {
    // 示例：根据客户端类型决定返回 JSON 还是重定向
    if r.Header.Get("Accept") == "application/json" {
        json.NewEncoder(w).Encode(res)
        return
    }
    http.Redirect(w, r, res.ReturnTo, http.StatusFound)
}

// 2. 注册时使用 NewResponder
r.Handle("POST /login", httpx.NewResponder(LoginHandler))
```

此外，httpx 提供了一些内置的原语：

*   `httpx.Redirect{URL, Code}`: 纯重定向。
*   `httpx.RawBytes{Body, ContentType}`: 直接写入字节流。
*   `httpx.NoContent{}`: 返回 204 No Content。

### 7. 中间件生态

| 中间件 | 说明 |
| :--- | :--- |
| `Chain` | 洋葱模型组合中间件。 |
| `Recovery` | 捕获 Panic 防止服务崩溃，支持自定义 Hook。 |
| `Logger` | 基于 **httpsnoop**，精准记录状态码和耗时。 |
| `SecurityHeaders`| 注入 `X-Frame-Options`, `X-XSS-Protection` 等安全头。 |
| `CORS` | 灵活的跨域配置。 |
| `RateLimit` | 限流接口集成。 |
| `Auth` | **灵活的认证策略**。支持 `AuthChain` (多策略尝试), `FromHeader`, `FromCookie`, `FromQuery`。 |
| `ShutdownManager` | **长连接优雅关闭管理器** (适用于 WebSocket/SSE)。 |
| `Router` | 增强版 `ServeMux`，支持 `Group` 路由组和 Method+Path 绑定。 |
| `ClientIP` | 提取真实客户端 IP 的中间件（支持 **可信代理 CIDR** 配置）。 |

### 进阶：长连接优雅关闭

标准的 `http.Server.Shutdown` 不会立即关闭被 Hijack 的连接（如 WebSocket）。`httpx.ShutdownManager` 解决了这个问题。

```go
// 1. 创建管理器
mgr := httpx.NewShutdownManager()

// 2. 包裹 Handler
mux.Handle("/ws", mgr.Middleware(websocketHandler))

// 3. 在 Handler 内部注册清理逻辑
func websocketHandler(w http.ResponseWriter, r *http.Request) {
    httpx.RegisterOnShutdown(r.Context(), func() {
        // 发送 Close Frame 并关闭连接
        conn.WriteMessage(websocket.CloseMessage, ...)
        conn.Close()
    })
    // ... 业务循环 ...
}

// 4. 服务停止时调用
mgr.Shutdown(ctx) // 并发执行所有注册的清理回调
```

