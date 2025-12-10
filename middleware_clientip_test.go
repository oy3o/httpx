package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClientIPMiddleware(t *testing.T) {
	tests := []struct {
		name           string
		trustedProxies []string
		remoteAddr     string
		headers        map[string]string
		wantIP         string
	}{
		{
			name:           "No Trusted Proxies - Ignore Headers",
			trustedProxies: nil,
			remoteAddr:     "1.1.1.1:1234",
			headers: map[string]string{
				"X-Forwarded-For": "2.2.2.2",
			},
			wantIP: "1.1.1.1",
		},
		{
			name:           "Trusted Proxy (Loopback) - Use XFF",
			trustedProxies: []string{"127.0.0.1/32"},
			remoteAddr:     "127.0.0.1:5678",
			headers: map[string]string{
				"X-Forwarded-For": "2.2.2.2",
			},
			wantIP: "2.2.2.2",
		},
		{
			name:           "Trusted Proxy - Use XFF (Multiple)",
			trustedProxies: []string{"10.0.0.0/8"},
			remoteAddr:     "10.0.0.1:5678",
			headers: map[string]string{
				"X-Forwarded-For": "3.3.3.3, 4.4.4.4",
			},
			wantIP: "3.3.3.3",
		},
		{
			name:           "Trusted Proxy - Use X-Real-IP",
			trustedProxies: []string{"10.0.0.0/8"},
			remoteAddr:     "10.0.0.1:5678",
			headers: map[string]string{
				"X-Real-IP": "5.5.5.5",
			},
			wantIP: "5.5.5.5",
		},
		{
			name:           "Trusted Proxy - XFF Priority over X-Real-IP",
			trustedProxies: []string{"10.0.0.0/8"},
			remoteAddr:     "10.0.0.1:5678",
			headers: map[string]string{
				"X-Forwarded-For": "6.6.6.6",
				"X-Real-IP":       "7.7.7.7",
			},
			wantIP: "6.6.6.6",
		},
		{
			name:           "Untrusted Remote - Ignore Headers",
			trustedProxies: []string{"10.0.0.0/8"},
			remoteAddr:     "192.168.1.1:5678",
			headers: map[string]string{
				"X-Forwarded-For": "8.8.8.8",
			},
			wantIP: "192.168.1.1",
		},
		{
			name:           "IPv6 Remote (Clean Brackets)",
			trustedProxies: nil,
			remoteAddr:     "[::1]:1234",
			headers:        nil,
			wantIP:         "::1",
		},
		{
			name:           "Invalid Remote Addr (Fallback)",
			trustedProxies: nil,
			remoteAddr:     "invalid-addr",
			headers:        nil,
			wantIP:         "invalid-addr",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewClientIPMiddleware(tt.trustedProxies)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got := ClientIP(r.Context())
				assert.Equal(t, tt.wantIP, got)
			}))

			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteAddr
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		})
	}
}

func TestClientIP_Helper(t *testing.T) {
	// Test helper when context is empty
	assert.Equal(t, "", ClientIP(context.Background()))
}
