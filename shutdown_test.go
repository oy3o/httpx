package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestShutdownManager_Lifecycle(t *testing.T) {
	mgr := NewShutdownManager()
	var mu sync.Mutex
	var closedResources []string

	// 模拟一个 WebSocket Handler
	wsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 模拟建立连接...
		connID := r.URL.Query().Get("id")

		// 注册关闭回调
		RegisterOnShutdown(r.Context(), func() {
			mu.Lock()
			closedResources = append(closedResources, connID)
			mu.Unlock()
		})

		// 模拟长连接阻塞，直到 context 被 cancel (通常由 http.Server Shutdown 触发)
		// 或者我们这里用一个 channel 模拟等待
		<-r.Context().Done()
	})

	// 包装中间件
	handler := mgr.Middleware(wsHandler)
	server := httptest.NewServer(handler)
	defer server.Close()

	// 启动两个长连接客户端
	var wg sync.WaitGroup
	wg.Add(2)

	// Client 1
	go func() {
		defer wg.Done()
		// 使用带超时的 client 防止测试死锁
		client := server.Client()
		client.Timeout = 2 * time.Second // Client timeout
		_, _ = client.Get(server.URL + "?id=conn1")
	}()

	// Client 2
	go func() {
		defer wg.Done()
		client := server.Client()
		client.Timeout = 2 * time.Second
		_, _ = client.Get(server.URL + "?id=conn2")
	}()

	// 等待连接建立 (简单 sleep)
	time.Sleep(100 * time.Millisecond)

	// 触发 Shutdown
	// 这应该调用所有注册的回调
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	err := mgr.Shutdown(ctx)
	assert.NoError(t, err)

	// 验证回调是否执行
	mu.Lock()
	assert.ElementsMatch(t, []string{"conn1", "conn2"}, closedResources)
	mu.Unlock()

	// 注意：httptest.Server 不支持通过 context cancel 来断开 active requests，
	// 所以上面的 Get 请求会超时退出，这符合预期。
	// mgr.Shutdown 只是负责执行回调，不负责关闭 http 连接 (那是 http.Server 的工作)。
	wg.Wait()
}

func TestRegisterOnShutdown_NoContext(t *testing.T) {
	// 测试在没有中间件的情况下调用，不应 panic
	assert.NotPanics(t, func() {
		RegisterOnShutdown(context.Background(), func() {})
	})
}
