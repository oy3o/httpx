package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSetCookie_AutoArmor 测试写入时的智能前缀升级逻辑。
// 验证系统是否能在不同配置下自动选择最安全的前缀。
func TestSetCookie_AutoArmor(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		opts         []CookieOption
		wantName     string // 期望最终写入的 Cookie 名称
		wantSecure   bool   // 期望是否开启 Secure
		wantHttpOnly bool   // 期望是否开启 HttpOnly
	}{
		{
			name:         "Default Production (Secure, Path=/, NoDomain)",
			key:          "token",
			opts:         nil, // 默认配置
			wantName:     "__Host-token",
			wantSecure:   true,
			wantHttpOnly: true,
		},
		{
			name: "Subdomain Context (Domain set -> Downgrade to __Secure-)",
			key:  "token",
			// 设置了 Domain，浏览器禁止使用 __Host-，应自动降级为 __Secure-
			opts:       []CookieOption{WithCookieDomain("api.example.com")},
			wantName:   "__Secure-token",
			wantSecure: true,
		},
		{
			name: "Custom Path Context (Path set -> Downgrade to __Secure-)",
			key:  "token",
			// 设置了非根路径，浏览器禁止使用 __Host-，应自动降级为 __Secure-
			opts:       []CookieOption{WithCookiePath("/api")},
			wantName:   "__Secure-token",
			wantSecure: true,
		},
		{
			name: "Localhost Dev (Insecure -> No Prefix)",
			key:  "token",
			// 显式关闭 Secure，无法使用任何前缀
			opts:       []CookieOption{WithInsecure()},
			wantName:   "token",
			wantSecure: false,
		},
		{
			name: "Explicit Prefix (User manually adds __Host-)",
			key:  "__Host-manual",
			// 用户自己加了前缀，代码不应重复添加
			opts:       nil,
			wantName:   "__Host-manual",
			wantSecure: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			SetCookie(w, tt.key, "payload", tt.opts...)

			res := w.Result()
			cookies := res.Cookies()

			if len(cookies) != 1 {
				t.Fatalf("expected 1 cookie, got %d", len(cookies))
			}
			got := cookies[0]

			if got.Name != tt.wantName {
				t.Errorf("expected cookie name %q, got %q", tt.wantName, got.Name)
			}
			if got.Secure != tt.wantSecure {
				t.Errorf("expected secure %v, got %v", tt.wantSecure, got.Secure)
			}
			if tt.wantHttpOnly && !got.HttpOnly {
				t.Error("expected HttpOnly to be true")
			}
		})
	}
}

// TestGetCookie_PriorityDefense 测试读取时的优先级逻辑。
// 模拟 Cookie Tossing 攻击场景，验证我们是否优先读取了安全版本。
func TestGetCookie_PriorityDefense(t *testing.T) {
	tests := []struct {
		name       string
		cookieName string
		// 模拟请求中携带的 Cookie 列表 (模拟浏览器发送的原始 Header)
		// 注意：顺序很重要，通常浏览器会按 path 匹配度排序，但服务端解析顺序不确定
		// 我们的 GetCookie 逻辑应该与 Header 顺序无关，而是按前缀优先级查找
		inCookies []*http.Cookie
		wantValue string
		wantErr   bool
	}{
		{
			name:       "Priority: __Host- over Raw",
			cookieName: "session",
			inCookies: []*http.Cookie{
				{Name: "session", Value: "evil-injected-value"},
				{Name: "__Host-session", Value: "safe-good-value"},
			},
			wantValue: "safe-good-value",
		},
		{
			name:       "Priority: __Secure- over Raw",
			cookieName: "session",
			inCookies: []*http.Cookie{
				{Name: "session", Value: "evil"},
				{Name: "__Secure-session", Value: "safe"},
			},
			wantValue: "safe",
		},
		{
			name:       "Priority: __Host- over __Secure-",
			cookieName: "session",
			inCookies: []*http.Cookie{
				{Name: "__Secure-session", Value: "weak-safe"},
				{Name: "__Host-session", Value: "strong-safe"},
			},
			wantValue: "strong-safe",
		},
		{
			name:       "Fallback to Raw (Legacy/Dev)",
			cookieName: "session",
			inCookies: []*http.Cookie{
				{Name: "session", Value: "legacy"},
			},
			wantValue: "legacy",
		},
		{
			name:       "Not Found",
			cookieName: "ghost",
			inCookies: []*http.Cookie{
				{Name: "other", Value: "foo"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			for _, c := range tt.inCookies {
				r.AddCookie(c)
			}

			got, err := GetCookie(r, tt.cookieName)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetCookie() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.wantValue {
				t.Errorf("GetCookie() = %v, want %v", got, tt.wantValue)
			}
		})
	}
}

// TestDelCookie_NukeStrategy 测试删除时的覆盖打击逻辑。
// 验证是否生成了针对所有变体的过期指令。
func TestDelCookie_NukeStrategy(t *testing.T) {
	w := httptest.NewRecorder()
	targetName := "auth"

	// 执行删除
	DelCookie(w, targetName)

	res := w.Result()
	cookies := res.Cookies()

	// 我们期望看到 3 个 Set-Cookie 指令：
	// 1. __Host-auth
	// 2. __Secure-auth
	// 3. auth
	expectedNames := map[string]bool{
		"__Host-" + targetName:   false,
		"__Secure-" + targetName: false,
		targetName:               false,
	}

	for _, c := range cookies {
		// 验证是否过期 (MaxAge < 0 或 Expires 是过去时间)
		if c.MaxAge > 0 {
			t.Errorf("cookie %s not expired: MaxAge=%d", c.Name, c.MaxAge)
		}
		// 验证 Value 是否被清空
		if c.Value != "" {
			t.Errorf("cookie %s value not cleared", c.Name)
		}

		if _, ok := expectedNames[c.Name]; ok {
			expectedNames[c.Name] = true
		} else {
			t.Logf("Warning: unexpected cookie cleared: %s", c.Name)
		}
	}

	for name, found := range expectedNames {
		if !found {
			t.Errorf("expected cookie variant %q to be cleared, but it was missing in headers", name)
		}
	}
}
