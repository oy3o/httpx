package httpx

import (
	"net/http"
	"reflect"
)

// PathBinder 处理 URL 路径参数 (Go 1.22+)
type PathBinder struct{}

func (b *PathBinder) Name() string     { return "path" }
func (b *PathBinder) Type() BinderType { return BinderMeta }
func (b *PathBinder) Match(r *http.Request) bool {
	return true // 总是尝试，因为开销极低
}

func (b *PathBinder) Bind(r *http.Request, v any) error {
	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return nil
	}

	// O(1) 获取缓存
	meta := getStructMeta(val.Type())
	if len(meta.pathFields) == 0 {
		return nil
	}

	// 构造 map 传给 schema
	// 预分配容量，避免 map 扩容
	values := make(map[string][]string, len(meta.pathFields))
	hasValue := false

	for _, info := range meta.pathFields {
		// Go 1.22+ method
		pathVal := r.PathValue(info.pathKey)
		if pathVal != "" {
			values[info.schemaKey] = []string{pathVal}
			hasValue = true
		}
	}

	if !hasValue {
		return nil
	}

	// 复用 SchemaDecoder 进行类型转换 (string -> int/bool)
	return SchemaDecoder.Decode(v, values)
}
