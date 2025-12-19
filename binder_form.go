package httpx

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
)

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
