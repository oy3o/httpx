package httpx

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/bytedance/sonic"
)

// JsonBinder 使用 sonic 进行极速解码
type JsonBinder struct {
	// DisallowUnknownFields 控制是否允许 JSON 中包含结构体未定义的字段。
	// 默认为 true (不允许)，否则可能导致“参数污染”或逻辑绕过。。
	DisallowUnknownFields bool
}

func (b *JsonBinder) Name() string     { return "json" }
func (b *JsonBinder) Type() BinderType { return BinderBody }
func (b *JsonBinder) Match(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Content-Type"), "application/json")
}

func (b *JsonBinder) Bind(r *http.Request, v any) error {
	if r.Body == nil || r.Body == http.NoBody {
		return nil
	}

	decoder := sonic.ConfigDefault.NewDecoder(r.Body)

	if b.DisallowUnknownFields {
		decoder.DisallowUnknownFields()
	}

	if err := decoder.Decode(v); err != nil {
		return fmt.Errorf("bind json error: %w", err)
	}
	return nil
}
