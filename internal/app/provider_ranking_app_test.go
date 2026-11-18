package app

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestRankingSeriesTarget(t *testing.T) {
	tests := map[string]string{
		"dragon8662://videoDetail?video_series_id=123456&from=rank": "123456",
		"dragon8662://videoDetail?video_series_id=abc":              "",
		"https://example.com/video_series_id=123":                   "",
		"": "",
	}
	for input, want := range tests {
		if got := rankingSeriesTarget(input); got != want {
			t.Fatalf("rankingSeriesTarget(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestParseRankingFilters(t *testing.T) {
	board := map[string]any{"panel_selector": map[string]any{"inner_rows": []any{
		map[string]any{"row_name": "综合", "type": "other", "items": []any{
			map[string]any{"selector_item_id": "", "show_name": "总榜"},
			map[string]any{"selector_item_id": "gender_female", "show_name": "女频"},
			map[string]any{"selector_item_id": "bad id", "show_name": "忽略"},
		}},
		map[string]any{"row_name": "", "items": []any{map[string]any{"selector_item_id": "x", "show_name": "无效组"}}},
	}}}
	groups := parseRankingFilters(board)
	if len(groups) != 1 || groups[0].Name != "综合" || groups[0].ID != "other" || len(groups[0].Options) != 2 {
		t.Fatalf("unexpected filters: %+v", groups)
	}
	if groups[0].Options[0].ID != "" || groups[0].Options[1].Name != "女频" {
		t.Fatalf("unexpected options: %+v", groups[0].Options)
	}
}

func TestLiveHongguoRankingApp(t *testing.T) {
	if os.Getenv("DUANJU_LIVE_RANKING") != "1" {
		t.Skip("仅在手动验收 App 榜单接口时启用")
	}
	cfg := defaultConfig()
	cfg.dataDir, cfg.OutputDir, cfg.Retries = t.TempDir(), t.TempDir(), 2
	d := NewDownloader(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	catalog, err := d.loadRankingAppCatalog(ctx, true)
	if err != nil {
		t.Fatal(publicError(err))
	}
	if len(catalog.Categories) < 4 || catalog.Default.Board == "" {
		t.Fatalf("catalog incomplete: %+v", catalog)
	}
	var human rankingCategoryInfo
	for _, category := range catalog.Categories {
		if category.ID == "human" {
			human = category
		}
	}
	if len(human.Boards) < 3 {
		t.Fatalf("human boards missing: %+v", human)
	}
	board := human.Boards[0]
	if len(board.Filters) == 0 {
		t.Fatalf("human filters missing: %+v", board)
	}
	selection := rankingAppSelection{Category: "human", Board: board.ID}
	page, err := d.loadRankingAppPage(ctx, selection, rankingAppCursor{}, true)
	if err != nil {
		t.Fatal(publicError(err))
	}
	if len(page.Items) == 0 || page.Items[0].Drama == nil || page.Items[0].Rank != 1 {
		t.Fatalf("page items invalid: %+v", page.Items)
	}
	first := page.Items[0]
	t.Logf("catalog categories=%d human boards=%d filters=%d page items=%d first=%s kind=%s metric=%s",
		len(catalog.Categories), len(human.Boards), len(board.Filters), len(page.Items), first.Title, first.Kind, first.Metric)
	if !page.HasMore || page.NextOffset <= 0 || page.NextSession == "" {
		t.Fatalf("pagination missing: %+v", page)
	}
	next, err := d.loadRankingAppPage(ctx, selection, rankingAppCursor{Offset: page.NextOffset, Session: page.NextSession, Version: page.NextVersion, FilterIDs: page.NextFilterIDs}, true)
	if err != nil {
		t.Fatal(publicError(err))
	}
	seen := map[string]bool{}
	for _, item := range page.Items {
		seen[item.ID] = true
	}
	for _, item := range next.Items {
		if seen[item.ID] {
			t.Fatalf("duplicate item across pages: %s", item.ID)
		}
		if item.Rank <= page.NextOffset {
			t.Fatalf("next page rank %d not after offset %d", item.Rank, page.NextOffset)
		}
	}
}
