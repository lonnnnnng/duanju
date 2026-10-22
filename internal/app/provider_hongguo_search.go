package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type hongguoSearchEntry struct {
	Dramas    []Drama
	Total     int
	ExpiresAt time.Time
}

type hongguoSearchCall struct {
	done  chan struct{}
	entry hongguoSearchEntry
	err   error
}

func hongguoSearchKeyword(keyword string) (string, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" || !utf8.ValidString(keyword) || utf8.RuneCountInString(keyword) > 30 || strings.IndexFunc(keyword, unicode.IsControl) >= 0 {
		return "", errors.New("请输入 1 至 30 个字符的搜索词")
	}
	return keyword, nil
}

func (downloader *Downloader) searchHongguoDramas(ctx context.Context, keyword string) (hongguoSearchEntry, error) {
	keyword, err := hongguoSearchKeyword(keyword)
	if err != nil {
		return hongguoSearchEntry{}, err
	}
	client := downloader.hongguoClient()
	client.mu.Lock()
	if cached, found := client.searches[keyword]; found && time.Now().Before(cached.ExpiresAt) {
		client.mu.Unlock()
		return cached, nil
	}
	if pending := client.searchPending[keyword]; pending != nil {
		client.mu.Unlock()
		select {
		case <-ctx.Done():
			return hongguoSearchEntry{}, ctx.Err()
		case <-pending.done:
			if (errors.Is(pending.err, context.Canceled) || errors.Is(pending.err, context.DeadlineExceeded)) && ctx.Err() == nil {
				return downloader.searchHongguoDramas(ctx, keyword)
			}
			return pending.entry, pending.err
		}
	}
	pending := &hongguoSearchCall{done: make(chan struct{})}
	client.searchPending[keyword] = pending
	client.mu.Unlock()
	entry, err := downloader.fetchHongguoSearch(ctx, keyword)
	client.mu.Lock()
	if err == nil {
		if len(client.searches) >= 64 {
			client.searches = map[string]hongguoSearchEntry{}
		}
		entry.ExpiresAt = time.Now().Add(5 * time.Minute)
		client.searches[keyword] = entry
	}
	pending.entry, pending.err = entry, err
	delete(client.searchPending, keyword)
	close(pending.done)
	client.mu.Unlock()
	return entry, err
}

func (downloader *Downloader) fetchHongguoSearch(ctx context.Context, keyword string) (hongguoSearchEntry, error) {
	// long: 官网搜索页已改为客户端渲染，直接请求其使用的搜索接口，避免解析不到 _ROUTER_DATA。
	searchURL := hongguoBaseURL + "/api/search/result?" + url.Values{"query": {keyword}}.Encode()
	pageURL := hongguoBaseURL + "/search/" + url.PathEscape(keyword)
	body, err := downloader.fetchProviderText(ctx, searchURL, pageURL)
	if err != nil {
		return hongguoSearchEntry{}, err
	}
	var result map[string]any
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil || result == nil {
		return hongguoSearchEntry{}, errors.New("搜索接口返回格式异常")
	}
	if code := mapString(result, "code"); code != "" && code != "0" {
		return hongguoSearchEntry{}, fmt.Errorf("搜索接口暂不可用（%s）", truncate(code, 20))
	}
	rows, valid := result["searchList"].([]any)
	if !valid {
		return hongguoSearchEntry{}, errors.New("搜索未返回有效结果")
	}
	entry := hongguoSearchEntry{Dramas: make([]Drama, 0, len(rows))}
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		if len(nestedMap(row, "video_data")) == 0 {
			continue
		}
		drama := hongguoDramaFromAny(row, "短剧")
		if drama.ID != "" && !seen[drama.ID] {
			seen[drama.ID] = true
			entry.Dramas = append(entry.Dramas, drama)
		}
	}
	if len(rows) > 0 && len(entry.Dramas) == 0 {
		return hongguoSearchEntry{}, errors.New("搜索结果中没有可识别的剧集")
	}
	entry.Total, _ = strconv.Atoi(mapString(result, "totalCount"))
	if entry.Total < len(entry.Dramas) {
		entry.Total = len(entry.Dramas)
	}
	return entry, nil
}
