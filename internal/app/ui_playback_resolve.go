package app

import (
	"context"
	"crypto/aes"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
)

// playbackNativeVariant 是原生 App 直连播放所需的单个清晰度直链及其 CENC 内容密钥。
type playbackNativeVariant struct {
	Quality int    `json:"quality"`
	URL     string `json:"url"`
	Key     string `json:"key"`
}

// playbackNativeMedia 是原生 App 直连播放的解析结果。服务器只做「解析地址 + 派生密钥」，
// 不做任何 ffmpeg 转码；解密与 HEVC 硬件解码由 App 在本地完成。
type playbackNativeMedia struct {
	SeriesID string                  `json:"seriesId"`
	Episode  int                     `json:"episode"`
	Title    string                  `json:"title,omitempty"`
	Referer  string                  `json:"referer,omitempty"`
	Duration float64                 `json:"duration"`
	Quality  int                     `json:"quality"`
	URL      string                  `json:"url"`
	Key      string                  `json:"key"`
	Variants []playbackNativeVariant `json:"variants"`
}

func (app *UIApp) handlePlaybackResolve(writer http.ResponseWriter, request *http.Request) {
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
	ctx, cancel := context.WithTimeout(request.Context(), 60*time.Second)
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
	result, err := buildPlaybackNativeMedia(media, seriesID, input.Episode, title)
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func buildPlaybackNativeMedia(media providerMedia, seriesID string, episode int, title string) (playbackNativeMedia, error) {
	key := ""
	if len(media.CENCKey) == aes.BlockSize {
		key = hex.EncodeToString(media.CENCKey)
	}
	if !isProviderHTTPMediaURL(media.URL) || key == "" {
		return playbackNativeMedia{}, errors.New("未返回该集可用的媒体和密钥")
	}
	result := playbackNativeMedia{
		SeriesID: seriesID,
		Episode:  episode,
		Title:    title,
		Referer:  media.Referer,
		Duration: media.Duration.Seconds(),
		Quality:  media.Quality,
		URL:      media.URL,
		Key:      key,
	}
	seen := map[string]bool{}
	addVariant := func(candidate providerMedia) {
		if !isProviderHTTPMediaURL(candidate.URL) || len(candidate.CENCKey) != aes.BlockSize || seen[candidate.URL] {
			return
		}
		seen[candidate.URL] = true
		result.Variants = append(result.Variants, playbackNativeVariant{
			Quality: candidate.Quality, URL: candidate.URL, Key: hex.EncodeToString(candidate.CENCKey),
		})
	}
	addVariant(media)
	for _, candidate := range media.Variants {
		addVariant(candidate)
	}
	return result, nil
}
