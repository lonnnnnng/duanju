package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// long: 榜单接口与 App 7.3.5 的 RankingResponse 对齐：cell_selector 提供分类/榜型/筛选项，
// cell_data 提供条目，next_offset/session_id/rank_version/filter_ids 负责翻页。
const (
	hongguoRankingCellID   = "7470092475068071998"
	hongguoRankingTabType  = "26"
	hongguoRankingPath     = "/reading/bookapi/bookmall/cell/change/v/"
	hongguoRankingCacheTTL = 5 * time.Minute
	hongguoRankingCoverTTL = 24 * time.Hour
)

var (
	hongguoRankingIDPattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,80}$`)
	hongguoRankingSeriesID  = regexp.MustCompile(`^[0-9]{1,24}$`)
)

type rankingFilterOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type rankingFilterGroup struct {
	ID      string                `json:"id"`
	Name    string                `json:"name"`
	Options []rankingFilterOption `json:"options"`
}

type rankingBoardInfo struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Filters     []rankingFilterGroup `json:"filters,omitempty"`
}

type rankingCategoryInfo struct {
	ID     string             `json:"id"`
	Name   string             `json:"name"`
	Boards []rankingBoardInfo `json:"boards"`
}

type rankingAppSelection struct {
	Category string `json:"category"`
	Board    string `json:"board"`
	Filter   string `json:"filter,omitempty"`
}

type rankingAppCursor struct {
	Offset    int
	Session   string
	Version   string
	FilterIDs string
}

type rankingAppItem struct {
	Rank        int      `json:"rank"`
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Title       string   `json:"title"`
	Cover       string   `json:"cover,omitempty"`
	Metric      string   `json:"metric,omitempty"`
	Subtitle    string   `json:"subtitle,omitempty"`
	Description string   `json:"description,omitempty"`
	Label       string   `json:"label,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Episodes    int      `json:"episodes,omitempty"`
	SeriesID    string   `json:"seriesId,omitempty"`
	Playable    bool     `json:"playable"`
	Drama       *Drama   `json:"drama,omitempty"`
}

type rankingAppPage struct {
	Category       string               `json:"category"`
	Board          string               `json:"board"`
	Filter         string               `json:"filter,omitempty"`
	Offset         int                  `json:"offset"`
	Items          []rankingAppItem     `json:"items"`
	NextOffset     int                  `json:"nextOffset,omitempty"`
	HasMore        bool                 `json:"hasMore"`
	NextSession    string               `json:"nextSession,omitempty"`
	NextVersion    string               `json:"nextVersion,omitempty"`
	NextFilterIDs  string               `json:"nextFilterIds,omitempty"`
	Description    string               `json:"description,omitempty"`
	FilterGroups   []rankingFilterGroup `json:"filterGroups,omitempty"`
	SelectedFilter string               `json:"selectedFilter,omitempty"`
	UpdatedText    string               `json:"updatedText,omitempty"`
	FetchedAt      time.Time            `json:"fetchedAt"`
	Stale          bool                 `json:"stale,omitempty"`
	Warning        string               `json:"warning,omitempty"`
}

type rankingAppCatalog struct {
	Source     string                `json:"source"`
	Categories []rankingCategoryInfo `json:"categories"`
	Default    rankingAppSelection   `json:"default"`
	FetchedAt  time.Time             `json:"fetchedAt"`
}

func rankingPageKey(selection rankingAppSelection, offset int) string {
	return selection.Category + "|" + selection.Board + "|" + selection.Filter + "|" + strconv.Itoa(offset)
}

func (downloader *Downloader) loadRankingAppCatalog(ctx context.Context, refresh bool) (rankingAppCatalog, error) {
	client := downloader.hongguoClient()
	client.rankingMu.Lock()
	if !refresh && len(client.rankingCatalog.Categories) > 0 && time.Since(client.rankingCatalog.FetchedAt) < hongguoRankingCacheTTL {
		catalog := client.rankingCatalog
		client.rankingMu.Unlock()
		return catalog, nil
	}
	client.rankingMu.Unlock()
	selection := rankingAppSelection{Category: "all", Board: "ranklist_hot_play_sc"}
	page, categories, err := downloader.fetchRankingAppPage(ctx, selection, rankingAppCursor{})
	if err != nil {
		return rankingAppCatalog{}, err
	}
	defaults := rankingDefaultSelection(categories)
	if defaults.Board == "" {
		return rankingAppCatalog{}, errors.New("榜单未返回可用分类")
	}
	catalog := rankingAppCatalog{Source: sourceHongguo, Categories: categories, Default: defaults, FetchedAt: page.FetchedAt}
	client.rankingMu.Lock()
	client.rankingCatalog = catalog
	client.rankingMu.Unlock()
	return catalog, nil
}

func (downloader *Downloader) loadRankingAppPage(ctx context.Context, selection rankingAppSelection, cursor rankingAppCursor, refresh bool) (rankingAppPage, error) {
	client := downloader.hongguoClient()
	key := rankingPageKey(selection, cursor.Offset)
	client.rankingMu.Lock()
	if !refresh {
		if cached, found := client.rankingPages[key]; found && time.Since(cached.FetchedAt) < hongguoRankingCacheTTL {
			client.rankingMu.Unlock()
			return cached, nil
		}
	}
	client.rankingMu.Unlock()
	page, categories, err := downloader.fetchRankingAppPage(ctx, selection, cursor)
	client.rankingMu.Lock()
	defer client.rankingMu.Unlock()
	if err == nil {
		page.FetchedAt = time.Now()
		if refresh && cursor.Offset == 0 {
			prefix := selection.Category + "|" + selection.Board + "|" + selection.Filter + "|"
			for cachedKey := range client.rankingPages {
				if strings.HasPrefix(cachedKey, prefix) {
					delete(client.rankingPages, cachedKey)
				}
			}
		}
		if client.rankingPages == nil {
			client.rankingPages = map[string]rankingAppPage{}
		}
		if len(client.rankingPages) >= 256 {
			var oldestKey string
			var oldest time.Time
			for cachedKey, cached := range client.rankingPages {
				if oldestKey == "" || cached.FetchedAt.Before(oldest) {
					oldestKey, oldest = cachedKey, cached.FetchedAt
				}
			}
			delete(client.rankingPages, oldestKey)
		}
		client.rankingPages[key] = page
		if len(categories) > 0 {
			client.rankingCatalog = rankingAppCatalog{Source: sourceHongguo, Categories: categories, Default: rankingDefaultSelection(categories), FetchedAt: page.FetchedAt}
		}
		return page, nil
	}
	if cached, found := client.rankingPages[key]; found && ctx.Err() == nil && time.Since(cached.FetchedAt) < hongguoRankingCoverTTL {
		cached.Stale = true
		cached.Warning = "站点榜单暂不可用，显示上次取得的内容，请稍后刷新。"
		client.rankingPages[key] = cached
		return cached, nil
	}
	return rankingAppPage{}, err
}

func (downloader *Downloader) fetchRankingAppPage(ctx context.Context, selection rankingAppSelection, cursor rankingAppCursor) (rankingAppPage, []rankingCategoryInfo, error) {
	params := url.Values{}
	params.Set("user_is_login", "0")
	params.Set("client_template", "2")
	params.Set("sub_genre", "0")
	params.Set("unlimited_selector_change_type", "2")
	params.Set("related_book_id", "0")
	params.Set("screen_width_px", "1280")
	params.Set("inner_algo", "0")
	params.Set("category_id", "0")
	params.Set("selected_items", selection.Category)
	params.Set("tab_type", hongguoRankingTabType)
	params.Set("ecom_category_id", "0")
	params.Set("idol_tag_id", "0")
	params.Set("genre", "0")
	params.Set("limit", "0")
	params.Set("tag_id", "0")
	params.Set("ecom_refresh_type", "0")
	params.Set("inner_category_id", "0")
	params.Set("pad_column_cover", "0")
	params.Set("cell_id", hongguoRankingCellID)
	params.Set("sub_tag_id", "0")
	params.Set("rank_sub_info_id", "0")
	params.Set("cell_sub_id", "0")
	params.Set("genre_type", "0")
	params.Set("page", "0")
	params.Set("client_req_type", "2")
	params.Set("panel_selected_items", selection.Filter)
	params.Set("sub_selected_items", selection.Board)
	params.Set("offset", strconv.Itoa(cursor.Offset))
	if cursor.Session != "" {
		params.Set("session_id", cursor.Session)
	}
	if cursor.Version != "" {
		params.Set("rank_version", cursor.Version)
	}
	if cursor.FilterIDs != "" {
		params.Set("filter_ids", cursor.FilterIDs)
	}
	result, err := downloader.hongguoAppRequest(ctx, http.MethodGet, hongguoRankingPath, params, nil)
	if err != nil {
		return rankingAppPage{}, nil, err
	}
	data, _ := result["data"].(map[string]any)
	cell, _ := data["cell_view"].(map[string]any)
	if len(cell) == 0 {
		return rankingAppPage{}, nil, errors.New("榜单接口缺少 cell_view")
	}
	categories, err := parseRankingCategories(cell)
	if err != nil {
		return rankingAppPage{}, nil, err
	}
	page, err := parseRankingAppPage(data, cell, selection, cursor, categories)
	if err != nil {
		return rankingAppPage{}, nil, err
	}
	return page, categories, nil
}

func rankingDefaultSelection(categories []rankingCategoryInfo) rankingAppSelection {
	for _, category := range categories {
		if category.ID != "all" || len(category.Boards) == 0 {
			continue
		}
		return rankingAppSelection{Category: category.ID, Board: category.Boards[0].ID}
	}
	for _, category := range categories {
		if len(category.Boards) > 0 {
			return rankingAppSelection{Category: category.ID, Board: category.Boards[0].ID}
		}
	}
	return rankingAppSelection{}
}

func findRankingCategory(categories []rankingCategoryInfo, id string) (rankingCategoryInfo, bool) {
	for _, category := range categories {
		if category.ID == id {
			return category, true
		}
	}
	return rankingCategoryInfo{}, false
}

func findRankingAppBoard(categories []rankingCategoryInfo, categoryID, boardID string) (rankingBoardInfo, bool) {
	category, found := findRankingCategory(categories, categoryID)
	if !found {
		return rankingBoardInfo{}, false
	}
	for _, board := range category.Boards {
		if board.ID == boardID {
			return board, true
		}
	}
	return rankingBoardInfo{}, false
}

func parseRankingCategories(cell map[string]any) ([]rankingCategoryInfo, error) {
	selector, _ := cell["cell_selector"].(map[string]any)
	outer, _ := selector["outer_row"].(map[string]any)
	rows := anyList(outer["items"])
	if len(rows) == 0 {
		return nil, errors.New("榜单缺少分类选项")
	}
	categories := make([]rankingCategoryInfo, 0, len(rows))
	for _, value := range rows {
		row, _ := value.(map[string]any)
		id := mapString(row, "selector_item_id")
		name := mapString(row, "show_name")
		if !hongguoRankingIDPattern.MatchString(id) || strings.TrimSpace(name) == "" {
			continue
		}
		sub, _ := row["sub_cell_selector"].(map[string]any)
		subOuter, _ := sub["outer_row"].(map[string]any)
		boardRows := anyList(subOuter["items"])
		boards := make([]rankingBoardInfo, 0, len(boardRows))
		for _, boardValue := range boardRows {
			boardRow, _ := boardValue.(map[string]any)
			boardID := mapString(boardRow, "selector_item_id")
			boardName := mapString(boardRow, "show_name")
			if !hongguoRankingIDPattern.MatchString(boardID) || strings.TrimSpace(boardName) == "" {
				continue
			}
			boards = append(boards, rankingBoardInfo{
				ID:          boardID,
				Name:        boardName,
				Description: mapString(boardRow, "sub_title"),
				Filters:     parseRankingFilters(boardRow),
			})
		}
		if len(boards) == 0 {
			continue
		}
		categories = append(categories, rankingCategoryInfo{ID: id, Name: name, Boards: boards})
	}
	if len(categories) == 0 {
		return nil, errors.New("榜单分类选项格式异常")
	}
	return categories, nil
}

func parseRankingFilters(board map[string]any) []rankingFilterGroup {
	panel, _ := board["panel_selector"].(map[string]any)
	rows := anyList(panel["inner_rows"])
	groups := make([]rankingFilterGroup, 0, len(rows))
	for _, value := range rows {
		row, _ := value.(map[string]any)
		name := mapString(row, "row_name")
		if strings.TrimSpace(name) == "" {
			continue
		}
		options := make([]rankingFilterOption, 0)
		seen := map[string]bool{}
		for _, optionValue := range anyList(row["items"]) {
			option, _ := optionValue.(map[string]any)
			id := mapString(option, "selector_item_id")
			optionName := mapString(option, "show_name")
			if strings.TrimSpace(optionName) == "" || (id != "" && !hongguoRankingIDPattern.MatchString(id)) || seen[id] {
				continue
			}
			seen[id] = true
			options = append(options, rankingFilterOption{ID: id, Name: optionName})
		}
		if len(options) == 0 {
			continue
		}
		groups = append(groups, rankingFilterGroup{ID: firstNonEmpty(mapString(row, "type"), name), Name: name, Options: options})
	}
	return groups
}

func parseRankingAppPage(data, cell map[string]any, selection rankingAppSelection, cursor rankingAppCursor, categories []rankingCategoryInfo) (rankingAppPage, error) {
	board, found := findRankingAppBoard(categories, selection.Category, selection.Board)
	if !found {
		return rankingAppPage{}, errors.New("榜单分类或榜型已变化，请刷新")
	}
	items := parseRankingItems(cell, selection, cursor)
	nextOffset := mapIntValue(data, "next_offset")
	if nextOffset == 0 {
		nextOffset = mapIntValue(cell, "next_offset")
	}
	hasMore := mapBoolValue(data, "has_more") || mapBoolValue(cell, "has_more")
	session := firstNonEmpty(mapString(data, "session_id"), mapString(cell, "session_id"))
	version := firstNonEmpty(mapString(data, "rank_version"), mapString(cell, "rank_version"))
	filterIDs := buildRankingFilterIDs(cursor, items)
	more := hasMore && session != "" && nextOffset > cursor.Offset && nextOffset <= 10000
	page := rankingAppPage{
		Category:       selection.Category,
		Board:          selection.Board,
		Filter:         selection.Filter,
		Offset:         cursor.Offset,
		Items:          items,
		HasMore:        more,
		Description:    board.Description,
		FilterGroups:   board.Filters,
		SelectedFilter: selection.Filter,
		UpdatedText:    board.Description,
		FetchedAt:      time.Now(),
	}
	if more {
		page.NextOffset = nextOffset
		page.NextSession = session
		page.NextVersion = version
		page.NextFilterIDs = filterIDs
	}
	return page, nil
}

func buildRankingFilterIDs(cursor rankingAppCursor, items []rankingAppItem) string {
	ids := make([]string, 0)
	seen := map[string]bool{}
	for _, id := range strings.Split(cursor.FilterIDs, ",") {
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, item := range items {
		id := firstNonEmpty(item.SeriesID, item.ID)
		id = strings.TrimPrefix(id, "video:")
		id = strings.TrimPrefix(id, "person:")
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return strings.Join(ids, ",")
}

func parseRankingItems(cell map[string]any, selection rankingAppSelection, cursor rankingAppCursor) []rankingAppItem {
	shown := map[string]bool{}
	for _, id := range strings.Split(cursor.FilterIDs, ",") {
		if id = strings.TrimSpace(id); id != "" {
			shown[id] = true
		}
	}
	items := make([]rankingAppItem, 0, 10)
	rank := cursor.Offset + 1
	for _, containerValue := range anyList(cell["cell_data"]) {
		container, _ := containerValue.(map[string]any)
		for _, rowValue := range anyList(container["video_data"]) {
			row, _ := rowValue.(map[string]any)
			sourceID := mapString(row, "series_id")
			if !hongguoRankingSeriesID.MatchString(sourceID) || shown[sourceID] {
				continue
			}
			shown[sourceID] = true
			item := rankingVideoItem(row, selection, sourceID)
			if item == nil {
				continue
			}
			item.Rank = rank
			items = append(items, *item)
			rank++
		}
		for _, rowValue := range anyList(container["ugc_user_data"]) {
			row, _ := rowValue.(map[string]any)
			item := rankingPersonItem(row, rank)
			if item == nil {
				continue
			}
			item.Rank = rank
			items = append(items, *item)
			rank++
		}
	}
	return items
}

func rankingVideoItem(row map[string]any, selection rankingAppSelection, sourceID string) *rankingAppItem {
	target := sourceID
	kind := "drama"
	episodes := mapIntValue(row, "episode_cnt")
	if selection.Board != "" && strings.HasSuffix(selection.Board, "subscribe") || episodes == 0 {
		kind = "upcoming"
	}
	if selection.Category == "series_album" {
		kind = "series"
		if value := rankingSeriesTarget(mapString(row, "jump_schema")); value != "" {
			target = value
		}
	}
	disablePlay := mapBoolValue(row, "disable_play")
	playable := kind != "upcoming" && target != "" && !disablePlay
	reason := rankingJSONMap(row["recommend_reason_list"])
	metric := firstNonEmpty(
		mapString(nestedMap(row, "rec_text_item"), "RecommendText"),
		mapString(nestedMap(reason, "rec_text_item"), "RecommendText"),
		mapString(row, "rec_text"),
	)
	subtitleList := rankingLines(row["sub_title_list"])
	if len(subtitleList) == 0 {
		subtitleList = rankingLines(reason["sub_title_list"])
	}
	subtitle := strings.Join(subtitleList, " · ")
	if subtitle == "" {
		subtitle = mapString(row, "sub_title")
	}
	secondaryList := rankingLines(row["secondary_info_list"])
	if len(secondaryList) == 0 {
		secondaryList = rankingLines(reason["secondary_info_list"])
	}
	description := firstNonEmpty(
		mapString(row, "video_desc"),
		mapString(nestedMap(row, "video_detail"), "series_intro"),
	)
	if description == "" && kind == "upcoming" {
		description = strings.Join(secondaryList, " · ")
	}
	label := mapString(nestedMap(row, "tag_info"), "text")
	if label == "" {
		label = mapString(nestedMap(reason, "tag_info"), "text")
	}
	if label == "" {
		if kind == "upcoming" {
			label = "预约"
		} else if kind == "series" {
			label = "系列剧"
		}
	}
	tags := make([]string, 0, 4)
	for _, value := range secondaryList {
		if value == metric || value == description || len(tags) >= 4 {
			continue
		}
		tags = append(tags, value)
	}
	drama := hongguoDramaFromAny(row, "")
	if drama.ID == "" || drama.Title == "" {
		return nil
	}
	if kind == "series" {
		drama.ID = providerDramaID(sourceHongguo, target)
		drama.SourceID = target
	}
	drama.Desc, drama.Intro = description, description
	drama.Remark = subtitle
	if metric != "" {
		drama.Heat = metric
	}
	if episodes > 0 {
		drama.TotalEpisode, drama.EpisodeCount = strconv.Itoa(episodes), strconv.Itoa(episodes)
	}
	if len(subtitleList) > 0 {
		drama.CategoryName = subtitleList[0]
	}
	if len(tags) > 0 {
		for _, tag := range tags {
			if !containsString(drama.Tags, tag) {
				drama.Tags = append(drama.Tags, tag)
			}
		}
	}
	cover := firstNonEmpty(mapString(row, "cover"), mapString(row, "horiz_cover"))
	if cover != "" {
		drama.Cover, drama.CoverURL = cover, cover
	}
	return &rankingAppItem{
		ID:          "video:" + sourceID,
		Kind:        kind,
		Title:       drama.Title,
		Metric:      metric,
		Subtitle:    subtitle,
		Description: description,
		Label:       label,
		Tags:        tags,
		Episodes:    episodes,
		SeriesID:    sourceID,
		Playable:    playable,
		Drama:       &drama,
	}
}

func rankingPersonItem(row map[string]any, rank int) *rankingAppItem {
	user, _ := row["user_info"].(map[string]any)
	base, _ := user["base_info"].(map[string]any)
	if len(base) == 0 {
		return nil
	}
	userID := firstNonEmpty(mapString(user, "user_id"), mapString(base, "user_id"))
	if userID == "" {
		return nil
	}
	title := mapString(base, "user_name")
	if title == "" {
		return nil
	}
	cover := firstNonEmpty(mapString(base, "user_avatar"), mapString(base, "avatar"))
	metric := mapString(nestedMap(row, "rec_text_item"), "RecommendText")
	subtitle := rankingLines(row["sub_title_list"])
	description := mapString(row, "intro")
	return &rankingAppItem{
		ID:          "person:" + userID,
		Kind:        "person",
		Title:       title,
		Cover:       cover,
		Metric:      metric,
		Subtitle:    strings.Join(subtitle, " · "),
		Description: description,
		Label:       "人物",
		Playable:    false,
	}
}

func rankingSeriesTarget(schema string) string {
	if schema == "" {
		return ""
	}
	parsed, err := url.Parse(schema)
	if err != nil || parsed.Scheme != "dragon8662" || parsed.Host != "videoDetail" {
		return ""
	}
	for _, pair := range strings.Split(parsed.RawQuery, "&") {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 || parts[0] != "video_series_id" {
			continue
		}
		value, err := url.QueryUnescape(parts[1])
		if err == nil && hongguoRankingSeriesID.MatchString(value) {
			return value
		}
	}
	return ""
}

func rankingJSONMap(value any) map[string]any {
	text, _ := value.(string)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var data map[string]any
	if json.Unmarshal([]byte(text), &data) != nil {
		return nil
	}
	return data
}

func rankingLines(value any) []string {
	lines := make([]string, 0)
	for _, itemValue := range anyList(value) {
		item, _ := itemValue.(map[string]any)
		content := strings.TrimSpace(mapString(item, "content"))
		if content != "" && !containsString(lines, content) {
			lines = append(lines, content)
		}
	}
	return lines
}

func containsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func mapIntValue(m map[string]any, key string) int {
	switch value := m[key].(type) {
	case json.Number:
		number, err := value.Int64()
		if err == nil {
			return int(number)
		}
	case float64:
		return int(value)
	case string:
		number, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil {
			return number
		}
	case int:
		return value
	}
	return 0
}

func mapBoolValue(m map[string]any, key string) bool {
	switch value := m[key].(type) {
	case bool:
		return value
	case json.Number:
		return value.String() != "0"
	case float64:
		return value != 0
	case string:
		text := strings.TrimSpace(strings.ToLower(value))
		return text == "1" || text == "true"
	}
	return false
}

func (a *UIApp) normalizeRankingAppPage(page *rankingAppPage) {
	for index := range page.Items {
		item := &page.Items[index]
		if item.Drama != nil {
			raw := bestDramaCover(*item.Drama)
			if raw != "" {
				if remote, ok := buildImageURL(raw); ok {
					a.allowRankingCover(remote)
				}
			}
			normalizeDramaCover(item.Drama)
			if cover, ok := item.Drama.Cover.(string); ok {
				item.Cover = cover
			}
			continue
		}
		raw := strings.TrimSpace(item.Cover)
		if raw == "" || strings.HasPrefix(raw, "/api/ui/image") {
			continue
		}
		if remote, ok := buildImageURL(raw); ok {
			a.allowRankingCover(remote)
		}
		item.Cover = "/api/ui/image?url=" + url.QueryEscape(raw)
	}
}

func (a *UIApp) allowRankingCover(remote string) {
	if remote == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.rankingCovers == nil {
		a.rankingCovers = map[string]time.Time{}
	}
	now := time.Now()
	for key, until := range a.rankingCovers {
		if now.After(until) {
			delete(a.rankingCovers, key)
		}
	}
	if len(a.rankingCovers) >= 1024 {
		var oldestKey string
		var oldest time.Time
		for key, until := range a.rankingCovers {
			if oldestKey == "" || until.Before(oldest) {
				oldestKey, oldest = key, until
			}
		}
		delete(a.rankingCovers, oldestKey)
	}
	a.rankingCovers[remote] = now.Add(hongguoRankingCoverTTL)
}

func (a *UIApp) acceptRankingAppDramas(key string, fetchedAt time.Time, stale bool, dramas []Drama) {
	if len(dramas) == 0 {
		return
	}
	a.mu.Lock()
	if a.rankingApplied == nil {
		a.rankingApplied = map[string]time.Time{}
	}
	if a.rankingApplied[key].Equal(fetchedAt) {
		a.mu.Unlock()
		return
	}
	if len(a.rankingApplied) >= 256 {
		a.rankingApplied = map[string]time.Time{}
	}
	a.rankingApplied[key] = fetchedAt
	fresh := onlyHongguoDramas(dramas)
	if len(fresh) == 0 {
		a.mu.Unlock()
		return
	}
	normalized := make([]Drama, 0, len(fresh))
	for _, drama := range fresh {
		normalizeDramaCover(&drama)
		normalized = append(normalized, drama)
	}
	newDramas := a.newSortMetadataDramasLocked(normalized)
	if stale {
		a.dramas = mergeLoadedDramas(normalized, a.dramas, nil)
	} else {
		a.dramas = mergeLoadedDramas(a.dramas, normalized, nil)
	}
	a.enqueueSortMetadataLocked(newDramas, false)
	if a.librarySources == nil {
		a.librarySources = map[string]librarySourceState{}
	}
	state := a.librarySources[sourceHongguo]
	state.Count = 0
	for _, drama := range a.dramas {
		if dramaProvider(drama) == sourceHongguo {
			state.Count++
		}
	}
	if state.Status == "" {
		state.Status = "ready"
	}
	if fetchedAt.After(state.UpdatedAt) {
		state.UpdatedAt = fetchedAt
	}
	a.librarySources[sourceHongguo] = state
	if fetchedAt.After(a.loadedAt) {
		a.loadedAt = fetchedAt
	}
	a.libraryDirty = true
	a.libraryRevision++
	a.mu.Unlock()
	a.persistLibrary()
}

func (a *UIApp) handleRankingCatalog(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 40*time.Second)
	defer cancel()
	catalog, err := a.downloader.loadRankingAppCatalog(ctx, request.URL.Query().Get("refresh") == "1")
	if err != nil {
		if request.Context().Err() != nil {
			return
		}
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "站点榜单暂不可用，请稍后重试"})
		return
	}
	writeJSON(writer, http.StatusOK, catalog)
}

func (a *UIApp) handleRankingAppPage(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	query := request.URL.Query()
	selection := rankingAppSelection{
		Category: firstNonEmpty(query.Get("category"), "all"),
		Board:    query.Get("board"),
		Filter:   query.Get("filter"),
	}
	if !hongguoRankingIDPattern.MatchString(selection.Category) || !hongguoRankingIDPattern.MatchString(selection.Board) ||
		(selection.Filter != "" && !hongguoRankingIDPattern.MatchString(selection.Filter)) {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "榜单分类或筛选项无效"})
		return
	}
	offset, err := strconv.Atoi(firstNonEmpty(query.Get("offset"), "0"))
	if err != nil || offset < 0 || offset > 10000 {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "榜单页码无效"})
		return
	}
	cursor := rankingAppCursor{
		Offset:    offset,
		Session:   query.Get("session"),
		Version:   query.Get("rankVersion"),
		FilterIDs: query.Get("filterIds"),
	}
	ctx, cancel := context.WithTimeout(request.Context(), 40*time.Second)
	defer cancel()
	page, err := a.downloader.loadRankingAppPage(ctx, selection, cursor, query.Get("refresh") == "1")
	if err != nil {
		if request.Context().Err() != nil {
			return
		}
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "站点榜单暂不可用，请稍后重试"})
		return
	}
	if page.Stale {
		writeJSON(writer, http.StatusOK, page)
		return
	}
	dramas := make([]Drama, 0, len(page.Items))
	for index := range page.Items {
		if page.Items[index].Drama != nil {
			dramas = append(dramas, *page.Items[index].Drama)
		}
	}
	if len(dramas) > 0 {
		key := "app:" + rankingPageKey(selection, offset)
		a.acceptRankingAppDramas(key, page.FetchedAt, false, dramas)
	}
	a.normalizeRankingAppPage(&page)
	writeJSON(writer, http.StatusOK, page)
}
