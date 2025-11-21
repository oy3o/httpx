package httpx

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type CustomBinderReq struct {
	Source string
}

type HeaderBinder struct{}

func (b *HeaderBinder) Name() string               { return "header" }
func (b *HeaderBinder) Type() BinderType           { return BinderMeta }
func (b *HeaderBinder) Match(r *http.Request) bool { return true }
func (b *HeaderBinder) Bind(r *http.Request, v any) error {
	if req, ok := v.(*CustomBinderReq); ok {
		req.Source = r.Header.Get("X-Source")
	}
	return nil
}

func TestNewHandler_AddBinders(t *testing.T) {
	handler := func(ctx context.Context, req *CustomBinderReq) (*TestRes, error) {
		return &TestRes{ID: req.Source}, nil
	}

	// 测试添加自定义 Binder
	h := NewHandler(handler, AddBinders(&HeaderBinder{}))

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Source", "custom-binder")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "custom-binder")
}

func TestNewHandler_WithMaxBodySize(t *testing.T) {
	handler := func(ctx context.Context, req *TestReqReflect) (*TestRes, error) {
		return &TestRes{ID: "ok"}, nil
	}

	// 限制 Body 为 5 字节
	h := NewHandler(handler, WithMaxBodySize(5))

	body := `{"name": "very long body"}`
	r := httptest.NewRequest("POST", "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, r)

	// 期望触发 http.MaxBytesReader 的错误
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	assert.Contains(t, w.Body.String(), CodeRequestEntityTooLarge)
}

func TestJsonBinder_EmptyBody(t *testing.T) {
	// 测试空 Body 不应报错
	b := &JsonBinder{}
	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.Header.Set("Content-Type", "application/json")

	var v TestReqReflect
	err := b.Bind(r, &v)
	assert.NoError(t, err)
}

type UnexportedFileReq struct {
	// 未导出字段，应该被 Binder 忽略而不是 Panic
	file   *multipart.FileHeader
	Public *multipart.FileHeader `form:"file"`
}

func TestFormBinder_UnexportedFields(t *testing.T) {
	// 构造 Multipart 请求
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "test.txt")
	part.Write([]byte("content"))
	writer.Close()

	r := httptest.NewRequest("POST", "/", body)
	r.Header.Set("Content-Type", writer.FormDataContentType())

	var req UnexportedFileReq
	binder := &FormBinder{}

	// 触发 ParseMultipartForm
	err := binder.Bind(r, &req)
	require.NoError(t, err)

	// 验证导出字段被绑定
	assert.NotNil(t, req.Public)
	assert.Equal(t, "test.txt", req.Public.Filename)

	// 验证未导出字段保持 nil (且未发生 Panic)
	assert.Nil(t, req.file)
}

type PathParamReq struct {
	ID   int    `path:"id"`
	Slug string `path:"slug"`
	Type string `path:"type" schema:"type_override"` // 测试 schema tag 映射
}

func TestPathBinder(t *testing.T) {
	// 模拟 Path Parameter
	handlerFunc := func(ctx context.Context, req *PathParamReq) (*TestRes, error) {
		assert.Equal(t, 101, req.ID)
		assert.Equal(t, "my-article", req.Slug)
		assert.Equal(t, "news", req.Type)
		return &TestRes{ID: "ok"}, nil
	}

	h := NewHandler(handlerFunc)

	req := httptest.NewRequest("GET", "/articles/news/101/my-article", nil)

	// 在 Go 1.22 中，httptest.NewRequest 不会自动解析路由参数，
	// 我们需要手动设置 SetPathValue 来模拟路由器的行为
	req.SetPathValue("id", "101")
	req.SetPathValue("slug", "my-article")
	req.SetPathValue("type", "news")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

type FileUploadReq struct {
	Title string                `form:"title"`
	File  *multipart.FileHeader `form:"file"`
}

func TestMultipart_LargeFile(t *testing.T) {
	// 1. 准备测试数据
	// 模拟一个大文件内容 (10MB)
	fileSize := 10 * 1024 * 1024
	largeData := make([]byte, fileSize)
	largeData[0] = 'H'
	largeData[fileSize-1] = '!'

	// 2. 构造 Multipart 请求 Body
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// 使用 form 标签匹配 struct
	_ = writer.WriteField("title", "My Large File")

	part, err := writer.CreateFormFile("file", "large.dat")
	require.NoError(t, err)
	_, err = part.Write(largeData)
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// 3. 定义 Handler
	handlerFunc := func(ctx context.Context, req *FileUploadReq) (*TestRes, error) {
		assert.Equal(t, "My Large File", req.Title)
		// 文件绑定应该也工作
		require.NotNil(t, req.File)
		return &TestRes{ID: "uploaded"}, nil
	}

	// 4. 配置 Handler，限制内存为 1MB
	h := NewHandler(handlerFunc, WithMultipartLimit(1*1024*1024), WithMaxBodySize(2*int64(fileSize)))

	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "uploaded")
}

func TestMultipart_LimitOptionVerify(t *testing.T) {
	c := &config{
		binders: []Binder{
			&PathBinder{},
			&QueryBinder{},
			&JsonBinder{},
			&FormBinder{MaxMemory: 32 << 20}, // 初始值
		},
	}

	// 应用 Option，修改为 1024 字节
	opt := WithMultipartLimit(1024)
	opt(c)

	var fb *FormBinder
	for _, b := range c.binders {
		if f, ok := b.(*FormBinder); ok {
			fb = f
			break
		}
	}

	require.NotNil(t, fb, "FormBinder should exist")
	assert.Equal(t, int64(1024), fb.MaxMemory, "MaxMemory should be updated")

	// 验证没有破坏其他 Binder
	assert.Len(t, c.binders, 4)
	_, isPath := c.binders[0].(*PathBinder)
	assert.True(t, isPath)
}

func TestMultipart_FileBinding(t *testing.T) {
	// 1. 准备 Multipart Body
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// 添加普通字段
	_ = writer.WriteField("title", "Doc1")

	// 添加文件字段
	part, err := writer.CreateFormFile("file", "test.txt")
	require.NoError(t, err)
	_, err = part.Write([]byte("hello world"))
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// 2. 构造 Handler
	handlerFunc := func(ctx context.Context, req *FileUploadReq) (*TestRes, error) {
		assert.Equal(t, "Doc1", req.Title)
		require.NotNil(t, req.File)
		assert.Equal(t, "test.txt", req.File.Filename)
		assert.Equal(t, int64(11), req.File.Size)
		return &TestRes{ID: "ok"}, nil
	}

	h := NewHandler(handlerFunc)

	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
