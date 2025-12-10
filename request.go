package httpx

import (
	"fmt"
	"mime/multipart"
	"net/http"
	"reflect"
	"strings"

	"github.com/bytedance/sonic"
	"github.com/gorilla/schema"
	"github.com/puzpuzpuz/xsync/v4"
)

// --- 1. 全局配置与基础组件 ---

// SchemaDecoder 用于 Query 和 Form 的解码
// gorilla/schema 内部已有缓存机制，性能良好
var SchemaDecoder = func() *schema.Decoder {
	d := schema.NewDecoder()
	d.SetAliasTag("form") // 优先读取 form tag
	d.IgnoreUnknownKeys(true)
	return d
}()

// DefaultMultipartMemory 8MB
const DefaultMultipartMemory = 8 << 20

// --- 2. 核心优化：结构体元数据缓存 (Type Caching) ---

// structMeta 保存结构体的静态分析结果，避免每次请求都进行反射遍历
type structMeta struct {
	// pathFields: 标记了 `path` tag 的字段
	pathFields []pathFieldInfo
	// fileFields: 类型为 *multipart.FileHeader 或 []*multipart.FileHeader 的字段
	fileFields []fileFieldInfo
}

type pathFieldInfo struct {
	fieldIdx  int    // 字段在结构体中的索引
	pathKey   string // 对应 URL 路径参数的 key (例如 "id")
	schemaKey string // 传给 SchemaDecoder 的 key (即 form tag 或 json tag)
}

type fileFieldInfo struct {
	fieldIdx int    // 字段索引
	formKey  string // 表单中的文件名 key
	isSlice  bool   // 是否是切片 []*multipart.FileHeader
}

// metaCache 缓存 reflect.Type -> *structMeta
var metaCache *xsync.Map[reflect.Type, *structMeta] = xsync.NewMap[reflect.Type, *structMeta]()

// getStructMeta 获取或解析结构体元数据 (线程安全，只解析一次)
func getStructMeta(t reflect.Type) *structMeta {
	// 始终处理 Struct 类型，解指针
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// 1. 快速路径：缓存命中
	if v, ok := metaCache.Load(t); ok {
		return v
	}

	// 2. 慢速路径：解析结构体
	meta := &structMeta{}
	if t.Kind() == reflect.Struct {
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}

			// 获取通用的映射 Key (form > json > name)
			mapKey := field.Tag.Get("form")
			if idx := strings.Index(mapKey, ","); idx != -1 {
				mapKey = mapKey[:idx]
			}
			if mapKey == "-" {
				continue
			}
			if mapKey == "" {
				mapKey = field.Tag.Get("json")
				if idx := strings.Index(mapKey, ","); idx != -1 {
					mapKey = mapKey[:idx]
				}
			}
			if mapKey == "" {
				mapKey = field.Name
			}

			// A. 解析 Path 参数元数据
			pathKey := field.Tag.Get("path")
			if pathKey != "" {
				meta.pathFields = append(meta.pathFields, pathFieldInfo{
					fieldIdx:  i,
					pathKey:   pathKey,
					schemaKey: mapKey,
				})
			}

			// B. 解析文件上传元数据
			fieldType := field.Type
			if fieldType == reflect.TypeOf((*multipart.FileHeader)(nil)) {
				meta.fileFields = append(meta.fileFields, fileFieldInfo{
					fieldIdx: i,
					formKey:  mapKey,
					isSlice:  false,
				})
			} else if fieldType == reflect.TypeOf([]*multipart.FileHeader{}) {
				meta.fileFields = append(meta.fileFields, fileFieldInfo{
					fieldIdx: i,
					formKey:  mapKey,
					isSlice:  true,
				})
			}
		}
	}

	// 3. 写入缓存
	actual, _ := metaCache.LoadOrStore(t, meta)
	return actual
}

// --- 3. Binder 接口定义 ---

type BinderType int

const (
	BinderMeta BinderType = iota // Query, Header, Path
	BinderBody                   // JSON, XML, Form (互斥)
)

type Binder interface {
	Name() string
	Type() BinderType
	Match(r *http.Request) bool
	Bind(r *http.Request, v any) error
}

// --- 4. Binder 具体实现 ---

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

// FormBinder 处理 multipart/form-data 和 x-www-form-urlencoded
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
	limit := b.MaxMemory
	if limit <= 0 {
		limit = DefaultMultipartMemory
	}

	// 解析表单，注意忽略非 Multipart 错误
	if err := r.ParseMultipartForm(limit); err != nil && err != http.ErrNotMultipart {
		return fmt.Errorf("parse form error: %w", err)
	}

	// 1. 绑定普通文本字段 (schema 库负责)
	if err := SchemaDecoder.Decode(v, r.Form); err != nil {
		return fmt.Errorf("decode form error: %w", err)
	}

	// 2. 绑定文件字段 (使用优化的缓存逻辑)
	if r.MultipartForm != nil && len(r.MultipartForm.File) > 0 {
		if err := bindFilesOptimized(r, v); err != nil {
			return fmt.Errorf("bind files error: %w", err)
		}
	}

	return nil
}

// bindFilesOptimized 利用缓存元数据绑定文件，避免反射遍历所有字段
func bindFilesOptimized(r *http.Request, v any) error {
	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return nil
	}

	// O(1) 获取元数据
	meta := getStructMeta(val.Type())
	if len(meta.fileFields) == 0 {
		return nil
	}

	for _, info := range meta.fileFields {
		files := r.MultipartForm.File[info.formKey]
		if len(files) == 0 {
			continue
		}

		fieldVal := val.Field(info.fieldIdx)
		// 确保字段可设置
		if !fieldVal.CanSet() {
			continue
		}

		if info.isSlice {
			// []*multipart.FileHeader
			fieldVal.Set(reflect.ValueOf(files))
		} else {
			// *multipart.FileHeader (取第一个)
			fieldVal.Set(reflect.ValueOf(files[0]))
		}
	}
	return nil
}

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

// --- 5. 入口函数 ---

// Binders 默认绑定器链
var Binders = []Binder{
	&PathBinder{},
	&QueryBinder{},
	&JsonBinder{DisallowUnknownFields: true},
	&FormBinder{MaxMemory: DefaultMultipartMemory},
}

// Bind 自动选择绑定器处理请求
func Bind(r *http.Request, v any, binders ...Binder) error {
	if len(binders) == 0 {
		binders = Binders
	}

	bodyBound := false // 标记 Body 类绑定器是否已执行

	for _, binder := range binders {
		if !binder.Match(r) {
			continue
		}

		// Body 类绑定器（JSON/Form）互斥，只能执行一次
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
