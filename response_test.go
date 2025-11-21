package httpx

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileResponse(t *testing.T) {
	// Test 1: Normal filename
	f1 := &FileResponse{
		Content: bytes.NewReader([]byte("content")),
		Name:    "hello.txt",
	}
	h1 := f1.Headers()
	// 期望: attachment; filename="hello.txt"
	assert.Equal(t, `attachment; filename="hello.txt"`, h1["Content-Disposition"])

	// Test 2: Filename with space and quotes (Injection check)
	f2 := &FileResponse{
		Content: bytes.NewReader([]byte("content")),
		Name:    `cool "file" name.txt`,
	}
	h2 := f2.Headers()
	// Go 的 %q 会转义双引号： "cool \"file\" name.txt"
	// 期望: attachment; filename="cool \"file\" name.txt"
	assert.Equal(t, `attachment; filename="cool \"file\" name.txt"`, h2["Content-Disposition"])

	// Test WriteTo
	w := httptest.NewRecorder()
	_, err := f1.WriteTo(w)
	require.NoError(t, err)
	assert.Equal(t, "content", w.Body.String())
}
