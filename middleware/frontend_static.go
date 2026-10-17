package middleware

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/andybalholm/brotli"
	ginGzip "github.com/gin-contrib/gzip"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
)

// precompressedFile 保存前端构建产物中某个文件的两种压缩表示。
// 构建产物在进程存活期间不会变化，因此每个文件只读入并压缩一次，
// 之后复用缓存的字节，避免每次请求都重新压缩数 MB 的 chunk。
type precompressedFile struct {
	once        sync.Once
	brBody      []byte
	gzipBody    []byte
	contentType string
	err         error
}

// ServeFrontendFiles 从内嵌的前端构建产物中提供静态文件。
// 对支持压缩的请求，优先返回 brotli（体积最小），仅当客户端不支持 br 时
// 回退到 gzip；两者都在首次请求时一次性生成并缓存。
// 客户端未声明任何受支持的编码、属于 gzip 中间件本就跳过的图片类型、目录、
// 以及 index.html 重定向，仍交由 http.FileServer 处理。
func ServeFrontendFiles(frontendFS static.ServeFileSystem) gin.HandlerFunc {
	fileServer := http.FileServer(frontendFS)
	var cache sync.Map // URL path -> *precompressedFile
	return func(c *gin.Context) {
		urlPath := c.Request.URL.Path
		if !frontendFS.Exists("/", urlPath) {
			return
		}
		c.Abort()
		// brotli 压缩率优于 gzip，因此只要客户端接受 br 就优先使用它。
		acceptEncoding := c.GetHeader("Accept-Encoding")
		useBrotli := acceptsEncoding(acceptEncoding, "br")
		useGzip := !useBrotli && acceptsEncoding(acceptEncoding, "gzip")
		if (!useBrotli && !useGzip) ||
			ginGzip.DefaultExcludedExtentions.Contains(path.Ext(urlPath)) ||
			strings.HasSuffix(urlPath, "/index.html") {
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}

		value, ok := cache.Load(urlPath)
		if !ok {
			value, _ = cache.LoadOrStore(urlPath, &precompressedFile{})
		}
		file := value.(*precompressedFile)
		file.once.Do(func() {
			f, err := frontendFS.Open(urlPath)
			if err != nil {
				file.err = err
				return
			}
			defer f.Close()
			info, err := f.Stat()
			if err != nil {
				file.err = err
				return
			}
			if info.IsDir() {
				file.err = errors.New("directory")
				return
			}
			raw, err := io.ReadAll(f)
			if err != nil {
				file.err = err
				return
			}
			// 同一次读入内生成两种编码：br 供支持的客户端，gzip 作为回退。
			// 两者随文件生命周期缓存，因此这里多算一次 brotli 的成本只发生一次。
			var compressed bytes.Buffer
			gzipWriter := gzip.NewWriter(&compressed)
			if _, err := gzipWriter.Write(raw); err != nil {
				file.err = err
				return
			}
			if err := gzipWriter.Close(); err != nil {
				file.err = err
				return
			}
			file.gzipBody = bytes.Clone(compressed.Bytes())

			compressed.Reset()
			brWriter := brotli.NewWriter(&compressed)
			if _, err := brWriter.Write(raw); err != nil {
				file.err = err
				return
			}
			if err := brWriter.Close(); err != nil {
				file.err = err
				return
			}
			file.brBody = bytes.Clone(compressed.Bytes())

			// 与 http.FileServer 保持一致：先按扩展名推断，再回退到内容嗅探。
			file.contentType = mime.TypeByExtension(path.Ext(urlPath))
			if file.contentType == "" {
				file.contentType = http.DetectContentType(raw)
			}
		})
		if file.err != nil {
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}

		encoding := "gzip"
		body := file.gzipBody
		if useBrotli {
			encoding = "br"
			body = file.brBody
		}

		// Range 请求返回完整内容；服务端可以忽略 Range。
		header := c.Writer.Header()
		header.Set("Content-Encoding", encoding)
		header.Set("Vary", "Accept-Encoding")
		header.Set("Content-Type", file.contentType)
		header.Set("Content-Length", strconv.Itoa(len(body)))
		c.Writer.WriteHeader(http.StatusOK)
		if c.Request.Method != http.MethodHead {
			_, _ = c.Writer.Write(body)
		}
	}
}

// acceptsEncoding 判断 Accept-Encoding 头是否允许指定的内容编码。
// 它同时处理列举形式（"gzip, deflate, br"）、通配符 "*"，以及 ";q=0"
// 表示的明确拒绝，与浏览器和 HTTP 客户端的协商语义保持一致。
func acceptsEncoding(header, encoding string) bool {
	header = strings.TrimSpace(header)
	if header == "" {
		return false
	}
	wildcardAllowed := false
	for part := range strings.SplitSeq(header, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		name = strings.ToLower(strings.TrimSpace(name))
		quality := 1.0
		if params != "" {
			// 只解析 q 参数，其余参数（例如 identity）在此无关。
			for param := range strings.SplitSeq(params, ";") {
				key, value, found := strings.Cut(strings.TrimSpace(param), "=")
				if !found || !strings.EqualFold(strings.TrimSpace(key), "q") {
					continue
				}
				if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
					quality = parsed
				}
			}
		}
		if name == encoding {
			// 精确匹配优先于通配符，且 q=0 视为拒绝。
			return quality > 0
		}
		if name == "*" {
			wildcardAllowed = quality > 0
		}
	}
	return wildcardAllowed
}
