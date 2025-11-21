package httpx

import (
	"fmt"
	"io"
	"strconv"
)

// Response 是默认的统一响应信封。
type Response[T any] struct {
	// Code 是业务错误码 (字符串)，例如 "OK", "INVALID_PARAM", "USER_BANNED"。
	// 它与 HTTP Status Code 分离，由前端用于展示具体的错误文案。
	Code    string `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data,omitempty"`
	TraceID string `json:"trace_id,omitempty"`
}

// Streamable 接口用于指示该结构体是流式响应（文件下载、SSE）。
type Streamable interface {
	Headers() map[string]string
	WriteTo(w io.Writer) (int64, error)
}

// --- 辅助类型：FileResponse ---

// FileResponse 是一个实现了 Streamable 的文件响应辅助类。
type FileResponse struct {
	Content io.Reader
	Name    string
	Size    int64
	Type    string
}

func (f *FileResponse) Headers() map[string]string {
	h := make(map[string]string)
	ct := f.Type
	if ct == "" {
		ct = "application/octet-stream"
	}
	h["Content-Type"] = ct
	if f.Name != "" {
		h["Content-Disposition"] = fmt.Sprintf("attachment; filename=%q", f.Name)
	}
	if f.Size > 0 {
		h["Content-Length"] = strconv.FormatInt(f.Size, 10)
	}
	return h
}

func (f *FileResponse) WriteTo(w io.Writer) (int64, error) {
	return io.Copy(w, f.Content)
}
