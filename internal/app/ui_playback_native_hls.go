package app

import (
	"bytes"
	"context"
	"crypto/aes"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// nativeHLSSession 是一次已完成「解密 + 拷贝封装」的 HLS 结果，供原生播放器直接拉取。
// 服务器只做 AES-CTR 解密和 `-c copy` 转封装，不重编码，因此不会消耗转码级 CPU。
type nativeHLSSession struct {
	id    string
	dir   string
	media providerMedia
}

const nativeHLSIdleTimeout = 20 * time.Minute
const nativeHLSMaxSessions = 8

func (app *UIApp) handlePlaybackNativeStream(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		DramaID string `json:"dramaId"`
		Episode int    `json:"episode"`
	}
	if !readPlaybackRequest(writer, request, &input) {
		return
	}
	if input.Episode < 1 {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "集数无效"})
		return
	}
	if requestViewer(writer, request) == nil {
		return
	}
	source, seriesID, valid := splitProviderDramaID(input.DramaID)
	if !valid || source != sourceHongguo {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "剧集 ID 无效"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 3*time.Minute)
	defer cancel()
	title, chapters, err := app.downloader.GetDramaChapters(ctx, seriesID)
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "获取选集失败：" + app.redactError(err)})
		return
	}
	if input.Episode > len(chapters) {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "集数超出范围"})
		return
	}
	videoID := strings.TrimPrefix(chapters[input.Episode-1].VideoURL, "hongguo-cenc://")
	if !hongguoNumericID.MatchString(videoID) {
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "章节媒体 ID 无效，请刷新剧库后重试"})
		return
	}
	media, err := app.downloader.resolveHongguoAppMedia(ctx, videoID)
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "解析播放地址失败：" + app.redactError(err)})
		return
	}
	if !isProviderHTTPMediaURL(media.URL) || len(media.CENCKey) != aes.BlockSize {
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "未返回该集可用的媒体和密钥"})
		return
	}
	ffmpeg, err := app.downloader.ensureFFmpeg(ctx)
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	directory, err := os.MkdirTemp("", "duanju-native-hls-")
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "无法准备媒体目录"})
		return
	}
	command := ffmpegMediaCommand(ctx, ffmpeg, playbackNativeHLSArgs(media, directory)...)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		_ = os.RemoveAll(directory)
		detail := output.String()
		if len(detail) > 2000 {
			detail = detail[len(detail)-2000:]
		}
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "媒体封装失败：" + app.redactError(fmt.Errorf("%s", detail))})
		return
	}
	if _, err := os.Stat(filepath.Join(directory, "index.m3u8")); err != nil {
		_ = os.RemoveAll(directory)
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "未生成播放列表"})
		return
	}
	id := randomHex(24)
	if id == "" {
		_ = os.RemoveAll(directory)
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "无法创建播放会话"})
		return
	}
	app.nativeHLSMu.Lock()
	if app.nativeHLS == nil {
		app.nativeHLS = map[string]*nativeHLSSession{}
	}
	if len(app.nativeHLS) >= nativeHLSMaxSessions {
		for key, session := range app.nativeHLS {
			_ = os.RemoveAll(session.dir)
			delete(app.nativeHLS, key)
			break
		}
	}
	app.nativeHLS[id] = &nativeHLSSession{id: id, dir: directory, media: media}
	app.nativeHLSMu.Unlock()
	time.AfterFunc(nativeHLSIdleTimeout, func() { app.removeNativeHLS(id) })
	writeJSON(writer, http.StatusOK, map[string]any{
		"url":      "/api/ui/playback/native/hls/" + id + "/index.m3u8",
		"duration": media.Duration.Seconds(),
		"quality":  media.Quality,
		"title":    title,
	})
}

func (app *UIApp) removeNativeHLS(id string) {
	app.nativeHLSMu.Lock()
	session := app.nativeHLS[id]
	delete(app.nativeHLS, id)
	app.nativeHLSMu.Unlock()
	if session != nil {
		_ = os.RemoveAll(session.dir)
	}
}

func (app *UIApp) handlePlaybackNativeHLSAsset(writer http.ResponseWriter, request *http.Request) {
	method := http.MethodGet
	if request.Method == http.MethodHead {
		method = http.MethodHead
	}
	if !playbackRequestAllowed(writer, request, method) {
		return
	}
	rest := strings.TrimPrefix(request.URL.Path, "/api/ui/playback/native/hls/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "资源地址无效"})
		return
	}
	id, name := parts[0], parts[1]
	app.nativeHLSMu.Lock()
	session := app.nativeHLS[id]
	app.nativeHLSMu.Unlock()
	if session == nil {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "此播放流已结束，请重新选择分集"})
		return
	}
	if !isNativeHLSFileName(name) {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "资源名称无效"})
		return
	}
	path := filepath.Join(session.dir, name)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "资源不存在"})
		return
	}
	file, err := os.Open(path)
	if err != nil {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "资源不可读"})
		return
	}
	defer file.Close()
	contentType := "application/vnd.apple.mpegurl"
	if name == "init.mp4" || strings.HasSuffix(name, ".m4s") {
		contentType = "video/mp4"
	}
	writer.Header().Set("Content-Type", contentType)
	http.ServeContent(writer, request, name, info.ModTime(), file)
}

func isNativeHLSFileName(name string) bool {
	switch name {
	case "index.m3u8", "init.mp4":
		return true
	}
	base, ok := strings.CutSuffix(name, ".m4s")
	if !ok || len(base) != 6 {
		return false
	}
	for index := 0; index < len(base); index++ {
		if base[index] < '0' || base[index] > '9' {
			return false
		}
	}
	return true
}

func playbackNativeHLSArgs(media providerMedia, directory string) []string {
	return []string{
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-rw_timeout", "20000000", "-protocol_whitelist", "http,https,tcp,tls,crypto,httpproxy",
		"-decryption_key", hex.EncodeToString(media.CENCKey),
		"-i", media.URL,
		"-map", "0:v:0", "-map", "0:a:0?",
		"-c", "copy",
		"-avoid_negative_ts", "make_zero",
		"-f", "hls", "-hls_time", "2", "-hls_list_size", "0",
		"-hls_playlist_type", "vod", "-hls_segment_type", "fmp4",
		"-hls_fmp4_init_filename", "init.mp4",
		"-hls_flags", "independent_segments",
		"-hls_segment_filename", filepath.ToSlash(filepath.Join(directory, "%06d.m4s")),
		filepath.ToSlash(filepath.Join(directory, "index.m3u8")),
	}
}
