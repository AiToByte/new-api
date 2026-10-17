package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

func Cache() func(c *gin.Context) {
	return func(c *gin.Context) {
		// 构建产物 /static/** 的文件名自带内容哈希，同一 URL 的内容永不变化，
		// 可以一年期不可变缓存；index.html 与 .html 直链必须逐次协商缓存，
		// 否则部署后浏览器会拿到旧页面并引用已不存在的哈希资源；其余路径
		// （favicon 等根目录文件、插件回退响应）保持原有的一周语义。
		if strings.HasPrefix(c.Request.URL.Path, "/static/") {
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
		} else if c.Request.RequestURI == "/" || strings.HasSuffix(c.Request.URL.Path, ".html") {
			c.Header("Cache-Control", "no-cache")
		} else {
			c.Header("Cache-Control", "max-age=604800") // one week
		}
		c.Header("Cache-Version", "b688f2fb5be447c25e5aa3bd063087a83db32a288bf6a4f35f2d8db310e40b14")
		c.Next()
	}
}
