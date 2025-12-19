package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"syscall"

	"github.com/bytedance/sonic"
)

type ErrorFunc func(w http.ResponseWriter, r *http.Request, err error, opts ...ErrorOption)

// ErrorOption 定义配置 Error 处理行为的函数签名
type ErrorOption func(*errorConfig)

// errorConfig 内部配置容器
type errorConfig struct {
	handler    ErrorFunc
	hook       func(context.Context, error)
	noEnvelope bool
	status     int // 允许强制覆盖状态码
}

// WithHandler 注入实际错误处理函数
func WithHandler(fn ErrorFunc) ErrorOption {
	return func(cfg *errorConfig) {
		cfg.handler = fn
	}
}

// WithHook 注入错误钩子
func WithHook(fn func(context.Context, error)) ErrorOption {
	return func(cfg *errorConfig) {
		cfg.hook = fn
	}
}

// WithNoEnvelope 禁止使用标准信封包裹 (用于 OIDC 或特殊协议)
func WithNoEnvelope() ErrorOption {
	return func(cfg *errorConfig) {
		cfg.noEnvelope = true
	}
}

// WithStatus 强制指定 HTTP 状态码 (覆盖 error 本身的推断)
func WithStatus(code int) ErrorOption {
	return func(cfg *errorConfig) {
		cfg.status = code
	}
}

// Error 负责将 error 转换为 HTTP 响应并写入 ResponseWriter。
func Error(w http.ResponseWriter, r *http.Request, err error, opts ...ErrorOption) {
	// 1. 初始化默认配置
	cfg := &errorConfig{
		hook: ErrorHook,
	}

	// 2. 应用选项
	for _, opt := range opts {
		opt(cfg)
	}

	// 3. 执行 Hook (日志记录)
	// 无论 SafeMode 是否开启，日志里都应该记录原始的详细错误
	if cfg.hook != nil {
		cfg.hook(r.Context(), err)
	}

	// 4. 执行实际错误处理函数
	if cfg.handler != nil {
		cfg.handler(w, r, err)
		return
	}

	// 5. 确定 HTTP 状态码和业务码
	httpCode := http.StatusInternalServerError
	bizCode := CodeInternalError // 默认业务码 "internal_error"
	msg := err.Error()

	// 尝试提取 HTTP 状态码
	if e, ok := err.(ErrorCoder); ok {
		httpCode = e.HTTPStatus()
		bizCode = inferBizCode(httpCode)
	}

	// 尝试提取业务码 (覆盖推断值)
	if e, ok := err.(BizCoder); ok {
		if code := e.BizStatus(); code != "" {
			bizCode = code
		}
	}

	// 如果选项里强制指定了 Status，则覆盖以上所有逻辑
	if cfg.status != 0 {
		httpCode = cfg.status
	}

	// 6. 安全模式下的错误脱敏 (Red Team Security Logic)
	if SafeMode {
		isSafe := false
		// a. 显式的 HttpError 视为安全 (通常是业务层抛出的)
		if _, ok := err.(*HttpError); ok {
			isSafe = true
		} else if pub, ok := err.(PublicError); ok {
			// b. 实现了 PublicError 接口，使用其安全消息
			safeMsg := pub.PublicMessage()
			if safeMsg != "" {
				msg = safeMsg
				isSafe = true
			}
		}

		// c. 屏蔽敏感的 5xx 错误
		if !isSafe && httpCode >= 500 {
			msg = "Internal Server Error"
		}
	}

	// 7. 写入响应头
	w.WriteHeader(httpCode)

	// 自动注入 TraceID
	var traceID string
	if GetTraceID != nil {
		traceID = GetTraceID(r.Context())
		if traceID != "" {
			if w.Header().Get("X-Trace-ID") == "" {
				w.Header().Set("X-Trace-ID", traceID)
			}
		}
	}

	// 8. 构建响应体
	var resp any = err

	// 如果配置了 NoEnvelope，或者错误本身实现了 json.Marshaler (说明它想自己控制 JSON 格式，如 OIDC Error)
	// 这是一个更智能的判断逻辑：
	// 如果 err 实现了 MarshalJSON，我们倾向于相信它是想自己控制输出格式的。
	_, isSelfMarshaler := err.(json.Marshaler)

	if !cfg.noEnvelope && !isSelfMarshaler {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		resp = &Response[any]{
			Code:    bizCode,
			Message: msg,
			TraceID: traceID,
		}
	} else {
		// NoEnvelope 模式下，ContentType 可能需要根据业务调整，但通常 JSON 居多
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
		}
	}

	// 9. 写入 Body
	if err := sonic.ConfigDefault.NewEncoder(w).Encode(resp); err != nil {
		if errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) {
			return
		}
		// 如果写入响应失败，且有 hook，再次记录这个“错误的错误”
		if cfg.hook != nil {
			cfg.hook(r.Context(), fmt.Errorf("httpx: failed to write error response: %w", err))
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
