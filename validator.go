package httpx

import (
	"context"
	"net/http"

	"github.com/go-playground/validator/v10"
)

// Validator 是默认的验证器实例
var Validator = validator.New()

// SelfValidatable 是高性能验证接口。
// 如果 Request 结构体实现了此接口，将跳过反射验证。
type SelfValidatable interface {
	Validate(ctx context.Context) error
}

// Validate 执行验证逻辑。
// v: 待验证的结构体指针
// validatorInstance: 可选的验证器实例，如果为 nil 则使用 DefaultValidator
func Validate(ctx context.Context, v any, validators ...*validator.Validate) error {
	// 1. Fast Path: 接口验证 (优先)
	if val, ok := v.(SelfValidatable); ok {
		err := val.Validate(ctx)
		if err != nil {
			return &HttpError{
				HttpCode: http.StatusBadRequest,
				BizCode:  CodeValidation,
				Msg:      err.Error(),
			}
		}
		return nil
	}

	// 2. Slow Path: 反射验证
	var validator *validator.Validate
	if len(validators) == 0 {
		validator = Validator
	} else {
		validator = validators[0]
	}

	if err := validator.Struct(v); err != nil {
		return &HttpError{
			HttpCode: http.StatusBadRequest,
			BizCode:  CodeValidation,
			Msg:      err.Error(),
		}
	}
	return nil
}
