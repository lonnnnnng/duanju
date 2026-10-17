package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEmbyExportPersistentSignedLinksAndSafeArchive(t *testing.T) {
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		t.Fatal("Emby export must use the cached text fixture")
		return nil, nil
	})
	id := "hongguo:7000000000000000001"
	chapters := []Chapter{{ID: id + ":1", Source: sourceHongguo, Title: "第一集"}, {ID: id + ":2", Source: sourceHongguo, Title: "第二集"}}
	d.hongguoClient().details["7000000000000000001"] = hongguoDetailEntry{Drama: Drama{ID: id, Title: "../测试剧 <&>"}, Chapters: chapters, ExpiresAt: time.Now().Add(time.Minute)}
	app := &UIApp{downloader: d, cfg: d.cfg, dramas: []Drama{{ID: id, Source: sourceHongguo, Title: "测试", Desc: "纯文字 <简介>"}}}
	body, _ := json.Marshal(map[string]string{"dramaId": id, "baseUrl": "http://library.test:8998"})
	req := httptest.NewRequest(http.MethodPost, "http://localhost/api/emby/export", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	result := httptest.NewRecorder()
	app.routes().ServeHTTP(result, req)
	if result.Code != 200 || result.Header().Get("Content-Type") != "application/zip" {
		t.Fatal(result.Code, result.Body.String())
	}
	archive, err := zip.NewReader(bytes.NewReader(result.Body.Bytes()), int64(result.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.File) != 5 {
		t.Fatal("missing STRM or NFO files")
	}
	links := []string{}
	for _, file := range archive.File {
		if strings.HasPrefix(file.Name, "/") || strings.Contains(file.Name, "..") || strings.Contains(file.Name, "\\") {
			t.Fatal("unsafe archive path", file.Name)
		}
		reader, _ := file.Open()
		content, _ := io.ReadAll(reader)
		reader.Close()
		if strings.HasSuffix(file.Name, ".strm") {
			links = append(links, strings.TrimSpace(string(content)))
		}
		if strings.HasSuffix(file.Name, "tvshow.nfo") && !bytes.Contains(content, []byte("&lt;&amp;&gt;")) {
			t.Fatal("NFO XML not escaped")
		}
	}
	if len(links) != 2 || links[0] == links[1] {
		t.Fatal("wrong per-episode links")
	}
	info, err := os.Stat(filepath.Join(d.cfg.dataDirectory(), "emby-key"))
	if err != nil || info.Size() != 32 || info.Mode().Perm() != 0o600 {
		t.Fatal("persistent signing key not saved privately")
	}
	restarted := &UIApp{cfg: d.cfg}
	for _, link := range links {
		request := httptest.NewRequest(http.MethodHead, link, nil)
		writer := httptest.NewRecorder()
		restarted.handleEmbyStream(writer, request)
		if writer.Code != 200 || writer.Body.Len() != 0 {
			t.Fatal("exported URL did not survive restart")
		}
		parsed, _ := url.Parse(link)
		query := parsed.Query()
		query.Set("chapter", id+":3")
		parsed.RawQuery = query.Encode()
		writer = httptest.NewRecorder()
		restarted.handleEmbyStream(writer, httptest.NewRequest(http.MethodGet, parsed.String(), nil))
		if writer.Code != 403 {
			t.Fatal("signed link accepted a different episode")
		}
	}
}

func TestEmbyMergedManualExportWorksWithoutBrowserCookie(t *testing.T) {
	app, admin, _, _, merged := deleteFixture(t)
	app.merges[historyFixtureDramaID].StartEpisode = 1
	app.merges[historyFixtureDramaID].EndEpisode = 2
	app.merges[historyFixtureDramaID].OutputPath = merged + ".missing"
	input := map[string]any{"dramaId": historyFixtureDramaID, "baseUrl": "http://library.test", "merged": true}
	if result := admin.request(t, http.MethodPost, "/api/emby/export", input); result.Code != http.StatusConflict {
		t.Fatal("missing merged file was exported", result.Code, result.Body.String())
	}
	if _, err := os.Stat(filepath.Join(app.cfg.dataDirectory(), "emby-key")); !os.IsNotExist(err) {
		t.Fatal("invalid merged export created a signing key")
	}
	app.merges[historyFixtureDramaID].OutputPath = merged
	app.merges[historyFixtureDramaID].Status = "failed"
	if result := admin.request(t, http.MethodPost, "/api/emby/export", input); result.Code != http.StatusConflict {
		t.Fatal("failed merge was exported", result.Code, result.Body.String())
	}
	app.merges[historyFixtureDramaID].Status = "success"
	result := admin.request(t, http.MethodPost, "/api/emby/export", input)
	if result.Code != http.StatusOK {
		t.Fatal(result.Code, result.Body.String())
	}
	archive, err := zip.NewReader(bytes.NewReader(result.Body.Bytes()), int64(result.Body.Len()))
	if err != nil || len(archive.File) != 3 {
		t.Fatal("merged archive does not contain show, episode and STRM", err)
	}
	var link string
	for _, file := range archive.File {
		reader, openErr := file.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		content, readErr := io.ReadAll(reader)
		reader.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.HasSuffix(file.Name, ".strm") {
			link = strings.TrimSpace(string(content))
		}
	}
	if !strings.Contains(link, "/api/emby/merged.mp4?") || !strings.Contains(link, "account=") {
		t.Fatal("merged STRM is missing account-bound local playback", link)
	}
	store := app.browserViewers().accountStore()
	store.mu.Lock()
	store.state.RequireLogin = true
	store.mu.Unlock()
	// long: Emby 请求不会携带网页 Cookie；签名链接必须走真实路由并在强制登录模式下仍可读取。
	request := httptest.NewRequest(http.MethodHead, link, nil)
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.Len() != 0 || response.Header().Get("Content-Type") != "video/mp4" {
		t.Fatal("signed merged playback was blocked by browser login policy", response.Code, response.Body.String())
	}
	// long: 手动 ZIP 仍提供 HTTP 全集入口，自动同步改为文件后仍要保留 Emby 拖动播放的 Range 能力和签名校验。
	request = httptest.NewRequest(http.MethodGet, link, nil)
	request.Header.Set("Range", "bytes=1-3")
	response = httptest.NewRecorder()
	app.routes().ServeHTTP(response, request)
	if response.Code != http.StatusPartialContent || response.Body.String() != "ide" {
		t.Fatal("手动导出的全集 Range 播放失效", response.Code, response.Body.String())
	}
	invalid, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	query := invalid.Query()
	query.Set("key", strings.Repeat("0", 64))
	invalid.RawQuery = query.Encode()
	response = httptest.NewRecorder()
	app.routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, invalid.String(), nil))
	if response.Code != http.StatusForbidden {
		t.Fatal("手动导出的全集入口接受了无效签名", response.Code)
	}
}

func TestEmbyMergedExportSurvivesDramaLeavingLibrary(t *testing.T) {
	app, admin, _, _, merged := deleteFixture(t)
	state := app.merges[historyFixtureDramaID]
	state.StartEpisode, state.EndEpisode, state.OutputPath = 1, 2, merged
	app.dramas = nil
	app.tasks = map[string]*UITask{}
	app.taskOrder = nil
	input := map[string]any{"dramaId": historyFixtureDramaID, "baseUrl": "http://library.test", "merged": true}
	result := admin.request(t, http.MethodPost, "/api/emby/export", input)
	if result.Code != http.StatusOK {
		t.Fatal("local merged video was not exportable after drama left library", result.Code, result.Body.String())
	}
	archive, err := zip.NewReader(bytes.NewReader(result.Body.Bytes()), int64(result.Body.Len()))
	if err != nil || len(archive.File) != 3 {
		t.Fatal("merged archive is incomplete", err)
	}
	var found bool
	for _, file := range archive.File {
		if !strings.HasSuffix(file.Name, ".strm") {
			continue
		}
		reader, openErr := file.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		body, readErr := io.ReadAll(reader)
		reader.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		found = strings.Contains(string(body), "/api/emby/merged.mp4?")
	}
	if !found {
		t.Fatal("archive lost the local merged playback link")
	}
	delete(input, "merged")
	if result := admin.request(t, http.MethodPost, "/api/emby/export", input); result.Code != http.StatusNotFound {
		t.Fatal("episode export unexpectedly succeeded without a library entry", result.Code)
	}
}

func TestEmbyRejectsInvalidURLAndSignature(t *testing.T) {
	for _, raw := range []string{"", "file:///tmp/local", "http://user:pass@example.test", "http://example.test/?key=secret", "http://example.test/#fragment"} {
		if _, err := embyBaseURL(raw); err == nil {
			t.Fatal("invalid public base URL accepted", raw)
		}
	}
	app := &UIApp{cfg: Config{dataDir: t.TempDir()}}
	writer := httptest.NewRecorder()
	app.handleEmbyStream(writer, httptest.NewRequest(http.MethodGet, "http://localhost/api/emby/stream.m3u8?id=hongguo:1&chapter=1&key=invalid", nil))
	if writer.Code != 403 {
		t.Fatal("unsigned stream allowed")
	}
	if _, err := os.Stat(filepath.Join(app.cfg.dataDirectory(), "emby-key")); !os.IsNotExist(err) {
		t.Fatal("unsigned request created a key")
	}
}

func TestEmbyHLSRangePlaybackAndExpiry(t *testing.T) {
	app, fixture := nativePlaybackFixture(t, "4", "320x180")
	app.mediaResources().settings.Enabled = false
	key, err := app.embySigningKey(true)
	if err != nil {
		t.Fatal(err)
	}
	task := fixture.tasks[0]
	query := url.Values{"id": {task.DramaID}, "chapter": {task.Chapter.ID}, "key": {embyToken(key, task.DramaID, task.Chapter.ID)}}
	writer := httptest.NewRecorder()
	app.handleEmbyStream(writer, httptest.NewRequest(http.MethodGet, "http://localhost/api/emby/stream.m3u8?"+query.Encode(), nil))
	if writer.Code != 200 || strings.Count(writer.Body.String(), "#EXTINF:") != 2 || !strings.Contains(writer.Body.String(), "#EXT-X-ENDLIST") {
		t.Fatal("invalid complete Emby HLS timeline", writer.Code, writer.Body.String())
	}
	var segment string
	for _, line := range strings.Split(writer.Body.String(), "\n") {
		if strings.HasPrefix(line, "segment.ts?") {
			segment = line
			break
		}
	}
	partURL := "http://localhost/api/emby/" + segment
	request := httptest.NewRequest(http.MethodGet, partURL, nil)
	request.Header.Set("Range", "bytes=0-187")
	part := httptest.NewRecorder()
	app.handleEmbySegment(part, request)
	if part.Code != 206 || part.Body.Len() != 188 || part.Body.Bytes()[0] != 0x47 {
		t.Fatal("Emby byte range did not return TS media")
	}
	part = httptest.NewRecorder()
	app.handleEmbySegment(part, httptest.NewRequest(http.MethodHead, partURL, nil))
	if part.Code != 200 || part.Body.Len() != 0 {
		t.Fatal("Emby segment HEAD failed")
	}
	parsed, _ := url.Parse(partURL)
	sessionID := parsed.Query().Get("session")
	app.playbackMu.Lock()
	active := app.playbacks[sessionID]
	app.playbackMu.Unlock()
	if active == nil || active.native == nil {
		t.Fatal("missing Emby playback cache")
	}
	app.closePlayback(sessionID)
	part = httptest.NewRecorder()
	app.handleEmbySegment(part, httptest.NewRequest(http.MethodGet, partURL, nil))
	if part.Code != 410 {
		t.Fatal("expired HLS session stayed usable")
	}
	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe unavailable")
	}
	server := httptest.NewServer(app.routes())
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, probe, "-v", "error", "-show_entries", "format=duration", "-of", "default=nw=1:nk=1", server.URL+"/api/emby/stream.m3u8?"+query.Encode()).CombinedOutput()
	if err != nil || !strings.Contains(string(output), "4.000000") {
		t.Fatalf("standard HLS client could not probe stream: %v %s", err, output)
	}
}
