package httpx

import "net/http"

// Responder 让业务返回值能够自主决定如何写入 ResponseWriter。
// 这是我们的“逃生舱”，用于处理重定向、静态文件或自定义 Header 等场景。
type Responder interface {
	WriteResponse(w http.ResponseWriter, r *http.Request)
}

// NewResponder 创建一个允许业务逻辑直接控制响应过程的处理器。
// 适用于重定向、文件下载、自定义状态码等。
func NewResponder[Req any, Res Responder](fn HandlerFunc[Req, Res], opts ...Option) http.HandlerFunc {
	cfg := &config{
		validator:   Validator,
		binders:     Binders,
		errorFunc:   Error,
		errorHook:   ErrorHook,
		maxBodySize: 2 << 20,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	nvHeader := buildNoVarySearch[Req](cfg)

	return func(w http.ResponseWriter, r *http.Request) {
		if nvHeader != "" {
			w.Header().Set("No-Vary-Search", nvHeader)
		}

		// 复用通用的绑定、验证和执行逻辑
		res, _, ok := prepare(w, r, cfg, fn)
		if !ok {
			return
		}

		// 执行业务自定义的写入逻辑
		// 注意：prepare 内部已经完成了 TraceID 在 Header 中的注入
		res.WriteResponse(w, r)
	}
}

// --- 常用原语 (Core Primitives) ---

// Redirect 实现了 Responder 接口，用于重定向。
type Redirect struct {
	URL  string
	Code int
}

func (rd Redirect) WriteResponse(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, rd.URL, rd.Code)
}

// RawBytes 直接写入原始字节流，带上自定义 Content-Type。
type RawBytes struct {
	Status      int
	ContentType string
	Data        []byte
}

func (rb RawBytes) WriteResponse(w http.ResponseWriter, r *http.Request) {
	if rb.ContentType != "" {
		w.Header().Set("Content-Type", rb.ContentType)
	}
	w.WriteHeader(rb.Status)
	_, _ = w.Write(rb.Data)
}

// NoContent 用于返回 204 等空响应
type NoContent struct {
	Status int
}

func (nc NoContent) WriteResponse(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(nc.Status)
}
