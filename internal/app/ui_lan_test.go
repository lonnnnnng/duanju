package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLANStartupPreservesAndIgnoresLegacyAccounts(t *testing.T) {
	for _, data := range []string{`{"version":1,"requireLogin":true,"allowRegistration":false}`, `{broken legacy accounts`} {
		t.Run(data[:8], func(t *testing.T) {
			app := viewerTestApp(t)
			path := filepath.Join(app.cfg.dataDirectory(), "accounts.json")
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if err := app.prepareBrowserViewers(); err != nil {
				t.Fatal("legacy account data blocked startup", err)
			}
			browser := newLANTestBrowser(t, app)
			state := browser.refresh(t)
			for _, key := range []string{"account", "requireLogin", "allowRegistration", "onlineOnly", "sources"} {
				if _, exists := state[key]; exists {
					t.Fatal("account policy remains in viewer response", key)
				}
			}
			for _, path := range []string{"/", "/api/ui/config", "/api/ui/settings/playback", "/api/ui/settings/emby", "/api/ui/tasks"} {
				viewerResultOK(t, browser.request(t, http.MethodGet, path, nil))
			}
			t.Cleanup(app.embySyncer().stop)
			for _, path := range []string{"/login", "/api/ui/account/login", "/api/ui/account/register", "/api/ui/admin/accounts", "/api/ui/admin/settings", "/assets/account.js", "/assets/users.js"} {
				if response := browser.request(t, http.MethodGet, path, nil); response.Code != http.StatusNotFound {
					t.Fatal("retired account entry still exists", path, response.Code)
				}
			}
			page := browser.request(t, http.MethodGet, "/", nil).Body.String()
			for _, entry := range []string{"openAccountBtn", "accountPanel", "usersPage", "data-admin-only"} {
				if strings.Contains(page, entry) {
					t.Fatal("retired account UI remains", entry)
				}
			}
			if actual, err := os.ReadFile(path); err != nil || string(actual) != data {
				t.Fatal("legacy data was changed", err)
			}
		})
	}
}

func TestLANSettingsKeepOriginAndViewerChecks(t *testing.T) {
	app, browser := lanFixture(t)
	settings := defaultPlaybackSettings()
	settings.MaxSessions = 7
	viewerResultOK(t, browser.request(t, http.MethodPost, "/api/ui/settings/playback", settings))
	for _, scenario := range []struct {
		name, origin, site, viewer string
		status                     int
	}{
		{"cross-site", "http://other.example.test", "cross-site", browser.id, http.StatusForbidden},
		{"wrong-origin", "http://other.example.test", "", browser.id, http.StatusForbidden},
		{"missing-viewer", "", "same-origin", "", http.StatusForbidden},
		{"stale-viewer", "", "same-origin", strings.Repeat("0", 64), http.StatusConflict},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			body, _ := json.Marshal(defaultPlaybackSettings())
			request := httptest.NewRequest(http.MethodPost, "http://shared.example.test/api/ui/settings/playback", bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", scenario.origin)
			request.Header.Set("Sec-Fetch-Site", scenario.site)
			request.Header.Set("X-Duanju-Viewer", scenario.viewer)
			for _, cookie := range browser.cookies {
				request.AddCookie(cookie)
			}
			response := httptest.NewRecorder()
			browser.handler.ServeHTTP(response, request)
			if response.Code != scenario.status || app.mediaResources().snapshot().MaxSessions != 7 {
				t.Fatal("rejected request changed settings", response.Code, response.Body.String())
			}
		})
	}
}

func TestLANGlobalWritesKeepOriginChecks(t *testing.T) {
	for _, endpoint := range []struct {
		path, body string
	}{
		{"/api/ui/tasks/delete", `{"ids":["episode-1"]}`},
		{"/api/ui/merge/auto", `{"dramaIds":["` + historyFixtureDramaID + `"],"enabled":false}`},
	} {
		app, browser, first, _, _ := deleteFixture(t)
		for _, scenario := range []struct {
			name, origin, site, contentType string
			status                          int
		}{
			{"cross-site", "http://other.example.test", "cross-site", "application/json", http.StatusForbidden},
			{"wrong-origin", "http://other.example.test", "", "application/json", http.StatusForbidden},
			{"simple-text", "", "", "text/plain", http.StatusUnsupportedMediaType},
			{"form", "", "same-origin", "application/x-www-form-urlencoded", http.StatusUnsupportedMediaType},
			{"missing-content-type", "", "same-origin", "", http.StatusUnsupportedMediaType},
		} {
			t.Run(endpoint.path+"/"+scenario.name, func(t *testing.T) {
				request := httptest.NewRequest(http.MethodPost, "http://shared.example.test"+endpoint.path, strings.NewReader(endpoint.body))
				request.Header.Set("Origin", scenario.origin)
				request.Header.Set("Sec-Fetch-Site", scenario.site)
				request.Header.Set("Content-Type", scenario.contentType)
				response := httptest.NewRecorder()
				browser.handler.ServeHTTP(response, request)
				if response.Code != scenario.status {
					t.Fatal("unexpected rejection", response.Code, response.Body.String())
				}
				if data, err := os.ReadFile(first); err != nil || string(data) != "video" || app.tasks["episode-1"] == nil || app.autoMerges[historyFixtureDramaID] != nil {
					t.Fatal("rejected request changed shared downloads", err)
				}
			})
		}
		// long: 来源校验只拒绝跨站写入，同源 JSON 调用仍可直接管理任务，无需账号或登录 Cookie。
		request := httptest.NewRequest(http.MethodPost, "http://shared.example.test"+endpoint.path, strings.NewReader(endpoint.body))
		request.Header.Set("Origin", "http://shared.example.test")
		request.Header.Set("Content-Type", "application/json; charset=utf-8")
		response := httptest.NewRecorder()
		browser.handler.ServeHTTP(response, request)
		viewerResultOK(t, response)
		if endpoint.path == "/api/ui/tasks/delete" {
			if _, err := os.Stat(first); !os.IsNotExist(err) || app.tasks["episode-1"] != nil {
				t.Fatal("anonymous deletion did not run", err)
			}
		} else if state := app.autoMerges[historyFixtureDramaID]; state == nil || state.Enabled {
			t.Fatal("anonymous auto-merge update did not run")
		}
	}
}

func lanFixture(t *testing.T) (*UIApp, *lanTestBrowser) {
	t.Helper()
	app := viewerTestApp(t)
	return app, newLANTestBrowser(t, app)
}

type lanTestBrowser struct {
	handler http.Handler
	cookies map[string]*http.Cookie
	id      string
	secure  bool
}

func (browser *lanTestBrowser) request(t *testing.T, method, path string, input any) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	if input != nil {
		if err := json.NewEncoder(&body).Encode(input); err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, "http://shared.example.test"+path, &body)
	request.RemoteAddr = "192.168.1.10:1234"
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set("Content-Type", "application/json")
	if browser.id != "" {
		request.Header.Set("X-Duanju-Viewer", browser.id)
	}
	if browser.secure {
		request.Header.Set("X-Forwarded-Proto", "https")
	}
	for _, cookie := range browser.cookies {
		request.AddCookie(cookie)
	}
	writer := httptest.NewRecorder()
	browser.handler.ServeHTTP(writer, request)
	for _, cookie := range writer.Result().Cookies() {
		if cookie.MaxAge < 0 {
			delete(browser.cookies, cookie.Name)
		} else {
			browser.cookies[cookie.Name] = cookie
		}
	}
	return writer
}

func (browser *lanTestBrowser) refresh(t *testing.T) map[string]any {
	t.Helper()
	result := browser.request(t, http.MethodGet, "/api/ui/viewer", nil)
	viewerResultOK(t, result)
	var state map[string]any
	if err := json.Unmarshal(result.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state["ready"] != true {
		result = browser.request(t, http.MethodGet, "/api/ui/viewer?confirm=1", nil)
		viewerResultOK(t, result)
		if err := json.Unmarshal(result.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
	}
	browser.id, _ = state["id"].(string)
	if len(browser.id) != 64 {
		t.Fatal("missing viewer identity", state)
	}
	return state
}

func newLANTestBrowser(t *testing.T, app *UIApp) *lanTestBrowser {
	t.Helper()
	browser := &lanTestBrowser{handler: app.routes(), cookies: map[string]*http.Cookie{}}
	browser.refresh(t)
	return browser
}

func (browser *lanTestBrowser) following(t *testing.T) []followingView {
	t.Helper()
	result := browser.request(t, http.MethodGet, "/api/ui/following", nil)
	viewerResultOK(t, result)
	var response struct {
		Data []followingView `json:"data"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response.Data
}
