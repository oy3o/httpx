package httpx

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bytedance/sonic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock Structures ---

// TestReqReflect 用于测试 struct tag 校验
type TestReqReflect struct {
	Name string `json:"name" validate:"required"`
	Age  int    `json:"age" validate:"gte=0"`
}

// TestReqInterface 用于测试接口方法校验
type TestReqInterface struct {
	Name string `json:"name"`
}

// Implement SelfValidatable
func (r *TestReqInterface) Validate(ctx context.Context) error {
	if r.Name == "invalid_manual" {
		return errors.New("manual validation failed")
	}
	return nil
}

type TestRes struct {
	ID string `json:"id"`
}

type TestReqQuery struct {
	Q    string `form:"q"`
	Sort string `form:"sort"`
	Page int    `form:"page" no_vary:"true"` // 假设我们有这个 tag? 不，目前只收集所有 form keys
}

type TestReqEmpty struct{}

// --- Test Cases ---

func TestNewHandler_Success(t *testing.T) {
	// Logic: Successful request
	handlerFunc := func(ctx context.Context, req *TestReqReflect) (*TestRes, error) {
		assert.Equal(t, "alice", req.Name)
		return &TestRes{ID: "123"}, nil
	}

	h := NewHandler(handlerFunc)

	// Build Request
	body := `{"name": "alice", "age": 20}`
	r := httptest.NewRequest("POST", "/", bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json") // 显式设置 Header
	w := httptest.NewRecorder()

	// Execute
	h.ServeHTTP(w, r)

	// Assert
	assert.Equal(t, http.StatusOK, w.Code)

	var resp Response[TestRes]
	err := sonic.ConfigDefault.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "OK", resp.Code)
	assert.Equal(t, "success", resp.Message)
	assert.Equal(t, "123", resp.Data.ID)
}

func TestNewHandler_ValidationError_Reflection(t *testing.T) {
	// Logic: Should fail on "required" tag
	// 使用 TestReqReflect，它没有实现接口，所以会走反射校验
	handlerFunc := func(ctx context.Context, req *TestReqReflect) (*TestRes, error) {
		return nil, nil
	}

	h := NewHandler(handlerFunc)

	// Missing "name"
	body := `{"age": 20}`
	r := httptest.NewRequest("POST", "/", bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	// 错误信息来自 validator 库
	assert.Contains(t, w.Body.String(), "Name")
	assert.Contains(t, w.Body.String(), "required")
}

func TestNewHandler_ValidationError_Interface(t *testing.T) {
	// Logic: Should fail on manual Validate() method
	// 使用 TestReqInterface，它实现了接口
	handlerFunc := func(ctx context.Context, req *TestReqInterface) (*TestRes, error) {
		return nil, nil
	}

	h := NewHandler(handlerFunc)

	// Trigger manual validation error
	body := `{"name": "invalid_manual"}`
	r := httptest.NewRequest("POST", "/", bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, r)

	// 接口验证错误默认由 Validate 返回 err，NewHandler 捕获后默认处理为 400
	// 除非 err 实现了 ErrorCoder 接口。
	// 这里的 errors.New 返回的是普通 error，所以是 400。
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "manual validation failed")
}

func TestNewHandler_NoEnvelope(t *testing.T) {
	handlerFunc := func(ctx context.Context, req *TestReqReflect) (*TestRes, error) {
		return &TestRes{ID: "raw_123"}, nil
	}

	h := NewHandler(handlerFunc, NoEnvelope())

	body := `{"name": "alice", "age": 20}`
	r := httptest.NewRequest("POST", "/", bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, r)

	assert.Equal(t, http.StatusOK, w.Code)

	var res TestRes
	err := sonic.ConfigDefault.NewDecoder(w.Body).Decode(&res)
	require.NoError(t, err)
	assert.Equal(t, "raw_123", res.ID)
}

func TestNewHandler_BusinessError(t *testing.T) {
	handlerFunc := func(ctx context.Context, req *TestReqReflect) (*TestRes, error) {
		return nil, &HttpError{HttpCode: 409, Msg: "Conflict"}
	}

	h := NewHandler(handlerFunc)

	body := `{"name": "alice", "age": 20}`
	r := httptest.NewRequest("POST", "/", bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, r)

	assert.Equal(t, 409, w.Code)
	assert.Contains(t, w.Body.String(), "Conflict")
}

// 验证 JSON 绑定默认不允许未知字段
func TestJsonBinder_UnknownFields(t *testing.T) {
	handlerFunc := func(ctx context.Context, req *TestReqReflect) (*TestRes, error) {
		return &TestRes{ID: req.Name}, nil
	}

	t.Run("Default_Permissive", func(t *testing.T) {
		h := NewHandler(handlerFunc, WithBinders(&JsonBinder{DisallowUnknownFields: false}))
		// 包含未知字段 "extra_field"
		body := `{"name": "bob", "age": 30, "extra_field": "ignored"}`
		r := httptest.NewRequest("POST", "/", bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.ServeHTTP(w, r)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Strict_Mode", func(t *testing.T) {
		// 使用 strict binder
		h := NewHandler(handlerFunc, WithBinders(&JsonBinder{DisallowUnknownFields: true}))

		body := `{"name": "bob", "age": 30, "extra_field": "should_fail"}`
		r := httptest.NewRequest("POST", "/", bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.ServeHTTP(w, r)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "unknown field")
	})
}

// 验证 SecurityHeaders 中间件
func TestSecurityHeaders(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := SecurityHeaders()(handler)

	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, r)

	assert.Equal(t, "DENY", w.Header().Get("X-Frame-Options"))
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "1; mode=block", w.Header().Get("X-XSS-Protection"))
}

// TestNewHandler_TraceID_Header 验证 Header 和 Body 中的 TraceID 自动注入
func TestNewHandler_TraceID_Injection(t *testing.T) {
	// 1. 模拟 TraceID 提供者
	GetTraceID = func(ctx context.Context) string {
		return "trace-id-999"
	}
	defer func() { GetTraceID = nil }()

	// 2. 定义标准签名的 Handler (注意：是 context.Context，不是 *httpx.Context)
	handlerFunc := func(ctx context.Context, req *TestReqReflect) (*TestRes, error) {
		return &TestRes{ID: "ok"}, nil
	}

	h := NewHandler(handlerFunc)

	body := `{"name": "alice"}`
	r := httptest.NewRequest("POST", "/", bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, r)

	assert.Equal(t, http.StatusOK, w.Code)

	// 验证 Header
	assert.Equal(t, "trace-id-999", w.Header().Get("X-Trace-ID"), "Header should contain TraceID")

	// 验证 Body
	var resp Response[TestRes]
	err := sonic.ConfigDefault.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "trace-id-999", resp.TraceID, "Response body should contain TraceID")
}

// TestError_TraceID_Injection 验证错误时的 TraceID 注入
func TestError_TraceID_Injection(t *testing.T) {
	GetTraceID = func(ctx context.Context) string {
		return "error-trace-id"
	}
	defer func() { GetTraceID = nil }()

	// 模拟触发错误
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	Error(w, r, ErrBadRequest)

	// 验证 Header
	assert.Equal(t, "error-trace-id", w.Header().Get("X-Trace-ID"))

	// 验证 Body
	var resp Response[any]
	sonic.ConfigDefault.NewDecoder(w.Body).Decode(&resp)
	assert.Equal(t, "error-trace-id", resp.TraceID)
	assert.Equal(t, "BAD_REQUEST", resp.Code)
}

func TestNoVarySearch(t *testing.T) {
	handlerFunc := func(ctx context.Context, req *TestReqQuery) (*TestRes, error) {
		return &TestRes{ID: "ok"}, nil
	}
	handlerFuncEmpty := func(ctx context.Context, req *TestReqEmpty) (*TestRes, error) {
		return &TestRes{ID: "ok"}, nil
	}

	t.Run("Default_AutoDetect", func(t *testing.T) {
		h := NewHandler(handlerFunc)
		r := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		// 期望：params, except=("q" "sort" "page") 顺序不保证，但 keys 应该都有
		header := w.Header().Get("No-Vary-Search")
		assert.Contains(t, header, "params")
		assert.Contains(t, header, "except=")
		assert.Contains(t, header, "\"q\"")
		assert.Contains(t, header, "\"sort\"")
		assert.Contains(t, header, "\"page\"")
	})

	t.Run("Default_EmptyStruct", func(t *testing.T) {
		h := NewHandler(handlerFuncEmpty)
		r := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		assert.Equal(t, "params", w.Header().Get("No-Vary-Search"))
	})

	t.Run("Manual_Override", func(t *testing.T) {
		h := NewHandler(handlerFunc, WithNoVarySearch("custom_k"))
		r := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		assert.Equal(t, "params, except=(\"custom_k\")", w.Header().Get("No-Vary-Search"))
	})

	t.Run("Manual_Empty", func(t *testing.T) {
		h := NewHandler(handlerFunc, WithNoVarySearch())
		r := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		assert.Equal(t, "params", w.Header().Get("No-Vary-Search"))
	})

	t.Run("Disabled", func(t *testing.T) {
		h := NewHandler(handlerFunc, DisableNoVarySearch())
		r := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		assert.Empty(t, w.Header().Get("No-Vary-Search"))
	})

	t.Run("NewResponder_Integration", func(t *testing.T) {
		h := NewResponder(func(ctx context.Context, req *TestReqQuery) (Responder, error) {
			return NoContent{Status: 204}, nil
		})
		r := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		header := w.Header().Get("No-Vary-Search")
		assert.Contains(t, header, "except=")
		assert.Contains(t, header, "\"q\"")
	})
}
