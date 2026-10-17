package app

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func retireEmbyInactiveFiles(ctx context.Context, settings embySyncSettings, dramaID, folder string, key []byte) (int, bool, error) {
	if !validEmbyFolder(folder) || folder == "" {
		return 0, false, errors.New("Emby 剧集目录无效")
	}
	root, err := filepath.EvalSymlinks(settings.OutputDir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	target := filepath.Join(root, folder)
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return 0, false, errors.New("Emby 剧集目录不是普通文件夹")
	}
	marker := filepath.Join(target, ".duanju-emby.json")
	info, err = os.Lstat(marker)
	if err != nil || !info.Mode().IsRegular() {
		return 0, false, errors.New("Emby 目录缺少有效的自动同步清单，原内容已保留")
	}
	var manifest embyFolderManifest
	if err = readEmbyJSON(marker, &manifest, 8<<20); err != nil {
		return 0, false, err
	}
	if manifest.Version != 1 || manifest.DramaID != dramaID || !validEmbyManifestMode(manifest.Mode) || !validEmbyRetiredMode(manifest.RetiredMode) || !validEmbyLocalManifest(manifest) {
		return 0, false, errors.New("Emby 目录所属剧集不匹配，原内容已保留")
	}
	// long: 剧集不再属于同步范围时，撤下程序生成的入口与校验匹配的入库 MP4；原下载文件和用户修改内容始终保留。
	removed, err := retireEmbySTRMs(ctx, target, manifest, key)
	if err != nil {
		return removed, false, err
	}
	if manifest.Mode != "retired" {
		// long: 退休后保留原模式；下次重试即使 STRM 已被外部删掉，也能准确识别全集 NFO。
		manifest.RetiredMode = manifest.Mode
		if manifest.RetiredMode == "" {
			manifest.RetiredMode = "episodes"
		}
	}
	manifest.Mode = "retired"
	if err = writeEmbyJSON(marker, manifest, 0600); err != nil {
		return removed, false, err
	}
	// long: 只有目录中不再有用户文件时才移除剧集目录，避免把 Emby 自己缓存的海报或用户手工内容一起删掉。
	entries, err := os.ReadDir(target)
	if err != nil {
		return removed, false, err
	}
	if len(entries) == 1 && entries[0].Name() == filepath.Base(marker) {
		if err = os.Remove(marker); err != nil {
			return removed, false, err
		}
		if err = os.Remove(target); err != nil {
			// long: 目录可能被 Emby 并发写入；恢复清单让下一轮仍能识别并重试清理。
			_ = writeEmbyJSON(marker, manifest, 0600)
			return removed, false, err
		}
	}
	return removed, true, nil
}

func retireEmbySTRMs(ctx context.Context, folder string, manifest embyFolderManifest, key []byte) (int, error) {
	season := filepath.Join(folder, "Season 01")
	info, err := os.Lstat(season)
	seasonExists := true
	if errors.Is(err, os.ErrNotExist) {
		seasonExists = false
	} else if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return 0, errors.New("Emby 分集目录不是普通文件夹")
	}
	chapters := manifest.Chapters
	merged := manifest.Mode == "merged" || manifest.Mode == embyLocalMergedMode || manifest.Mode == "retired" && (manifest.RetiredMode == "merged" || manifest.RetiredMode == embyLocalMergedMode)
	if manifest.Mode == "retired" && manifest.RetiredMode == "" && seasonExists {
		// long: 旧版本把全集退休后只把清单模式写成 retired；读取剩余入口可恢复其单入口语义，避免按分集校验而误保留。
		path := filepath.Join(season, "S01E001.strm")
		if body, readErr := os.ReadFile(path); readErr == nil && isManagedEmbyLink(strings.TrimSpace(string(body)), manifest.DramaID, "merged", key, true) {
			merged = true
		}
		if !merged {
			// long: 兼容旧版退休清单；只接受与生成格式完全一致的全集 NFO，保留无法确认来源的文件。
			merged = legacyMergedEmbyEpisodeNFO(filepath.Join(season, "S01E001.nfo"))
		}
	}
	if merged {
		chapters = []embyChapter{{ID: "merged", Number: 1}}
	}
	var paths []string
	if manifest.Media != nil || manifest.PendingMedia != nil {
		actual, _, err := checkEmbyLocalMedia(ctx, folder, manifest)
		if err != nil {
			return 0, err
		}
		if actual != nil {
			paths = append(paths, filepath.Join(folder, filepath.FromSlash(embyLocalMergedName)))
		}
	}
	// long: Emby 或用户换过的海报属于用户内容，只有仍与发布清单一致的本地海报才随全集撤下。
	seenPosters := map[string]bool{}
	for _, poster := range []*embyLocalFile{manifest.Poster, manifest.PendingPoster} {
		if poster == nil || seenPosters[poster.Name] {
			continue
		}
		path := filepath.Join(folder, poster.Name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxCoverBytes {
			continue
		}
		actual, _, err := hashEmbyLocalFile(ctx, path, poster.Name)
		if err != nil {
			return 0, err
		}
		if sameEmbyLocalFile(actual, manifest.Poster) || sameEmbyLocalFile(actual, manifest.PendingPoster) {
			paths = append(paths, path)
			seenPosters[poster.Name] = true
		}
	}
	seen := make(map[int]bool, len(chapters))
	for _, chapter := range chapters {
		if !validEmbyIdentity(manifest.DramaID, chapter.ID) || chapter.Number < 1 || chapter.Number > 2000 || seen[chapter.Number] {
			return 0, errors.New("Emby 分集清单无效，原内容已保留")
		}
		seen[chapter.Number] = true
		if !seasonExists {
			continue
		}
		path := filepath.Join(season, fmt.Sprintf("S01E%03d.strm", chapter.Number))
		info, err := os.Lstat(path)
		if !errors.Is(err, os.ErrNotExist) {
			if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
				return 0, errors.New("Emby 播放入口已改变，原文件已保留")
			}
			body, err := os.ReadFile(path)
			if err != nil || !isManagedEmbyLink(strings.TrimSpace(string(body)), manifest.DramaID, chapter.ID, key, merged) {
				return 0, errors.New("Emby 播放入口不是本程序生成，原文件已保留")
			}
			paths = append(paths, path)
		}
		nfoPath := filepath.Join(season, fmt.Sprintf("S01E%03d.nfo", chapter.Number))
		managed, err := managedEmbyEpisodeNFO(nfoPath, chapter)
		if err != nil {
			return 0, err
		}
		if managed {
			paths = append(paths, nfoPath)
		}
	}
	showNFO := filepath.Join(folder, "tvshow.nfo")
	managed, err := managedEmbyShowNFO(showNFO, manifest.DramaID, key)
	if err != nil {
		return 0, err
	}
	if managed {
		paths = append(paths, showNFO)
	}
	removed := 0
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		if err := os.Remove(path); err != nil {
			return removed, err
		}
		removed++
	}
	if seasonExists {
		entries, err := os.ReadDir(season)
		if err != nil {
			return removed, err
		}
		if len(entries) == 0 {
			if err := os.Remove(season); err != nil {
				return removed, err
			}
		}
	} else if _, err := os.Stat(season); err == nil {
		return removed, errors.New("Emby 分集目录状态已改变，原文件已保留")
	} else if !errors.Is(err, os.ErrNotExist) {
		return removed, err
	}
	return removed, nil
}

func managedEmbyShowNFO(path, dramaID string, key []byte) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return false, errors.New("Emby 元数据文件已改变，原文件已保留")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var document struct {
		XMLName  xml.Name
		Identity struct {
			Value string `xml:",chardata"`
			Type  string `xml:"type,attr"`
		} `xml:"uniqueid"`
	}
	if err := xml.Unmarshal(body, &document); err != nil {
		// long: 无法确认来源的 NFO 可能是用户文件，宁可保留也不误删。
		return false, nil
	}
	return document.XMLName.Local == "tvshow" && document.Identity.Type == "duanju" && document.Identity.Value == embyMetadataID(key, dramaID), nil
}

func managedEmbyEpisodeNFO(path string, chapter embyChapter) (bool, error) {
	if chapter.ID == "merged" {
		// long: 全集标题可能被用户改成相似文案；仅回收与程序生成字节完全一致的 NFO。
		return legacyMergedEmbyEpisodeNFO(path), nil
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return false, errors.New("Emby 分集元数据文件已改变，原文件已保留")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var document struct {
		XMLName xml.Name
		Title   string `xml:"title"`
		Season  int    `xml:"season"`
		Episode int    `xml:"episode"`
	}
	if err := xml.Unmarshal(body, &document); err != nil {
		// long: 无法确认来源的 NFO 可能是用户文件，宁可保留也不误删。
		return false, nil
	}
	return document.XMLName.Local == "episodedetails" && document.Title == chapter.Title && document.Season == 1 && document.Episode == chapter.Number, nil
}

func legacyMergedEmbyEpisodeNFO(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return false
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var document struct {
		XMLName xml.Name `xml:"episodedetails"`
		Title   string   `xml:"title"`
		Show    string   `xml:"showtitle"`
		Season  int      `xml:"season"`
		Episode int      `xml:"episode"`
	}
	if xml.Unmarshal(body, &document) != nil || document.XMLName.Local != "episodedetails" || document.Show == "" || document.Season != 1 || document.Episode != 1 {
		return false
	}
	var start, end int
	if _, err := fmt.Sscanf(document.Title, "全集（第%d–%d集）", &start, &end); err != nil || start < 1 || end < start || end > 2000 || document.Title != fmt.Sprintf("全集（第%d–%d集）", start, end) {
		return false
	}
	var generated []byte
	if err := writeEmbyEpisodeFile(document.Show, document.Title, 1, "S01E001", func(_ string, content []byte) error { generated = content; return nil }); err != nil {
		return false
	}
	return string(body) == string(generated)
}

func isManagedEmbyLink(raw, dramaID, chapterID string, key []byte, merged bool) bool {
	link, err := url.Parse(raw)
	if err != nil || link.Host == "" || (link.Scheme != "http" && link.Scheme != "https") || link.User != nil || link.Fragment != "" {
		return false
	}
	endpoint := "/api/emby/stream.m3u8"
	if merged {
		endpoint = "/api/emby/merged.mp4"
	}
	query := link.Query()
	if !strings.HasSuffix(link.Path, endpoint) || query.Get("id") != dramaID || query.Get("chapter") != chapterID {
		return false
	}
	supplied, err := hex.DecodeString(query.Get("key"))
	if err != nil || len(supplied) != 32 {
		return false
	}
	expected, _ := hex.DecodeString(embyToken(key, dramaID, chapterID, query.Get("account")))
	return hmac.Equal(supplied, expected)
}
