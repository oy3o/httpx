package httpx

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/bytedance/sonic"
)

// 优化: 预分配 JSON 的 Content-Type 切片，避免每次请求调用 w.Header().Set 产生的字符串分配和规范化开销
var jsonContentType = []string{"application/json; charset=utf-8"}

// 优化: 预分配换行符的字节切片，避免 w.Write([]byte("\n")) 每次请求时分配堆内存 (因为 interface{})
var nlBytes = []byte("\n")

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

	// ⚡ Bolt: 对于静态 Header (一次性计算的值)，可以在闭包外预分配切片
	// 避免在每次请求时执行 w.Header()["No-Vary-Search"] = []string{nvHeader} 产生的分配
	var nvHeaderSlice []string
	if nvHeader != "" {
		nvHeaderSlice = []string{nvHeader}
	}

	// 优化：针对泛型 Res 的复用池，避免每次请求装箱 Response[Res] 时分配内存
	respPool := sync.Pool{
		New: func() any {
			return new(Response[Res])
		},
	}

	return func(w http.ResponseWriter, r *http.Request) {
		// 设置 No-Vary-Search 头
		if nvHeaderSlice != nil {
			w.Header()["No-Vary-Search"] = nvHeaderSlice
		}

		res, traceID, ok := prepare(w, r, cfg, fn)
		if !ok {
			return
		}

		// JSON 响应
		w.Header()["Content-Type"] = jsonContentType
		w.WriteHeader(http.StatusOK)

		if !cfg.noEnvelope {
			// 使用标准信封包裹，并自动填充 TraceID
			resp := respPool.Get().(*Response[Res])
			resp.Code = CodeOK
			resp.Message = "success"
			resp.Data = res
			resp.TraceID = traceID

			data, err := sonic.ConfigDefault.Marshal(resp)
			if err == nil {
				_, err = w.Write(data)
				if err == nil {
					_, err = w.Write(nlBytes)
				}
			}
			if err != nil && cfg.errorHook != nil {
				cfg.errorHook(r.Context(), err)
			}

			// 清理引用，避免内存泄漏
			var zero Res
			resp.Data = zero
			respPool.Put(resp)
			return
		}

		// 无信封模式，直接返回 res
		data, err := sonic.ConfigDefault.Marshal(res)
		if err == nil {
			_, err = w.Write(data)
			if err == nil {
				_, err = w.Write(nlBytes)
			}
		}
		if err != nil && cfg.errorHook != nil {
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

	// ⚡ Bolt: 预分配切片避免每次请求分配
	var nvHeaderSlice []string
	if nvHeader != "" {
		nvHeaderSlice = []string{nvHeader}
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if nvHeaderSlice != nil {
			w.Header()["No-Vary-Search"] = nvHeaderSlice
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
	if cfg.maxBodySize > 0 && r.Body != nil && r.Body != http.NoBody {
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
			w.Header()["X-Trace-Id"] = []string{traceID}
		}
	}

	return res, traceID, true
}

func buildNoVarySearch[Req any](cfg *config) string {
	if cfg.noVarySearch == nil {
		return ""
	}

	keys := cfg.noVarySearch

	if len(keys) == 0 {
		return "key-order, params"
	}

	// Calculate expected length to pre-allocate strings.Builder
	// "key-order, params, except=(" is 27 chars
	// Each key gets quotes, and spaces between keys.
	expectedLen := 27 + len(keys)*4
	for _, k := range keys {
		expectedLen += len(k)
	}
	expectedLen += 1 // For closing parenthesis

	var sb strings.Builder
	sb.Grow(expectedLen)
	sb.WriteString("key-order, params, except=(")

	// Since slice length is usually small, standard deduplication slice can be faster than map
	// However, if we only deduplicate string pointers / content...
	// Let's use a slice for deduplication to avoid map allocations.
	seen := make([]string, 0, len(keys))

	first := true
	for _, k := range keys {
		found := false
		for _, s := range seen {
			if s == k {
				found = true
				break
			}
		}
		if !found {
			seen = append(seen, k)
			if !first {
				sb.WriteByte(' ')
			}
			sb.WriteByte('"')
			sb.WriteString(k)
			sb.WriteByte('"')
			first = false
		}
	}
	sb.WriteByte(')')
	return sb.String()
}
