package httpx

import (
	"context"
	"net/http"
	"sync"

	"github.com/puzpuzpuz/xsync/v4"
	"github.com/rs/xid"
)

type shutdownKey struct{}

// ShutdownManager 管理活跃连接的优雅关闭
type ShutdownManager struct {
	// activeHandlers 存储所有活跃请求的关闭回调
	// Key: RequestID (由 xid 生成), Value: []func()
	activeHandlers *xsync.Map[string, *sessionState]
	// closed 标记是否已关闭，防止重复关闭
	closed bool
	mu     sync.Mutex
}

// NewShutdownManager 创建一个新的关闭管理器
func NewShutdownManager() *ShutdownManager {
	return &ShutdownManager{
		activeHandlers: xsync.NewMap[string, *sessionState](),
	}
}

// RegisterOnShutdown 在当前请求的上下文中注册一个关闭回调。
// 当 ShutdownManager.Shutdown 被调用时，这些回调会被并发执行。
// 适用于 WebSocket、SSE 等需要发送协议级关闭信号（如 Close Frame）的场景。
func RegisterOnShutdown(ctx context.Context, fn func()) {
	// 从 Context 中获取 Session ID 和 Manager
	sessionID, ok := ctx.Value(shutdownKey{}).(string)
	if !ok || sessionID == "" {
		return // 上下文中没有管理器，忽略
	}

	if adder, ok := ctx.Value(shutdownAdderKey{}).(func(func())); ok {
		adder(fn)
	}
}

type shutdownAdderKey struct{}

// Middleware 返回一个中间件，用于跟踪请求生命周期并注入关闭支持。
func (m *ShutdownManager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. 生成会话 ID
		sessionID := xid.New().String()

		// 2. 准备回调存储 (使用 slice 存储该请求的所有回调)
		// 我们使用一个独立的锁来保护这个 slice，或者直接利用 Manager 的 map
		// 这里为了简单，我们在 Manager 的 map 中存一个 *sync.Mutex 保护的结构
		session := &sessionState{}
		m.activeHandlers.Store(sessionID, session)

		// 3. 注入 "注册器" 到 Context
		// 用户调用 RegisterOnShutdown 时，实际上是往这个 session 对象里 append 函数
		adder := func(fn func()) {
			session.mu.Lock()
			defer session.mu.Unlock()
			session.callbacks = append(session.callbacks, fn)
		}
		ctx := context.WithValue(r.Context(), shutdownAdderKey{}, adder)
		// 同时也存一下 ID，方便调试（可选）
		ctx = context.WithValue(ctx, shutdownKey{}, sessionID)

		// 4. 确保请求结束时清理
		defer m.activeHandlers.Delete(sessionID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Shutdown 触发优雅关闭。
// 它会并发调用所有活跃请求注册的回调函数。
// 此方法会阻塞直到所有回调执行完毕或 ctx 超时。
func (m *ShutdownManager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.mu.Unlock()

	var wg sync.WaitGroup

	// 遍历所有活跃会话
	m.activeHandlers.Range(func(key string, session *sessionState) bool {
		// 为每个会话启动一个 goroutine 处理其回调
		wg.Add(1)
		go func() {
			defer wg.Done()
			session.mu.Lock()
			callbacks := session.callbacks
			session.mu.Unlock()

			// 倒序执行回调（类似 defer）
			for i := len(callbacks) - 1; i >= 0; i-- {
				// 检查总超时
				if ctx.Err() != nil {
					return
				}
				// 执行回调 (例如发送 WS Close Frame)
				// 注意：回调本身不应阻塞太久
				callbacks[i]()
			}
		}()
		return true
	})

	// 等待所有回调执行完成 或 上下文超时
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type sessionState struct {
	mu        sync.Mutex
	callbacks []func()
}
