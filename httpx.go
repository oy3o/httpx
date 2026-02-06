package httpx

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"

	"github.com/bytedance/sonic"
)

// GetTraceID 是一个依赖注入点。
// 外部库（如 o11y）应该设置这个函数，以便 httpx 能获取到 TraceID。
var GetTraceID func(ctx context.Context) string = nil

// HandlerFunc 定义业务处理函数签名。
// 坚持使用标准 context.Context，避免框架耦合, 我们和 gin 那样的框架有本质上的不同,
// 我们所有的实现都是通过配置和中间件完成, 所谓渐进式开发即是如此, 用户不必依赖我们, 但有我们会更好。
type HandlerFunc[Req any, Res any] func(ctx context.Context, req *Req) (Res, error)

func NewHandler[Req any, Res any](fn HandlerFunc[Req, Res], opts ...Option) http.HandlerFunc {
	// 应用配置 (默认值)
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

	// 计算 No-Vary-Search 头 (一次性计算)
	nvHeader := buildNoVarySearch[Req](cfg)

	return func(w http.ResponseWriter, r *http.Request) {
		// 设置 No-Vary-Search 头
		if nvHeader != "" {
			w.Header().Set("No-Vary-Search", nvHeader)
		}

		res, traceID, ok := prepare(w, r, cfg, fn)
		if !ok {
			return
		}

		// JSON 响应
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)

		var body any = res
		if !cfg.noEnvelope {
			// 使用标准信封包裹，并自动填充 TraceID
			resp := &Response[Res]{
				Code:    CodeOK,
				Message: "success",
				Data:    res,
				TraceID: traceID, // 自动注入
			}
			body = resp
		}

		if err := sonic.ConfigDefault.NewEncoder(w).Encode(body); err != nil && cfg.errorHook != nil {
			cfg.errorHook(r.Context(), err)
		}
	}
}

// NewStreamHandler 创建一个流式处理函数。
func NewStreamHandler[Req any, Res Streamable](fn HandlerFunc[Req, Res], opts ...Option) http.HandlerFunc {
	// 应用配置 (默认值)
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

		res, _, ok := prepare(w, r, cfg, fn)
		if !ok {
			return
		}

		// 处理流式响应
		for k, v := range res.Headers() {
			w.Header().Set(k, v)
		}
		if _, err := res.WriteTo(w); err != nil && cfg.errorHook != nil {
			cfg.errorHook(r.Context(), err)
		}
	}
}

// 内部辅助函数，处理通用的请求准备工作
func prepare[Req any, Res any](w http.ResponseWriter, r *http.Request, cfg *config, fn HandlerFunc[Req, Res]) (res Res, traceID string, ok bool) {
	ctx := r.Context()
	errFunc := cfg.errorFunc

	// 1. 应用 Body 大小限制
	if cfg.maxBodySize > 0 {
		// http.MaxBytesReader 会包装 r.Body。
		// 当读取超过限制时，Read 会返回 error，并且 ResponseWriter 会被标记，
		// 指示服务器应该关闭连接而不是复用。
		r.Body = http.MaxBytesReader(w, r.Body, cfg.maxBodySize)
	}

	// 2. 绑定 (Binding)
	var req Req
	// 使用配置中的 binders
	if err := Bind(r, &req, cfg.binders...); err != nil {
		// 如果是因为 Body 太大导致的错误，返回 413
		var maxBytesErr *http.MaxBytesError
		if cfg.maxBodySize > 0 && errors.As(err, &maxBytesErr) {
			errFunc(w, r, ErrRequestEntityTooLarge, WithHook(cfg.errorHook))
			return
		}
		errFunc(w, r, &HttpError{HttpCode: http.StatusBadRequest, Msg: err.Error()}, WithHook(cfg.errorHook))
		return
	}

	// 3. 验证 (Validation)
	// 传入配置中的 validator 实例
	if err := Validate(ctx, &req, cfg.validator); err != nil {
		errFunc(w, r, err, WithHook(cfg.errorHook)) // Validate 返回的通常已经是 HttpError (400)
		return
	}

	// 4. 业务逻辑 (Business Logic)
	// 直接传递标准 Context
	res, err := fn(ctx, &req)
	if err != nil {
		errFunc(w, r, err, WithHook(cfg.errorHook))
		return
	}

	// 5. 处理成功路径: 自动在 Response Header 中注入 TraceID
	// 失败路径: errFunc 内部处理
	// 这样无论业务逻辑是否成功，客户端都能通过 Header 拿到 TraceID
	if GetTraceID != nil {
		traceID = GetTraceID(ctx)
		if traceID != "" {
			w.Header().Set("X-Trace-ID", traceID)
		}
	}

	return res, traceID, true
}

func buildNoVarySearch[Req any](cfg *config) string {
	if cfg.disableNoVarySearch {
		return ""
	}

	var keys []string
	if cfg.noVarySearch != nil {
		// 手动指定
		keys = cfg.noVarySearch
	} else {
		// 自动推断
		t := reflect.TypeOf((*Req)(nil))
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
		// 只有 struct 才有元数据
		if t.Kind() == reflect.Struct {
			meta := getStructMeta(t)
			keys = meta.formKeys
		}
	}

	if len(keys) == 0 {
		return "key-order, params"
	}
	// 简单去重
	seen := make(map[string]struct{}, len(keys))
	unique := make([]string, 0, len(keys))
	for _, k := range keys {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			unique = append(unique, "\""+k+"\"")
		}
	}
	return "key-order, params, except=(" + strings.Join(unique, " ") + ")"
}
