package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (app *UIApp) embySigningKey(create bool) ([]byte, error) {
	app.embyMu.Lock()
	defer app.embyMu.Unlock()
	if len(app.embyKey) == 32 {
		return app.embyKey, nil
	}
	path := filepath.Join(app.cfg.dataDirectory(), "emby-key")
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && create {
		if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		body = make([]byte, 32)
		if _, err = rand.Read(body); err != nil {
			return nil, err
		}
		file, openErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(openErr, os.ErrExist) {
			body, err = os.ReadFile(path)
		} else if openErr != nil {
			return nil, openErr
		} else {
			_, err = file.Write(body)
			if syncErr := file.Sync(); err == nil {
				err = syncErr
			}
			if closeErr := file.Close(); err == nil {
				err = closeErr
			}
		}
	}
	if err != nil {
		return nil, err
	}
	if len(body) != 32 {
		return nil, errors.New("Emby 链接密钥无效")
	}
	app.embyKey = body
	return body, nil
}

func embyToken(key []byte, dramaID, chapterID string, accounts ...string) string {
	mac := hmac.New(sha256.New, key)
	payload := "emby-v1\x00" + dramaID + "\x00" + chapterID
	if len(accounts) > 0 && accounts[0] != "" {
		payload = "emby-v2\x00" + accounts[0] + "\x00" + dramaID + "\x00" + chapterID
	}
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func validEmbyIdentity(dramaID, chapterID string) bool {
	canonical, _, ok := playbackHistoryIdentity(dramaID)
	return ok && canonical == dramaID && len(dramaID) <= 256 && chapterID != "" && len(chapterID) <= 512 && !strings.ContainsAny(chapterID, "\x00\r\n")
}

func (app *UIApp) authorizeEmby(writer http.ResponseWriter, request *http.Request) (string, string, bool) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writer.Header().Set("Allow", "GET, HEAD")
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "请求方法不支持"})
		return "", "", false
	}
	query := request.URL.Query()
	id, chapter := query.Get("id"), query.Get("chapter")
	supplied, err := hex.DecodeString(query.Get("key"))
	key, keyErr := app.embySigningKey(false)
	if err != nil || len(supplied) != 32 || keyErr != nil || !validEmbyIdentity(id, chapter) {
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "Emby 播放链接无效，请重新导出"})
		return "", "", false
	}
	owner := query.Get("account")
	expected, _ := hex.DecodeString(embyToken(key, id, chapter, owner))
	if !hmac.Equal(expected, supplied) {
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "Emby 播放链接无效，请重新导出"})
		return "", "", false
	}
	if owner != "" && !app.embyAccountSourceAllowed(owner, id) {
		writeViewerError(writer, http.StatusForbidden, "export_forbidden", "当前账号无权使用此 Emby 链接，请联系管理员")
		return "", "", false
	}
	return id, chapter, true
}

func embyBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || len(raw) > 2048 || parsed == nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || strings.ContainsAny(raw, "\x00\r\n") {
		return "", errors.New("请输入 Emby 能访问的剧库 HTTP/HTTPS 地址")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func (app *UIApp) handleEmbyExport(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		DramaID string `json:"dramaId"`
		BaseURL string `json:"baseUrl"`
		Merged  bool   `json:"merged"`
	}
	if !readPlaybackRequest(writer, request, &input) {
		return
	}
	if !app.requireDramaSources(writer, request, []string{input.DramaID}) {
		return
	}
	base, err := embyBaseURL(input.BaseURL)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	app.mu.Lock()
	var drama Drama
	for _, item := range app.dramas {
		if item.ID == input.DramaID {
			drama = item
			break
		}
	}
	var merge UIMergeState
	if input.Merged {
		task, _, mergeErr := app.mergedPlaybackTaskLocked(mergedPlaybackPrefix + input.DramaID)
		if mergeErr != nil {
			app.mu.Unlock()
			writeJSON(writer, http.StatusConflict, map[string]string{"error": "本剧没有可导出的本地全集，请先完成合并"})
			return
		}
		merge = *app.merges[input.DramaID]
		if drama.ID == "" {
			// long: 源站下架或剧库刷新后，本地全集仍可播放；手动导出沿用合并记录中的剧名和站源。
			drama = Drama{ID: input.DramaID, Title: merge.DramaTitle, Source: task.Chapter.Source}
		}
	}
	app.mu.Unlock()
	if drama.ID == "" {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "请先在剧库中找到此剧"})
		return
	}
	var chapters []Chapter
	if !input.Merged {
		ctx, cancel := context.WithTimeout(request.Context(), 90*time.Second)
		defer cancel()
		title, resolved, resolveErr := app.downloader.GetDramaChapters(ctx, drama.ID)
		if resolveErr != nil {
			writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "读取分集失败：" + app.redactError(resolveErr)})
			return
		}
		chapters = resolved
		if len(chapters) == 0 || len(chapters) > 2000 {
			writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "可导出分集数无效"})
			return
		}
		for _, chapter := range chapters {
			if !validEmbyIdentity(drama.ID, chapter.ID) {
				writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "此剧未提供稳定分集 ID，暂不能导出"})
				return
			}
		}
		drama.Title = firstNonEmpty(title, drama.DisplayTitle())
	}
	key, err := app.embySigningKey(true)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "无法保存 Emby 链接密钥"})
		return
	}
	owner := ""
	if scope := sourceScope(request.Context()); scope != nil {
		owner = scope.AccountID
	}
	var body []byte
	if input.Merged {
		body, err = buildEmbyMergedArchive(drama, merge, base, key, owner)
	} else {
		body, err = buildEmbyArchive(drama, chapters, base, key, owner)
	}
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "生成 Emby 导出文件失败"})
		return
	}
	writer.Header().Set("Content-Type", "application/zip")
	filename := safeFilename(drama.DisplayTitle()) + "-Emby.zip"
	if input.Merged {
		filename = safeFilename(drama.DisplayTitle()) + "-Emby-全集.zip"
	}
	writer.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
	writer.Write(body)
}

func buildEmbyArchive(drama Drama, chapters []Chapter, base string, key []byte, accounts ...string) ([]byte, error) {
	owner := ""
	if len(accounts) > 0 {
		owner = accounts[0]
	}
	records, err := embyChapterRecords(drama.ID, chapters, nil)
	if err != nil {
		return nil, err
	}
	return buildEmbyZip(drama, func(write func(string, []byte) error) error {
		return writeEmbyFiles(drama, records, base, key, owner, write)
	})
}

func buildEmbyMergedArchive(drama Drama, merge UIMergeState, base string, key []byte, owner string) ([]byte, error) {
	return buildEmbyZip(drama, func(write func(string, []byte) error) error {
		return writeEmbyMergedFiles(drama, merge, base, key, owner, write)
	})
}

func buildEmbyZip(drama Drama, writeFiles func(func(string, []byte) error) error) ([]byte, error) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	prefix := embyFolderName(drama) + "/"
	// long: 手动导出的全集与自动同步共用文件写入器，确保 STRM 签名和 NFO 内容一致。
	err := writeFiles(func(name string, body []byte) error {
		file, err := archive.Create(prefix + name)
		if err != nil {
			return err
		}
		_, err = file.Write(body)
		return err
	})
	if err != nil {
		archive.Close()
		return nil, err
	}
	if err = archive.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func (app *UIApp) embyTask(ctx context.Context, id, chapterID string) (Task, string, error) {
	app.mu.Lock()
	for _, candidate := range app.tasks {
		if candidate.DramaID == id && candidate.Source.Chapter.ID == chapterID && isPlayableDownloadTask(candidate) {
			task, downloadID := candidate.Source, candidate.ID
			app.mu.Unlock()
			return task, downloadID, nil
		}
	}
	app.mu.Unlock()
	title, chapters, err := app.downloader.GetDramaChapters(ctx, id)
	if err != nil {
		return Task{}, "", err
	}
	for index, chapter := range chapters {
		if chapter.ID == chapterID {
			return Task{DramaID: id, DramaTitle: title, Chapter: chapter, Index: index + 1, Total: len(chapters)}, "", nil
		}
	}
	return Task{}, "", errors.New("此分集已不可用，请更新剧库并重新导出")
}

func (app *UIApp) handleEmbyMerged(writer http.ResponseWriter, request *http.Request) {
	id, chapter, ok := app.authorizeEmby(writer, request)
	if !ok {
		return
	}
	if chapter != "merged" {
		http.NotFound(writer, request)
		return
	}
	app.mu.Lock()
	_, path, err := app.mergedPlaybackTaskLocked(mergedPlaybackPrefix + id)
	app.mu.Unlock()
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	// long: 合并全集直接按 HTTP Range 读取本地 MP4，避免再把完整文件转成两秒一段的 HLS。
	file, err := os.Open(path)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		http.NotFound(writer, request)
		return
	}
	writer.Header().Set("Content-Type", "video/mp4")
	http.ServeContent(writer, request, "merged.mp4", info.ModTime(), file)
}

func (app *UIApp) handleEmbyLegacyStream(writer http.ResponseWriter, request *http.Request) {
	id, chapter, ok := app.authorizeEmby(writer, request)
	if !ok {
		return
	}
	if request.Method == http.MethodHead {
		writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		return
	}
	session := app.acquireEmbyPlayback(writer, request, id, chapter, "", true)
	if session == nil {
		return
	}
	defer app.releaseEmbyPlayback(session)
	app.serveEmbyPlaylist(writer, request, session)
}

func (app *UIApp) serveEmbyPlaylist(writer http.ResponseWriter, request *http.Request, session *playbackSession) {
	id, chapter, sessionID, duration := session.dramaID, session.tasks[0].Chapter.ID, session.id, session.duration
	var playlist strings.Builder
	fmt.Fprintf(&playlist, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-INDEPENDENT-SEGMENTS\n", playbackNativeSegmentSeconds)
	for part, count := 0, int(math.Ceil(duration/playbackNativeSegmentSeconds)); part < count; part++ {
		query := url.Values{"id": {id}, "chapter": {chapter}, "key": {request.URL.Query().Get("key")}, "session": {sessionID}, "segment": {strconv.Itoa(part)}}
		if owner := request.URL.Query().Get("account"); owner != "" {
			query.Set("account", owner)
		}
		fmt.Fprintf(&playlist, "#EXTINF:%.6f,\nsegment.ts?%s\n", math.Min(playbackNativeSegmentSeconds, duration-float64(part*playbackNativeSegmentSeconds)), query.Encode())
	}
	playlist.WriteString("#EXT-X-ENDLIST\n")
	writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	http.ServeContent(writer, request, "index.m3u8", time.Time{}, strings.NewReader(playlist.String()))
}

func (app *UIApp) handleEmbySegment(writer http.ResponseWriter, request *http.Request) {
	id, chapter, ok := app.authorizeEmby(writer, request)
	if !ok {
		return
	}
	query := request.URL.Query()
	part, err := strconv.Atoi(query.Get("segment"))
	if err != nil || part < 0 {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "分片编号无效"})
		return
	}
	if query.Get("session") == "" {
		app.writeEmbyPlaybackError(writer, request, http.StatusGone, "播放已过期，请重新播放")
		return
	}
	session := app.acquireEmbyPlayback(writer, request, id, chapter, query.Get("session"), true)
	if session == nil {
		return
	}
	defer app.releaseEmbyPlayback(session)
	cache := session.native
	if cache == nil {
		app.writeEmbyPlaybackError(writer, request, http.StatusGone, "播放格式已变化，请重新播放")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 90*time.Second)
	defer cancel()
	body, err := cache.segment(ctx, part)
	if err != nil {
		app.writeEmbyPlaybackError(writer, request, http.StatusBadGateway, "读取 Emby 分片失败："+app.redactError(err))
		return
	}
	active := &playbackRunContext{id: session.id, run: session.run, session: session, ctx: cache.ctx}
	writer = &playbackActivityWriter{ResponseWriter: writer, app: app, run: active}
	writer.Header().Set("Content-Type", "video/mp2t")
	http.ServeContent(writer, request, "segment.ts", time.Time{}, bytes.NewReader(body))
}
