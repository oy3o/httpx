package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInferBizCode(t *testing.T) {
	tests := []struct {
		status   int
		expected string
	}{
		{http.StatusBadRequest, CodeBadRequest},
		{http.StatusUnauthorized, CodeUnauthorized},
		{http.StatusForbidden, CodeForbidden},
		{http.StatusNotFound, CodeNotFound},
		{http.StatusTooManyRequests, CodeTooManyRequests},
		{http.StatusConflict, CodeConflict},
		{http.StatusInternalServerError, CodeInternalError},
		{http.StatusRequestEntityTooLarge, CodeRequestEntityTooLarge},
		{418, "ERROR"},           // 4xx default
		{502, CodeInternalError}, // 5xx default
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, inferBizCode(tt.status), "Status %d should map to %s", tt.status, tt.expected)
	}
}

func TestNewError_Accessors(t *testing.T) {
	err := NewError(404, "USER_GONE", "User is gone")
	assert.Equal(t, 404, err.HTTPStatus())
	assert.Equal(t, "USER_GONE", err.BizStatus())
	assert.Equal(t, "User is gone", err.Error())
	assert.Equal(t, "User is gone", err.PublicMessage())
}

func TestError_GenericFallback(t *testing.T) {
	// 测试未知错误回退到 500 INTERNAL_ERROR
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)

	Error(w, r, errors.New("unknown db error"))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), CodeInternalError)
	assert.Contains(t, w.Body.String(), "INTERNAL_ERROR")
}

// TestError_SafeMode 验证敏感信息脱敏
func TestError_SafeMode(t *testing.T) {
	// 开启安全模式
	oldMode := SafeMode
	SafeMode = true
	defer func() { SafeMode = oldMode }()

	t.Run("Mask_RawError", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)

		// 模拟一个包含敏感信息的原生错误（如 SQL 报错）
		rawErr := errors.New("sql: syntax error near 'SELECT * FROM users'")
		Error(w, r, rawErr)

		// 期望：状态码 500，但消息被替换
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Internal Server Error")
		assert.NotContains(t, w.Body.String(), "sql: syntax error")
	})

	t.Run("Pass_HttpError", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)

		// 模拟显式构造的业务错误
		bizErr := NewError(400, "INVALID_PARAM", "age must be positive")
		Error(w, r, bizErr)

		// 期望：消息透传
		assert.Equal(t, 400, w.Code)
		assert.Contains(t, w.Body.String(), "age must be positive")
	})

	t.Run("Pass_PublicError", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)

		// 模拟实现了 PublicError 接口的自定义错误
		pubErr := &customPublicError{msg: "safe message"}
		Error(w, r, pubErr)

		assert.Contains(t, w.Body.String(), "safe message")
	})
}

type customPublicError struct {
	msg string
}

func (e *customPublicError) Error() string         { return "sensitive info" }
func (e *customPublicError) PublicMessage() string { return e.msg }
