package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingFrontendFS struct {
	static.ServeFileSystem
	opens atomic.Int32
}

func (f *countingFrontendFS) Open(name string) (http.File, error) {
	f.opens.Add(1)
	return f.ServeFileSystem.Open(name)
}

func performFrontendRequest(router http.Handler, method, target, acceptEncoding string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, target, nil)
	if acceptEncoding != "" {
		request.Header.Set("Accept-Encoding", acceptEncoding)
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestServeFrontendFilesCompressesEachFileOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	frontendDir := t.TempDir()
	script := []byte(strings.Repeat("export const answer = 42;\n", 400))
	image := []byte("\x89PNG\r\n\x1a\nnot-really-an-image")
	require.NoError(t, os.MkdirAll(filepath.Join(frontendDir, "static", "js"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(frontendDir, "static", "js", "app.js"), script, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(frontendDir, "logo.png"), image, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(frontendDir, "index.html"), []byte("<html></html>"), 0o644))
	frontendFS := &countingFrontendFS{ServeFileSystem: static.LocalFile(frontendDir, false)}

	router := gin.New()
	router.NoRoute(ServeFrontendFiles(frontendFS), func(c *gin.Context) {
		c.String(http.StatusTeapot, "fallback")
	})

	plain := performFrontendRequest(router, http.MethodGet, "/static/js/app.js", "")
	require.Equal(t, http.StatusOK, plain.Code)
	assert.Empty(t, plain.Header().Get("Content-Encoding"))
	assert.Equal(t, script, plain.Body.Bytes())
	frontendFS.opens.Store(0)

	responses := make([]*httptest.ResponseRecorder, 8)
	var wg sync.WaitGroup
	for i := range responses {
		wg.Go(func() {
			responses[i] = performFrontendRequest(router, http.MethodGet, "/static/js/app.js", "gzip, deflate, br")
		})
	}
	wg.Wait()
	for _, response := range responses {
		require.Equal(t, http.StatusOK, response.Code)
		// 客户端同时声明 br 与 gzip 时，优先返回体积更小的 brotli。
		assert.Equal(t, "br", response.Header().Get("Content-Encoding"))
		assert.Equal(t, "Accept-Encoding", response.Header().Get("Vary"))
		assert.Equal(t, plain.Header().Get("Content-Type"), response.Header().Get("Content-Type"))
		assert.Equal(t, strconv.Itoa(response.Body.Len()), response.Header().Get("Content-Length"))
		assert.Equal(t, responses[0].Body.Bytes(), response.Body.Bytes())
	}
	brDecoded, err := io.ReadAll(brotli.NewReader(bytes.NewReader(responses[0].Body.Bytes())))
	require.NoError(t, err)
	assert.Equal(t, script, brDecoded)

	// 只声明 gzip 的客户端拿到 gzip，且体积应大于 brotli。
	gzipResponse := performFrontendRequest(router, http.MethodGet, "/static/js/app.js", "gzip")
	require.Equal(t, "gzip", gzipResponse.Header().Get("Content-Encoding"))
	assert.Less(t, responses[0].Body.Len(), gzipResponse.Body.Len(), "brotli must produce a smaller body than gzip")
	gzipReader, err := gzip.NewReader(bytes.NewReader(gzipResponse.Body.Bytes()))
	require.NoError(t, err)
	gzipDecoded, err := io.ReadAll(gzipReader)
	require.NoError(t, err)
	assert.Equal(t, script, gzipDecoded)

	head := performFrontendRequest(router, http.MethodHead, "/static/js/app.js", "gzip")
	assert.Equal(t, http.StatusOK, head.Code)
	assert.Equal(t, "gzip", head.Header().Get("Content-Encoding"))
	assert.Equal(t, strconv.Itoa(gzipResponse.Body.Len()), head.Header().Get("Content-Length"))
	assert.Empty(t, head.Body.Bytes())
	assert.Equal(t, int32(1), frontendFS.opens.Load(), "the file must be read and compressed once")

	png := performFrontendRequest(router, http.MethodGet, "/logo.png", "gzip")
	assert.Equal(t, http.StatusOK, png.Code)
	assert.Empty(t, png.Header().Get("Content-Encoding"))
	assert.Equal(t, image, png.Body.Bytes())

	index := performFrontendRequest(router, http.MethodGet, "/index.html", "gzip")
	assert.Equal(t, http.StatusMovedPermanently, index.Code)
	assert.Equal(t, "./", index.Header().Get("Location"))

	missing := performFrontendRequest(router, http.MethodGet, "/static/js/missing.js", "gzip")
	assert.Equal(t, http.StatusTeapot, missing.Code)
	assert.Equal(t, "fallback", missing.Body.String())
}

func TestServeFrontendFilesEncodingNegotiation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	frontendDir := t.TempDir()
	script := []byte(strings.Repeat("const value = 1;\n", 200))
	require.NoError(t, os.MkdirAll(filepath.Join(frontendDir, "static", "js"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(frontendDir, "static", "js", "app.js"), script, 0o644))

	router := gin.New()
	router.NoRoute(ServeFrontendFiles(static.LocalFile(frontendDir, false)))

	cases := []struct {
		name           string
		acceptEncoding string
		want           string
	}{
		{"brotli preferred over gzip", "gzip, deflate, br, zstd", "br"},
		{"brotli refused falls back to gzip", "br;q=0, gzip", "gzip"},
		{"gzip only", "gzip", "gzip"},
		{"wildcard allows brotli", "*", "br"},
		{"unsupported encoding serves raw", "deflate", ""},
		{"missing header serves raw", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := performFrontendRequest(router, http.MethodGet, "/static/js/app.js", tc.acceptEncoding)
			require.Equal(t, http.StatusOK, response.Code)
			assert.Equal(t, tc.want, response.Header().Get("Content-Encoding"))
			if tc.want == "" {
				assert.Equal(t, script, response.Body.Bytes())
				return
			}
			assert.Equal(t, "Accept-Encoding", response.Header().Get("Vary"))
		})
	}
}

func TestServeFrontendFilesETagAndConditionalRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	frontendDir := t.TempDir()
	script := []byte(strings.Repeat("const value = 2;\n", 200))
	require.NoError(t, os.MkdirAll(filepath.Join(frontendDir, "static", "js"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(frontendDir, "static", "js", "app.a1b2c3d4e5.js"), script, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(frontendDir, "static", "js", "app.js"), script, 0o644))

	router := gin.New()
	router.NoRoute(ServeFrontendFiles(static.LocalFile(frontendDir, false)))

	// 哈希命名的文件按编码区分校验器：br 与 gzip 的 ETag 必须不同，
	// 客户端切换 Accept-Encoding 重新协商时会重新拉取对应表示。
	br := performFrontendRequest(router, http.MethodGet, "/static/js/app.a1b2c3d4e5.js", "br")
	require.Equal(t, http.StatusOK, br.Code)
	assert.Equal(t, `"a1b2c3d4e5-br"`, br.Header().Get("ETag"))
	gz := performFrontendRequest(router, http.MethodGet, "/static/js/app.a1b2c3d4e5.js", "gzip")
	require.Equal(t, http.StatusOK, gz.Code)
	assert.Equal(t, `"a1b2c3d4e5-gzip"`, gz.Header().Get("ETag"))
	assert.NotEqual(t, br.Header().Get("ETag"), gz.Header().Get("ETag"))

	// If-None-Match 命中当前编码的校验器时返回 304，且不携带表示头。
	conditional := httptest.NewRequest(http.MethodGet, "/static/js/app.a1b2c3d4e5.js", nil)
	conditional.Header.Set("Accept-Encoding", "br")
	conditional.Header.Set("If-None-Match", `"a1b2c3d4e5-br"`)
	notModified := httptest.NewRecorder()
	router.ServeHTTP(notModified, conditional)
	require.Equal(t, http.StatusNotModified, notModified.Code)
	assert.Empty(t, notModified.Body.Bytes())
	assert.Equal(t, `"a1b2c3d4e5-br"`, notModified.Header().Get("ETag"))
	assert.Empty(t, notModified.Header().Get("Content-Encoding"))
	assert.Empty(t, notModified.Header().Get("Content-Length"))

	// 校验器不匹配（例如部署后哈希变化）时返回完整新表示。
	stale := httptest.NewRequest(http.MethodGet, "/static/js/app.a1b2c3d4e5.js", nil)
	stale.Header.Set("Accept-Encoding", "br")
	stale.Header.Set("If-None-Match", `"deadbeef00-br"`)
	fresh := httptest.NewRecorder()
	router.ServeHTTP(fresh, stale)
	require.Equal(t, http.StatusOK, fresh.Code)
	assert.Equal(t, "br", fresh.Header().Get("Content-Encoding"))
	assert.NotEmpty(t, fresh.Body.Bytes())

	// 未压缩路径把裸哈希交给 http.FileServer，由它完成 304。
	raw := performFrontendRequest(router, http.MethodGet, "/static/js/app.a1b2c3d4e5.js", "")
	require.Equal(t, http.StatusOK, raw.Code)
	assert.Equal(t, `"a1b2c3d4e5"`, raw.Header().Get("ETag"))
	rawConditional := httptest.NewRequest(http.MethodGet, "/static/js/app.a1b2c3d4e5.js", nil)
	rawConditional.Header.Set("If-None-Match", `"a1b2c3d4e5"`)
	rawNotModified := httptest.NewRecorder()
	router.ServeHTTP(rawNotModified, rawConditional)
	assert.Equal(t, http.StatusNotModified, rawNotModified.Code)

	// 非哈希命名的文件不设置 ETag，按协商编码返回完整内容。
	unhashed := performFrontendRequest(router, http.MethodGet, "/static/js/app.js", "br")
	require.Equal(t, http.StatusOK, unhashed.Code)
	assert.Empty(t, unhashed.Header().Get("ETag"))
	assert.Equal(t, "br", unhashed.Header().Get("Content-Encoding"))
	assert.NotEmpty(t, unhashed.Body.Bytes())
}

func TestCacheMiddlewareAssetPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(Cache())
	router.NoRoute(func(c *gin.Context) {
		c.String(http.StatusOK, "served")
	})

	cases := []struct {
		path string
		want string
	}{
		// 构建产物文件名自带内容哈希，同一 URL 内容永不变化。
		{"/static/js/app.a1b2c3d4e5.js", "public, max-age=31536000, immutable"},
		{"/static/font/lora.f60a385cfb.woff2", "public, max-age=31536000, immutable"},
		// 首页与 .html 直链必须逐次协商，避免部署后读到旧页面。
		{"/", "no-cache"},
		{"/index.html", "no-cache"},
		// 其余路径（favicon 等根目录文件、回退响应）保持一周语义。
		{"/favicon.ico", "max-age=604800"},
		{"/unknown", "max-age=604800"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			response := performFrontendRequest(router, http.MethodGet, tc.path, "")
			require.Equal(t, http.StatusOK, response.Code)
			assert.Equal(t, tc.want, response.Header().Get("Cache-Control"))
			assert.NotEmpty(t, response.Header().Get("Cache-Version"))
		})
	}
}
