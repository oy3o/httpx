package httpx

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"syscall"

	"github.com/bytedance/sonic"
)

// SafeMode 控制是否开启错误脱敏。
// 建议在生产环境设置为 true。
var SafeMode = false

// --- 1. 预定义业务错误码 (Predefined Business Codes) ---

const (
	// CodeOK 表示成功
	CodeOK = "OK"

	// CodeInternalError 服务器内部错误 (500)
	CodeInternalError = "INTERNAL_ERROR"

	// CodeBadRequest 请求参数错误 (400)
	CodeBadRequest = "BAD_REQUEST"

	// CodeUnauthorized 未认证 (401)
	CodeUnauthorized = "UNAUTHORIZED"

	// CodeForbidden 无权限 (403)
	CodeForbidden = "FORBIDDEN"

	// CodeNotFound 资源不存在 (404)
	CodeNotFound = "NOT_FOUND"

	// CodeTooManyRequests 请求过多 (429)
	CodeTooManyRequests = "TOO_MANY_REQUESTS"

	// CodeConflict 资源冲突 (409)
	CodeConflict = "CONFLICT"

	// CodeValidation 校验失败 (400)
	CodeValidation = "VALIDATION_FAILED"

	// CodeRequestEntityTooLarge 请求体过大 (413)
	CodeRequestEntityTooLarge = "REQUEST_ENTITY_TOO_LARGE"
)

// --- 2. 预定义错误实例 (Common Errors) ---
// 可以在 Handler 中直接 return 这些变量

var (
	ErrBadRequest            = &HttpError{HttpCode: http.StatusBadRequest, BizCode: CodeBadRequest, Msg: "Bad Request"}
	ErrUnauthorized          = &HttpError{HttpCode: http.StatusUnauthorized, BizCode: CodeUnauthorized, Msg: "Unauthorized"}
	ErrForbidden             = &HttpError{HttpCode: http.StatusForbidden, BizCode: CodeForbidden, Msg: "Forbidden"}
	ErrNotFound              = &HttpError{HttpCode: http.StatusNotFound, BizCode: CodeNotFound, Msg: "Not Found"}
	ErrTooManyRequests       = &HttpError{HttpCode: http.StatusTooManyRequests, BizCode: CodeTooManyRequests, Msg: "Too Many Requests"}
	ErrInternal              = &HttpError{HttpCode: http.StatusInternalServerError, BizCode: CodeInternalError, Msg: "Internal Server Error"}
	ErrRequestEntityTooLarge = &HttpError{HttpCode: http.StatusRequestEntityTooLarge, BizCode: CodeRequestEntityTooLarge, Msg: "Request Entity Too Large"}
)

// --- 3. 接口定义 ---

// ErrorCoder 定义了如何提取 HTTP 状态码。
// 任何实现了此接口的 error，httpx 都会使用其返回的状态码，而不是默认的 500。
type ErrorCoder interface {
	HTTPStatus() int
}

// BizCoder 定义了如何提取业务错误码 (String)。
// 任何实现了此接口的 error，httpx 都会使用其返回的字符串作为响应体中的 code 字段。
type BizCoder interface {
	BizStatus() string
}

// PublicError 定义了该错误是否包含可安全展示给前端的信息。
// 在 SafeMode=true 时，只有实现了此接口且 PublicMessage() 返回非空，或者具体类型为 *HttpError 的错误，
// 其原本的 Error() 内容才会被返回给客户端，否则将被替换为 "Internal Server Error"。
type PublicError interface {
	PublicMessage() string
}

// --- 4. HttpError 核心结构 ---

// HttpError 是一个通用的错误实现，同时满足 error, ErrorCoder 和 BizCoder 接口。
// HttpError 被视为“安全的”，因为它是开发者显式构造的业务错误。
type HttpError struct {
	HttpCode int
	BizCode  string
	Msg      string
}

func (e *HttpError) Error() string { return e.Msg }

func (e *HttpError) HTTPStatus() int { return e.HttpCode }

func (e *HttpError) BizStatus() string { return e.BizCode }

func (e *HttpError) PublicMessage() string { return e.Msg }

// NewError 创建一个新的 HttpError。
// httpCode: HTTP 状态码 (如 404)
// bizCode: 业务错误码 (如 "USER_NOT_FOUND")
// msg: 错误描述
func NewError(httpCode int, bizCode string, msg string) *HttpError {
	return &HttpError{
		HttpCode: httpCode,
		BizCode:  bizCode,
		Msg:      msg,
	}
}

// --- 5. Error Handler (核心逻辑) ---

// ErrorHook 是一个回调函数，用于处理错误的副作用（如记录日志）。
// 用户可以在 NewHandler 的 Option 中覆盖它。
var ErrorHook func(ctx context.Context, err error) = nil

// Error 负责将 error 转换为 HTTP 响应并写入 ResponseWriter。
func Error(w http.ResponseWriter, r *http.Request, err error, errhooks ...func(ctx context.Context, err error)) {
	// 执行 ErrorHook (通常用于日志记录)
	// 无论 SafeMode 是否开启，日志里都应该记录原始的详细错误
	var errhook func(ctx context.Context, err error)
	if len(errhooks) == 0 {
		errhook = ErrorHook
	} else {
		errhook = errhooks[0]
	}
	if errhook != nil {
		errhook(r.Context(), err)
	}

	// 确定 HTTP 状态码和业务码
	httpCode := http.StatusInternalServerError
	bizCode := CodeInternalError // 默认业务码
	msg := err.Error()

	// 尝试提取 HTTP 状态码
	if e, ok := err.(ErrorCoder); ok {
		httpCode = e.HTTPStatus()
		// 如果有 HTTP 状态码，我们可以提供更智能的默认 BizCode
		bizCode = inferBizCode(httpCode)
	}

	// 尝试提取业务码 (如果实现了接口，覆盖推断值)
	if e, ok := err.(BizCoder); ok {
		if code := e.BizStatus(); code != "" {
			bizCode = code
		}
	}

	// 安全模式下的错误脱敏
	if SafeMode {
		isSafe := false
		// 1. 如果是 HttpError 类型，视为安全
		if _, ok := err.(*HttpError); ok {
			isSafe = true
		} else if pub, ok := err.(PublicError); ok {
			// 2. 如果实现了 PublicError 接口，使用其安全消息
			safeMsg := pub.PublicMessage()
			if safeMsg != "" {
				msg = safeMsg
				isSafe = true
			}
		}

		// 3. 如果不安全，或者是 5xx 类服务端错误（通常包含堆栈或内部细节），进行屏蔽
		// 注意：即使是 HttpError，如果是 500，我们通常也保留原 msg，因为那是开发者自己 NewError(500, ...) 写的。
		// 这里主要拦截那些未被包装的 raw error (e.g. sql.ErrNoRows, os.PathError)
		if !isSafe && httpCode >= 500 {
			msg = "Internal Server Error"
		}
	}

	// 自动在 Error Response Header 中注入 TraceID
	var traceID string
	if GetTraceID != nil {
		traceID = GetTraceID(r.Context())
		if traceID != "" {
			// 避免重复设置（如果 NewHandler 已经设置过）
			if w.Header().Get("X-Trace-ID") == "" {
				w.Header().Set("X-Trace-ID", traceID)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(httpCode)

	resp := &Response[any]{
		Code:    bizCode,
		Message: msg,
		TraceID: traceID,
	}

	if err := sonic.ConfigDefault.NewEncoder(w).Encode(resp); err != nil {
		// 忽略网络断开引发的写入错误
		if errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) {
			return
		}
		if errhook != nil {
			errhook(r.Context(), fmt.Errorf("httpx: failed to write error response: %w", err))
		}
	}
}

func inferBizCode(httpCode int) string {
	switch httpCode {
	case http.StatusBadRequest:
		return CodeBadRequest
	case http.StatusUnauthorized:
		return CodeUnauthorized
	case http.StatusForbidden:
		return CodeForbidden
	case http.StatusNotFound:
		return CodeNotFound
	case http.StatusTooManyRequests:
		return CodeTooManyRequests
	case http.StatusConflict:
		return CodeConflict
	case http.StatusInternalServerError:
		return CodeInternalError
	case http.StatusRequestEntityTooLarge:
		return CodeRequestEntityTooLarge
	default:
		if httpCode >= 400 && httpCode < 500 {
			return "ERROR"
		}
		return CodeInternalError
	}
}
