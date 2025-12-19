package httpx

import (
	"context"
	"net/http"
)

// SafeMode 控制是否开启错误脱敏。
// 建议在生产环境设置为 true。
var SafeMode = true

// ErrorHook 是一个回调函数，用于处理错误的副作用（如记录日志）。
// 用户可以在 NewHandler 的 Option 中覆盖它。
var ErrorHook func(ctx context.Context, err error) = nil

// 预定义业务错误码
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

// 预定义错误实例
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
