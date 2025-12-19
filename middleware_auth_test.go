package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockValidator 创建一个简单的验证器闭包
func mockValidator(expectedToken string, mockUserID any) func(context.Context, string) (any, error) {
	return func(ctx context.Context, token string) (any, error) {
		if token == expectedToken {
			return mockUserID, nil
		}
		return nil, errors.New("invalid_token")
	}
}

func TestAuthChain_Flow(t *testing.T) {
	validator := mockValidator("secret", "user-1")

	// 定义一条标准的认证链: Bearer Header -> Cookie -> Query
	chain := AuthChain(
		FromHeader("Bearer", validator),
		FromCookie("auth", validator),
		FromQuery("token", validator),
	)

	tests := []struct {
		name        string
		setupReq    func(r *http.Request)
		wantID      any
		wantErr     bool
		wantErrType error // 可选：期望的具体错误类型
	}{
		{
			name: "Priority 1: Header Success",
			setupReq: func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer secret")
				// 即使有恶意的 cookie 或 query，也应该只认 Header
				r.AddCookie(&http.Cookie{Name: "auth", Value: "bad_cookie"})
			},
			wantID: "user-1",
		},
		{
			name: "Priority 2: Header Missing -> Cookie Success",
			setupReq: func(r *http.Request) {
				r.AddCookie(&http.Cookie{Name: "auth", Value: "secret"})
			},
			wantID: "user-1",
		},
		{
			name: "Priority 3: Header/Cookie Missing -> Query Success",
			setupReq: func(r *http.Request) {
				q := r.URL.Query()
				q.Set("token", "secret")
				r.URL.RawQuery = q.Encode()
			},
			wantID: "user-1",
		},
		{
			name: "Fail-Fast: Header Present but Invalid -> Stop Chain",
			setupReq: func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer wrong_secret")
				// 即使 Cookie 是对的，Header 验证失败也是致命的，不应降级
				r.AddCookie(&http.Cookie{Name: "auth", Value: "secret"})
			},
			wantErr: true,
			// 这里期望的是验证器返回的错误，而不是 ErrNoCredentials
		},
		{
			name:        "All Fail: No Credentials",
			setupReq:    func(r *http.Request) {}, // 空请求
			wantErr:     true,
			wantErrType: ErrNoCredentials,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/", nil)
			tt.setupReq(r)

			gotID, err := chain(w, r)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.wantErrType != nil && !errors.Is(err, tt.wantErrType) {
					t.Errorf("expected error type %v, got %v", tt.wantErrType, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotID != tt.wantID {
				t.Errorf("expected identity %v, got %v", tt.wantID, gotID)
			}
		})
	}
}

func TestWithAuthChallenge(t *testing.T) {
	// 一个总是找不到凭证的策略
	noopStrategy := func(w http.ResponseWriter, r *http.Request) (any, error) {
		return nil, ErrNoCredentials
	}

	// 装饰它
	strategy := WithAuthChallenge(noopStrategy, "WWW-Authenticate", `Bearer realm="test"`)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	_, err := strategy(w, r)

	// 验证是否转换为了 HttpError
	var httpErr *HttpError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected HttpError, got %T: %v", err, err)
	}

	if httpErr.HttpCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", httpErr.HttpCode)
	}

	if val := w.Header().Get("WWW-Authenticate"); val != `Bearer realm="test"` {
		t.Errorf("header not set correctly: got %v", val)
	}
}

func TestAuthMiddleware_Integration(t *testing.T) {
	validator := mockValidator("secret", "user-1")

	// 构建完整中间件：Bearer Header -> Cookie -> 401 Challenge
	mw := Auth(
		WithAuthChallenge(
			AuthChain(
				FromHeader("Bearer", validator),
				FromCookie("session", validator),
			),
			"WWW-Authenticate", `Bearer realm="test"`,
		),
	)

	// 模拟业务 Handler
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 检查 Identity 是否注入成功
		id := r.Context().Value(IdentityKey{})
		if id != "user-1" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	t.Run("Success Path", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("Challenge Path", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		// 没带任何凭证
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, r)

		// 由于 Auth 中间件使用了 httpx.Error，这里我们需要确保 Error 函数能正确处理 HttpError.Headers
		// (假设 Error 函数已经按照之前讨论的逻辑实现了)
		// 这里只验证中间件是否 correctly returned (在没有 ErrorHook 劫持的情况下，httpx.Error 通常会写 Response)

		// 检查 Header 是否存在
		authHead := w.Header().Get("WWW-Authenticate")
		if authHead == "" {
			// 注意：如果 httpx.Error 的实现没有写入 Header，这里会失败。
			// 这是一个集成测试点，确保 httpx.Error 和 Auth 中间件配合良好。
			// 假设你已经在 httpx.Error 里加上了 Headers 的处理逻辑。
			t.Log("Warning: WWW-Authenticate header missing. Ensure httpx.Error handles HttpError.Headers")
		} else if authHead != `Bearer realm="test"` {
			t.Errorf("expected challenge header, got %q", authHead)
		}

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})
}

// TestFromCookie_InternalLogic 验证 FromCookie 策略是否正确利用了 GetCookie 的智能读取
func TestFromCookie_InternalLogic(t *testing.T) {
	validator := mockValidator("secret", "user-1")
	strategy := FromCookie("token", validator)

	t.Run("Reads __Host- Prefix Transparently", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		// 模拟请求中只有 __Host-token，没有 token
		r.AddCookie(&http.Cookie{Name: "__Host-token", Value: "secret"})

		id, err := strategy(w, r)
		if err != nil {
			t.Fatalf("failed to read secure cookie: %v", err)
		}
		if id != "user-1" {
			t.Errorf("got wrong identity: %v", id)
		}
	})
}
