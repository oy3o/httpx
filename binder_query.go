package httpx

import (
	"net/http"
)

// QueryBinder 处理 URL 查询参数
type QueryBinder struct{}

func (b *QueryBinder) Name() string     { return "query" }
func (b *QueryBinder) Type() BinderType { return BinderMeta }
func (b *QueryBinder) Match(r *http.Request) bool {
	return r.URL.RawQuery != ""
}

func (b *QueryBinder) Bind(r *http.Request, v any) error {
	// SchemaDecoder 性能已足够好
	return SchemaDecoder.Decode(v, r.URL.Query())
}
