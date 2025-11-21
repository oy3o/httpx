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
}

func TestError_GenericFallback(t *testing.T) {
	// 测试未知错误回退到 500 INTERNAL_ERROR
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)

	Error(w, r, errors.New("unknown db error"))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), CodeInternalError)
	assert.Contains(t, w.Body.String(), "unknown db error")
}
