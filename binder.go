package httpx

import (
	"mime/multipart"
	"net/http"
	"reflect"
	"strings"

	"github.com/gorilla/schema"
	"github.com/puzpuzpuz/xsync/v4"
)

// 全局配置与基础组件

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

// 结构体元数据缓存 (Type Caching)

// structMeta 保存结构体的静态分析结果，避免每次请求都进行反射遍历
type structMeta struct {
	// pathFields: 标记了 `path` tag 的字段
	pathFields []pathFieldInfo
	// fileFields: 类型为 *multipart.FileHeader 或 []*multipart.FileHeader 的字段
	fileFields []fileFieldInfo

	// OIDC/OAuth2 专用字段索引
	// 记录嵌套层级的字段索引
	clientIDIdx     []int
	clientSecretIdx []int

	// formKeys collecting for No-Vary-Search
	formKeys []string
}

type pathFieldInfo struct {
	fieldIdx  []int  // 字段在结构体中的索引路径
	pathKey   string // 对应 URL 路径参数的 key (例如 "id")
	schemaKey string // 传给 SchemaDecoder 的 key (即 form tag 或 json tag)
}

type fileFieldInfo struct {
	fieldIdx []int  // 字段索引路径
	formKey  string // 表单中的文件名 key
	isSlice  bool   // 是否是切片 []*multipart.FileHeader
}

// metaCache 缓存 reflect.Type -> *structMeta
var metaCache *xsync.Map[reflect.Type, *structMeta] = xsync.NewMap[reflect.Type, *structMeta]()

// getFieldByIndex 支持多层级安全访问，如果遇到 nil 指针会自动初始化
func getFieldByIndex(v reflect.Value, index []int) reflect.Value {
	for _, i := range index {
		if v.Kind() == reflect.Ptr {
			if v.IsNil() {
				if !v.CanSet() {
					return reflect.Value{}
				}
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		v = v.Field(i)
	}
	return v
}

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
		var walk func(typ reflect.Type, basePath []int)
		walk = func(typ reflect.Type, basePath []int) {
			for i := 0; i < typ.NumField(); i++ {
				field := typ.Field(i)

				idxPath := make([]int, len(basePath)+1)
				copy(idxPath, basePath)
				idxPath[len(basePath)] = i

				fieldType := field.Type
				if fieldType.Kind() == reflect.Ptr {
					fieldType = fieldType.Elem()
				}

				// 处理匿名嵌套结构体
				if field.Anonymous && fieldType.Kind() == reflect.Struct {
					walk(fieldType, idxPath)
					continue
				}

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

				// 识别 ClientID 和 ClientSecret
				// 逻辑：如果 form/json tag 声明为 "client_id" 或 "client_secret"，则认为是目标字段
				if meta.clientIDIdx == nil && mapKey == "client_id" && field.Type.Kind() == reflect.String {
					meta.clientIDIdx = idxPath
				}
				if meta.clientSecretIdx == nil && mapKey == "client_secret" && field.Type.Kind() == reflect.String {
					meta.clientSecretIdx = idxPath
				}

				// 收集 Query 参数用于 No-Vary-Search
				meta.formKeys = append(meta.formKeys, mapKey)

				// A. 解析 Path 参数元数据
				pathKey := field.Tag.Get("path")
				if pathKey != "" {
					meta.pathFields = append(meta.pathFields, pathFieldInfo{
						fieldIdx:  idxPath,
						pathKey:   pathKey,
						schemaKey: mapKey,
					})
				}

				// B. 解析文件上传元数据
				if field.Type == reflect.TypeOf((*multipart.FileHeader)(nil)) {
					meta.fileFields = append(meta.fileFields, fileFieldInfo{
						fieldIdx: idxPath,
						formKey:  mapKey,
						isSlice:  false,
					})
				} else if field.Type == reflect.TypeOf([]*multipart.FileHeader{}) {
					meta.fileFields = append(meta.fileFields, fileFieldInfo{
						fieldIdx: idxPath,
						formKey:  mapKey,
						isSlice:  true,
					})
				}
			}
		}
		walk(t, nil)
	}

	// 3. 写入缓存
	actual, _ := metaCache.LoadOrStore(t, meta)
	return actual
}

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
