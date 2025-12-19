package httpx

import (
	"net/http"
	"reflect"
	"strings"
)

// ClientAuthBinder 处理 HTTP Basic Auth
// 专门用于 OIDC/OAuth2 场景，自动提取 client_id 和 client_secret
type ClientAuthBinder struct{}

func (b *ClientAuthBinder) Name() string     { return "client_auth" }
func (b *ClientAuthBinder) Type() BinderType { return BinderMeta } // 属于元数据类 (Header)

func (b *ClientAuthBinder) Match(r *http.Request) bool {
	// 快速检查 Header 是否存在，避免无意义的解析
	auth := r.Header.Get("Authorization")
	return len(auth) >= 6 && strings.EqualFold(auth[:6], "Basic ")
}

func (b *ClientAuthBinder) Bind(r *http.Request, v any) error {
	// 1. 解析 Basic Auth
	// 标准库 BasicAuth 已经处理了 base64 解码
	uid, pwd, ok := r.BasicAuth()
	if !ok {
		return nil
	}

	// 2. 获取结构体值与元数据
	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return nil
	}

	// O(1) 获取缓存
	meta := getStructMeta(val.Type())

	// 3. 填充 ClientID (如果结构体中对应字段为空)
	if meta.clientIDIdx != -1 && uid != "" {
		field := val.Field(meta.clientIDIdx)
		if field.CanSet() && field.String() == "" {
			field.SetString(uid)
		}
	}

	// 4. 填充 ClientSecret (如果结构体中对应字段为空)
	if meta.clientSecretIdx != -1 && pwd != "" {
		field := val.Field(meta.clientSecretIdx)
		if field.CanSet() && field.String() == "" {
			field.SetString(pwd)
		}
	}

	return nil
}
