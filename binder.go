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
	// 使用 int 指针或 -1 代表不存在，这里为了内存对齐和简单，使用 int，初始化为 -1
	clientIDIdx     int
	clientSecretIdx int

	// formKeys collecting for No-Vary-Search
	formKeys []string
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
	meta := &structMeta{
		// 初始化为 -1，避免 0 索引误判
		clientIDIdx:     -1,
		clientSecretIdx: -1,
	}

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

			// 识别 ClientID 和 ClientSecret
			// 逻辑：如果 form/json tag 声明为 "client_id" 或 "client_secret"，则认为是目标字段
			// 这符合 OIDC 协议规范，也兼容结构体定义
			if meta.clientIDIdx == -1 && mapKey == "client_id" && field.Type.Kind() == reflect.String {
				meta.clientIDIdx = i
			}
			if meta.clientSecretIdx == -1 && mapKey == "client_secret" && field.Type.Kind() == reflect.String {
				meta.clientSecretIdx = i
			}

			// 收集 Query 参数用于 No-Vary-Search
			meta.formKeys = append(meta.formKeys, mapKey)

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
