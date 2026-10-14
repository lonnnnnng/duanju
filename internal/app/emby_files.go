package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"sort"
)

type embyChapter struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Number int    `json:"number"`
}

func embyFolderName(drama Drama) string {
	folder := []rune(safeFilename(drama.DisplayTitle()))
	for len(string(folder)) > 180 {
		folder = folder[:len(folder)-1]
	}
	identity := sha256.Sum256([]byte(drama.ID))
	return string(folder) + " [" + hex.EncodeToString(identity[:8]) + "]"
}

func embyChapterRecords(id string, chapters []Chapter, previous []embyChapter) ([]embyChapter, error) {
	if len(chapters) == 0 || len(chapters) > 2000 || len(previous) > 2000 {
		return nil, errors.New("可导出分集数无效")
	}
	byID := make(map[string]embyChapter, len(previous)+len(chapters))
	used := make(map[int]bool, len(previous)+len(chapters))
	for _, chapter := range previous {
		if !validEmbyIdentity(id, chapter.ID) || chapter.Number < 1 || chapter.Number > 2000 || used[chapter.Number] || byID[chapter.ID].ID != "" {
			return nil, errors.New("已有 Emby 分集清单无效，原文件已保留")
		}
		byID[chapter.ID], used[chapter.Number] = chapter, true
	}
	seen := make(map[string]bool, len(chapters))
	for index, chapter := range chapters {
		if !validEmbyIdentity(id, chapter.ID) || seen[chapter.ID] {
			return nil, errors.New("站源未提供唯一、稳定的分集 ID，暂不能导出")
		}
		seen[chapter.ID] = true
		record := byID[chapter.ID]
		if record.ID == "" {
			number := index + 1
			for used[number] && number <= 2000 {
				number++
			}
			if number > 2000 {
				return nil, errors.New("Emby 分集数超过 2000 集")
			}
			record = embyChapter{ID: chapter.ID, Number: number}
			used[number] = true
		}
		record.Title = firstNonEmpty(chapter.Title, "第 "+chapter.EpisodeString(index+1)+" 集")
		byID[chapter.ID] = record
	}
	result := make([]embyChapter, 0, len(byID))
	for _, chapter := range byID {
		result = append(result, chapter)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Number < result[j].Number })
	return result, nil
}

func writeEmbyFiles(drama Drama, chapters []embyChapter, base string, key []byte, owner string, write func(string, []byte) error) error {
	if err := writeEmbyShowFile(drama, base, key, owner, write); err != nil {
		return err
	}
	for _, chapter := range chapters {
		query := url.Values{"id": {drama.ID}, "chapter": {chapter.ID}, "key": {embyToken(key, drama.ID, chapter.ID, owner)}}
		if owner != "" {
			query.Set("account", owner)
		}
		path := fmt.Sprintf("Season 01/S01E%03d", chapter.Number)
		if err := writeEmbyEpisodeFile(drama.DisplayTitle(), chapter.Title, chapter.Number, path, write); err != nil {
			return err
		}
		if err := write(path+".strm", []byte(base+"/api/emby/stream.m3u8?"+query.Encode()+"\n")); err != nil {
			return err
		}
	}
	return nil
}

func writeEmbyMergedFiles(drama Drama, merge UIMergeState, base string, key []byte, owner string, write func(string, []byte) error) error {
	if err := writeEmbyShowFile(drama, base, key, owner, write); err != nil {
		return err
	}
	// long: 全集作为电视剧中的单一条目，保留现有海报和电视剧媒体库的识别方式。
	path := "Season 01/S01E001"
	title := fmt.Sprintf("全集（第%d–%d集）", merge.StartEpisode, merge.EndEpisode)
	if err := writeEmbyEpisodeFile(drama.DisplayTitle(), title, 1, path, write); err != nil {
		return err
	}
	query := url.Values{"id": {drama.ID}, "chapter": {"merged"}, "key": {embyToken(key, drama.ID, "merged", owner)}}
	if owner != "" {
		query.Set("account", owner)
	}
	return write(path+".strm", []byte(base+"/api/emby/merged.mp4?"+query.Encode()+"\n"))
}

func writeEmbyShowFile(drama Drama, base string, key []byte, owner string, write func(string, []byte) error) error {
	title := drama.DisplayTitle()
	type nfoValue struct {
		Value  string `xml:",chardata"`
		Type   string `xml:"type,attr,omitempty"`
		Aspect string `xml:"aspect,attr,omitempty"`
	}
	var thumb *nfoValue
	if cover := embyCoverURL(drama, base, key, owner); cover != "" {
		thumb = &nfoValue{Value: cover, Aspect: "poster"}
	}
	show := struct {
		XMLName  xml.Name  `xml:"tvshow"`
		Title    string    `xml:"title"`
		Plot     string    `xml:"plot,omitempty"`
		Identity nfoValue  `xml:"uniqueid"`
		Thumb    *nfoValue `xml:"thumb,omitempty"`
	}{Title: title, Plot: firstNonEmpty(drama.Desc, drama.Intro), Identity: nfoValue{Value: embyMetadataID(key, drama.ID), Type: "duanju"}, Thumb: thumb}
	body, err := xml.MarshalIndent(show, "", "  ")
	if err != nil {
		return err
	}
	if err = write("tvshow.nfo", append([]byte(xml.Header), body...)); err != nil {
		return err
	}
	return nil
}

func writeEmbyEpisodeFile(show, title string, number int, path string, write func(string, []byte) error) error {
	episode := struct {
		XMLName xml.Name `xml:"episodedetails"`
		Title   string   `xml:"title"`
		Show    string   `xml:"showtitle"`
		Season  int      `xml:"season"`
		Episode int      `xml:"episode"`
	}{Title: title, Show: show, Season: 1, Episode: number}
	body, err := xml.MarshalIndent(episode, "", "  ")
	if err != nil {
		return err
	}
	return write(path+".nfo", append([]byte(xml.Header), body...))
}
