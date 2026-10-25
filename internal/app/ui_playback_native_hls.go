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
	"sync"
	"time"
)

// nativeHLSSession 是一路「解密 + 拷贝封装」的 HLS 流。服务器只做 AES-CTR 解密和
// `-c copy` 转封装，不重编码；采用渐进式生成：首段可播即返回，后续分片边生成边拉取，
// 避免切集时为下载整集而卡顿数秒。
type nativeHLSSession struct {
	id     string
	dir    string
	media  providerMedia
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
	err    error
}

func (session *nativeHLSSession) setErr(err error) {
	session.mu.Lock()
	if session.err == nil {
		session.err = err
	}
	session.mu.Unlock()
}

func (session *nativeHLSSession) getErr() error {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.err
}

func (session *nativeHLSSession) ended() bool {
	select {
	case <-session.done:
		return true
	default:
		return false
	}
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
	resolveCtx, cancelResolve := context.WithTimeout(request.Context(), 60*time.Second)
	defer cancelResolve()
	title, chapters, err := app.downloader.GetDramaChapters(resolveCtx, seriesID)
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
	media, err := app.downloader.resolveHongguoAppMedia(resolveCtx, videoID)
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "解析播放地址失败：" + app.redactError(err)})
		return
	}
	if !isProviderHTTPMediaURL(media.URL) || len(media.CENCKey) != aes.BlockSize {
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "未返回该集可用的媒体和密钥"})
		return
	}
	ffmpeg, err := app.downloader.ensureFFmpeg(resolveCtx)
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	directory, err := os.MkdirTemp("", "duanju-native-hls-")
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "无法准备媒体目录"})
		return
	}
	id := randomHex(24)
	if id == "" {
		_ = os.RemoveAll(directory)
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "无法创建播放会话"})
		return
	}
	jobCtx, jobCancel := context.WithCancel(context.Background())
	session := &nativeHLSSession{id: id, dir: directory, media: media, cancel: jobCancel, done: make(chan struct{})}
	app.nativeHLSMu.Lock()
	if app.nativeHLS == nil {
		app.nativeHLS = map[string]*nativeHLSSession{}
	}
	if len(app.nativeHLS) >= nativeHLSMaxSessions {
		for key, old := range app.nativeHLS {
			old.cancel()
			_ = os.RemoveAll(old.dir)
			delete(app.nativeHLS, key)
			break
		}
	}
	app.nativeHLS[id] = session
	app.nativeHLSMu.Unlock()
	go runNativeHLSJob(jobCtx, session, ffmpeg)
	time.AfterFunc(nativeHLSIdleTimeout, func() { app.removeNativeHLS(id) })
	writeJSON(writer, http.StatusOK, map[string]any{
		"url":      "/api/ui/playback/native/hls/" + id + "/index.m3u8",
		"duration": media.Duration.Seconds(),
		"quality":  media.Quality,
		"title":    title,
	})
}

func runNativeHLSJob(ctx context.Context, session *nativeHLSSession, ffmpeg string) {
	defer close(session.done)
	command := ffmpegMediaCommand(ctx, ffmpeg, playbackNativeHLSArgs(session.media, session.dir)...)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil && ctx.Err() == nil {
		detail := output.String()
		if len(detail) > 2000 {
			detail = detail[len(detail)-2000:]
		}
		session.setErr(fmt.Errorf("媒体封装失败：%s", detail))
	}
}

func (app *UIApp) removeNativeHLS(id string) {
	app.nativeHLSMu.Lock()
	session := app.nativeHLS[id]
	delete(app.nativeHLS, id)
	app.nativeHLSMu.Unlock()
	if session != nil {
		session.cancel()
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
	timeout := 60 * time.Second
	if name == "index.m3u8" || name == "init.mp4" {
		timeout = 10 * time.Second
	}
	file, info, err := session.waitForFile(name, timeout)
	if err == os.ErrNotExist {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "资源不存在"})
		return
	}
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": app.redactError(err)})
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

func (session *nativeHLSSession) waitForFile(name string, timeout time.Duration) (*os.File, os.FileInfo, error) {
	path := filepath.Join(session.dir, name)
	deadline := time.Now().Add(timeout)
	for {
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			file, err := os.Open(path)
			if err == nil {
				return file, info, nil
			}
		}
		if session.ended() {
			if jobErr := session.getErr(); jobErr != nil {
				return nil, nil, jobErr
			}
			if _, err := os.Stat(path); os.IsNotExist(err) {
				return nil, nil, os.ErrNotExist
			}
			continue
		}
		if time.Now().After(deadline) {
			return nil, nil, os.ErrNotExist
		}
		time.Sleep(50 * time.Millisecond)
	}
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
		"-hls_playlist_type", "event", "-hls_segment_type", "fmp4",
		"-hls_fmp4_init_filename", "init.mp4",
		"-hls_flags", "independent_segments+temp_file",
		"-hls_segment_filename", filepath.ToSlash(filepath.Join(directory, "%06d.m4s")),
		filepath.ToSlash(filepath.Join(directory, "index.m3u8")),
	}
}
