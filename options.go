package httpx

import (
	"context"

	"github.com/go-playground/validator/v10"
)

type config struct {
	noEnvelope          bool
	validator           *validator.Validate
	binders             []Binder
	errorFunc           ErrorFunc
	errorHook           func(ctx context.Context, err error)
	maxBodySize         int64
	noVarySearch        []string
	disableNoVarySearch bool
}

type Option func(*config)

// NoEnvelope 指示 Handler 不要使用标准的 {code, msg, data} 封口
func NoEnvelope() Option {
	return func(c *config) {
		c.noEnvelope = true
	}
}

// WithValidator 设置自定义的 Validator 实例
func WithValidator(v *validator.Validate) Option {
	return func(c *config) {
		c.validator = v
	}
}

// WithBinders 设置自定义的 Binder 链（将覆盖默认链）
func WithBinders(b ...Binder) Option {
	return func(c *config) {
		c.binders = b
	}
}

// AddBinders 在默认 Binder 链之前添加自定义 Binder
func AddBinders(b ...Binder) Option {
	return func(c *config) {
		c.binders = append(b, c.binders...)
	}
}

// WithErrorFunc 设置该 Handler 专属的错误处理器
func WithErrorFunc(handler ErrorFunc) Option {
	return func(c *config) {
		c.errorFunc = handler
	}
}

// WithErrorHook 设置该 Handler 专属的错误处理 Hook
func WithErrorHook(hook func(ctx context.Context, err error)) Option {
	return func(c *config) {
		c.errorHook = hook
	}
}

// WithMultipartLimit 设置解析 Multipart 表单时的最大内存限制 (字节)。
// 默认值为 8MB (DefaultMultipartMemory)。
// 设置此选项会创建一个新的 FormBinder 实例并替换掉默认链中的实例，
// 这样可以避免修改全局变量，也不破坏链中其他 Binder 的配置。
func WithMultipartLimit(limit int64) Option {
	return func(c *config) {
		// 创建副本以避免修改原始切片（如果它是共享的）
		binders := make([]Binder, len(c.binders))
		copy(binders, c.binders)

		// 简化调用
		if limit > c.maxBodySize {
			c.maxBodySize = limit
		}

		found := false
		for i, b := range binders {
			if _, ok := b.(*FormBinder); ok {
				// 替换为带有新限制的新实例
				binders[i] = &FormBinder{MaxMemory: limit}
				found = true
				break
			}
		}

		// 如果原本没有 FormBinder (虽然默认有)，则追加一个
		if !found {
			binders = append(binders, &FormBinder{MaxMemory: limit})
		}

		c.binders = binders
	}
}

// WithMaxBodySize 限制请求体 (Body) 的最大字节数。
// 超过限制时将返回 413 Request Entity Too Large。
// 这是一个硬限制，会切断连接，有效防止大文件上传攻击或磁盘耗尽。
// 建议值：常规 API 设为 2MB-10MB；文件上传接口根据业务需求设定 (如 100MB)。
func WithMaxBodySize(maxBytes int64) Option {
	return func(c *config) {
		c.maxBodySize = maxBytes
	}
}

// WithNoVarySearch 手动指定 No-Vary-Search 头部允许的参数列表。
// 如果不指定 (默认)，将自动使用 Req 结构体中定义的参数作为 allowlist (No-Vary-Search: params, except=(...))。
// 传入的 keys 将被视为允许变化的参数 (except 列表)。
// 如果传入空且未禁用，将生成 "No-Vary-Search: params" (忽略所有参数)。
func WithNoVarySearch(keys ...string) Option {
	return func(c *config) {
		c.noVarySearch = keys
		// 显式设置为空 slice (非 nil) 以区别于默认行为
		if c.noVarySearch == nil {
			c.noVarySearch = []string{}
		}
	}
}

// DisableNoVarySearch 禁用 No-Vary-Search 头部的自动生成。
func DisableNoVarySearch() Option {
	return func(c *config) {
		c.disableNoVarySearch = true
	}
}
