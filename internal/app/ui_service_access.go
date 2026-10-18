package app

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
)

func serviceSettingsPath(path string) bool {
	if strings.HasPrefix(path, "/api/ui/settings/") {
		return true
	}
	switch path {
	case "/api/ui/config", "/api/ui/network/check", "/api/ui/directory/pick", "/api/ui/ffmpeg":
		return true
	}
	return false
}

func (app *UIApp) withServiceAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		settings := serviceSettingsPath(request.URL.Path)
		writeAPI := request.Method != http.MethodGet && request.Method != http.MethodHead &&
			(strings.HasPrefix(request.URL.Path, "/api/ui/") || request.URL.Path == "/api/emby/export")
		if settings || writeAPI {
			writer.Header().Set("Cache-Control", "private, no-store")
			// long: 下载、删除和合并同样会改动共享数据，免登录后也必须阻止第三方网页借浏览器提交修改。
			if !playbackRequestAllowed(writer, request, request.Method) {
				return
			}
			if writeAPI {
				contentType, _, _ := mime.ParseMediaType(request.Header.Get("Content-Type"))
				if contentType != "application/json" {
					writeJSON(writer, http.StatusUnsupportedMediaType, map[string]string{"error": "请求必须使用 JSON"})
					return
				}
			}
			if settings && writeAPI && request.Header.Get("X-Duanju-Viewer") == "" {
				writeJSON(writer, http.StatusForbidden, map[string]string{"error": "请从剧库设置面板操作"})
				return
			}
		}
		next.ServeHTTP(writer, request)
	})
}

func readServiceRequest(writer http.ResponseWriter, request *http.Request, input any) bool {
	if !playbackRequestAllowed(writer, request, http.MethodPost) {
		return false
	}
	if request.Header.Get("X-Duanju-Viewer") == "" {
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "请从剧库设置面板操作"})
		return false
	}
	contentType, _, _ := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if contentType != "application/json" {
		writeJSON(writer, http.StatusUnsupportedMediaType, map[string]string{"error": "设置请求必须使用 JSON"})
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(input) != nil || decoder.Decode(new(any)) != io.EOF {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "设置请求无效"})
		return false
	}
	return requestViewer(writer, request) != nil
}

func (manager *viewerManager) setGuestCookie(writer http.ResponseWriter, request *http.Request, identity string) {
	expires := time.Now().Add(viewerLifetime)
	http.SetCookie(writer, &http.Cookie{Name: manager.cookie, Value: manager.token(identity, expires), Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: request.TLS != nil || strings.EqualFold(request.Header.Get("X-Forwarded-Proto"), "https"), MaxAge: int(viewerLifetime / time.Second), Expires: expires})
}
