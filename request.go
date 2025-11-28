package httpx

import (
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"reflect"
	"strings"

	"github.com/gorilla/schema"
)

var SchemaDecoder = schema.NewDecoder()

// DefaultMultipartMemory 默认内存限制调整为 8MB
// 超过此限制的部分将暂存到磁盘临时文件中，防止容器环境 OOM。
const DefaultMultipartMemory = 8 << 20

// BinderType 定义绑定器类型
type BinderType int

const (
	// BinderMeta 表示绑定 URL Query, Header, Path 等元数据 (非互斥)
	BinderMeta BinderType = iota
	// BinderBody 表示绑定 Request Body (互斥，流只能读一次)
	BinderBody
)

type Binder interface {
	Name() string
	Type() BinderType
	Match(r *http.Request) bool
	Bind(r *http.Request, v any) error
}

// --- 内置 Binder 实现 ---

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
	decoder := json.NewDecoder(r.Body)

	if b.DisallowUnknownFields {
		decoder.DisallowUnknownFields()
	}

	if err := decoder.Decode(v); err != nil {
		return fmt.Errorf("bind json error: %w", err)
	}
	// 检查是否有多余的数据
	if decoder.More() {
		return fmt.Errorf("bind json error: unexpected extra data in body")
	}
	return nil
}

type FormBinder struct {
	MaxMemory int64
}

func (b *FormBinder) Name() string     { return "form" }
func (b *FormBinder) Type() BinderType { return BinderBody }
func (b *FormBinder) Match(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	return strings.HasPrefix(ct, "application/x-www-form-urlencoded") ||
		strings.HasPrefix(ct, "multipart/form-data")
}

func (b *FormBinder) Bind(r *http.Request, v any) error {
	// 确定内存限制
	limit := b.MaxMemory
	if limit <= 0 {
		limit = DefaultMultipartMemory
	}

	// ParseMultipartForm 会自动处理 urlencoded 和 multipart
	// 使用配置的 limit。如果上传文件超过 limit，多余部分会存储在临时文件中。
	if err := r.ParseMultipartForm(limit); err != nil {
		return fmt.Errorf("parse form error: %w", err)
	}

	// 1. 绑定普通表单字段
	// r.Form 包含了 Query 和 Body 的参数，Body 优先
	if err := SchemaDecoder.Decode(v, r.Form); err != nil {
		return fmt.Errorf("decode form error: %w", err)
	}

	// 2. 绑定文件
	if r.MultipartForm != nil && len(r.MultipartForm.File) > 0 {
		if err := bindFiles(r, v); err != nil {
			return fmt.Errorf("bind files error: %w", err)
		}
	}

	return nil
}

// bindFiles 使用反射将 multipart 文件绑定到结构体字段
func bindFiles(r *http.Request, v any) error {
	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return nil
	}

	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}

		// 查找 form 标签，如果没有则使用字段名 (snake_case 转换逻辑略，这里简单匹配)
		// 为了兼容性，我们优先看 form 标签，其次 json 标签
		key := field.Tag.Get("form")
		if key == "" {
			key = field.Tag.Get("json")
			// 去掉 json tag 的 options, e.g. "name,omitempty"
			if idx := strings.Index(key, ","); idx != -1 {
				key = key[:idx]
			}
		}
		if key == "" {
			key = field.Name
		}

		// 检查字段类型并赋值
		fieldVal := val.Field(i)
		fieldType := field.Type

		// Case 1: 单个文件 *multipart.FileHeader
		if fieldType == reflect.TypeOf((*multipart.FileHeader)(nil)) {
			file, _, err := r.FormFile(key)
			if err == nil {
				// r.FormFile 返回的是 multipart.File 接口，
				// 但我们需要 *multipart.FileHeader。
				// r.FormFile 内部调用了 MultipartForm.File[key]，我们直接取 FileHeader 更方便。
				if fhs := r.MultipartForm.File[key]; len(fhs) > 0 {
					file.Close() // FormFile 打开了文件，这里我们只需要 Header，所以关掉
					fieldVal.Set(reflect.ValueOf(fhs[0]))
				}
			}
		} else if fieldType == reflect.TypeOf([]*multipart.FileHeader{}) {
			// Case 2: 多个文件 []*multipart.FileHeader
			if fhs := r.MultipartForm.File[key]; len(fhs) > 0 {
				fieldVal.Set(reflect.ValueOf(fhs))
			}
		}
	}
	return nil
}

type QueryBinder struct{}

func (b *QueryBinder) Name() string     { return "query" }
func (b *QueryBinder) Type() BinderType { return BinderMeta }
func (b *QueryBinder) Match(r *http.Request) bool {
	// Query Binder 总是尝试匹配（只要有 Query 参数）
	return len(r.URL.Query()) > 0
}

func (b *QueryBinder) Bind(r *http.Request, v any) error {
	return SchemaDecoder.Decode(v, r.URL.Query())
}

// PathBinder (New Feature)
// 适配 Go 1.22 的 PathValue
type PathBinder struct{}

func (b *PathBinder) Name() string     { return "path" }
func (b *PathBinder) Type() BinderType { return BinderMeta }
func (b *PathBinder) Match(r *http.Request) bool {
	// 总是运行，因为我们无法轻易知道路由是否有 path 参数，
	// 但此操作开销极低（仅反射查找 tag）
	return true
}

func (b *PathBinder) Bind(r *http.Request, v any) error {
	// 由于 schema 库不支持直接从 request pathvalue 获取，
	// 我们需要手动构建一个 map[string][]string 给它。

	values := make(map[string][]string)

	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return nil
	}

	typ := val.Type()
	hasPathTags := false

	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)
		pathKey := field.Tag.Get("path")
		if pathKey == "" {
			continue
		}

		// 从 Go 1.22 request 中获取路径参数
		pathVal := r.PathValue(pathKey)
		if pathVal != "" {
			// 确定 Schema Decoder 使用的 key
			// 优先使用 schema 标签，否则使用字段名
			schemaKey := field.Tag.Get("schema")
			if schemaKey == "" {
				schemaKey = field.Name
			}

			values[schemaKey] = []string{pathVal}
			hasPathTags = true
		}
	}

	if !hasPathTags {
		return nil
	}

	// 复用 SchemaDecoder 进行类型转换 (string -> int/bool etc)
	return SchemaDecoder.Decode(v, values)
}

// Binders 默认链。
// 顺序: Path -> Query -> Json/Form
var Binders = []Binder{
	&PathBinder{},
	&QueryBinder{},
	&JsonBinder{DisallowUnknownFields: true},
	&FormBinder{MaxMemory: DefaultMultipartMemory},
}

// Bind 执行绑定逻辑 (支持协同)
func Bind(r *http.Request, v any, binders ...Binder) error {
	if len(binders) == 0 {
		binders = Binders
	}

	bodyBound := false // 标记 Body 是否已被消耗

	for _, binder := range binders {
		if !binder.Match(r) {
			continue
		}

		// 如果是 Body Binder 且 Body 已经被绑定过，跳过
		if binder.Type() == BinderBody {
			if bodyBound {
				continue
			}
			bodyBound = true
		}

		if err := binder.Bind(r, v); err != nil {
			return err
		}
	}
	return nil
}
