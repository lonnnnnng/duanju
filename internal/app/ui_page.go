package app

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"

	"duanju/internal/webui"
)

var uiHTML = webui.HTML

// long: Web UI 需要从站源加载图片、媒体和 HLS Worker；策略只放行这些资源类型，同时禁止第三方脚本和页面嵌入。
const uiContentSecurityPolicy = "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: http: https:; media-src 'self' blob: http: https:; connect-src 'self' http: https:; worker-src 'self' blob:"

func setWebUIHeaders(header http.Header) {
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
}

func webAssets() http.Handler {
	tags := map[string]string{}
	entries, _ := webui.Assets.ReadDir(".")
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		body, err := webui.Assets.ReadFile(entry.Name())
		if err == nil {
			tags["/assets/"+entry.Name()] = fmt.Sprintf(`"%x"`, sha256.Sum256(body))
		}
	}
	files := http.StripPrefix("/assets/", http.FileServer(http.FS(webui.Assets)))
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		setWebUIHeaders(writer.Header())
		writer.Header().Set("Cache-Control", "no-cache")
		if tag := tags[request.URL.Path]; tag != "" {
			writer.Header().Set("ETag", tag)
			if request.Method == http.MethodGet || request.Method == http.MethodHead {
				for _, candidate := range strings.Split(request.Header.Get("If-None-Match"), ",") {
					candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
					if candidate == tag || candidate == "*" {
						writer.WriteHeader(http.StatusNotModified)
						return
					}
				}
			}
		}
		files.ServeHTTP(writer, request)
	})
}
