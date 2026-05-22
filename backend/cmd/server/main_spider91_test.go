package main

import (
	"strconv"
	"testing"
	"time"

	"github.com/video-site/backend/internal/catalog"
)

func TestSpider91DueAtTriggerWindow(t *testing.T) {
	d := &catalog.Drive{
		ID:          "x",
		Credentials: map[string]string{"crawl_hour": "0"},
	}
	// 凌晨 0 点 → 触发
	now := time.Date(2026, 5, 22, 0, 30, 0, 0, time.Local)
	if !spider91DueAt(now, d) {
		t.Fatalf("expected due at 0:30 with hour=0")
	}
	// 1 点 → 不触发
	now2 := time.Date(2026, 5, 22, 1, 0, 0, 0, time.Local)
	if spider91DueAt(now2, d) {
		t.Fatalf("not expected due at 1:00 with hour=0")
	}
}

func TestSpider91DueAtCustomHour(t *testing.T) {
	d := &catalog.Drive{
		ID:          "x",
		Credentials: map[string]string{"crawl_hour": "3"},
	}
	due := time.Date(2026, 5, 22, 3, 5, 0, 0, time.Local)
	if !spider91DueAt(due, d) {
		t.Fatalf("expected due at 3:05 with hour=3")
	}
	notDue := time.Date(2026, 5, 22, 4, 5, 0, 0, time.Local)
	if spider91DueAt(notDue, d) {
		t.Fatalf("not expected due at 4:05 with hour=3")
	}
}

func TestSpider91DueAtRespectsLastCrawl(t *testing.T) {
	now := time.Date(2026, 5, 22, 0, 30, 0, 0, time.Local)

	// 上次刚跑过 1 小时 → 不触发
	d1 := &catalog.Drive{
		ID: "x",
		Credentials: map[string]string{
			"crawl_hour":    "0",
			"last_crawl_at": strconv.FormatInt(now.Add(-1*time.Hour).Unix(), 10),
		},
	}
	if spider91DueAt(now, d1) {
		t.Fatalf("not expected due 1h after last_crawl_at")
	}

	// 上次跑了 13 小时前（>=12h）→ 触发
	d2 := &catalog.Drive{
		ID: "x",
		Credentials: map[string]string{
			"crawl_hour":    "0",
			"last_crawl_at": strconv.FormatInt(now.Add(-13*time.Hour).Unix(), 10),
		},
	}
	if !spider91DueAt(now, d2) {
		t.Fatalf("expected due 13h after last_crawl_at")
	}
}

func TestSpider91DueAtFirstRun(t *testing.T) {
	d := &catalog.Drive{
		ID:          "x",
		Credentials: map[string]string{"crawl_hour": "0"},
	}
	now := time.Date(2026, 5, 22, 0, 30, 0, 0, time.Local)
	// 没有 last_crawl_at → 视为首次运行，命中窗口就触发
	if !spider91DueAt(now, d) {
		t.Fatalf("expected due on first run within window")
	}
}

func TestSpider91IntCredFallbacks(t *testing.T) {
	tests := []struct {
		name string
		d    *catalog.Drive
		key  string
		def  int
		want int
	}{
		{"nil drive", nil, "page", 1, 1},
		{"nil creds", &catalog.Drive{}, "page", 7, 7},
		{"empty value", &catalog.Drive{Credentials: map[string]string{"page": ""}}, "page", 5, 5},
		{"non-numeric", &catalog.Drive{Credentials: map[string]string{"page": "abc"}}, "page", 9, 9},
		{"happy", &catalog.Drive{Credentials: map[string]string{"page": "42"}}, "page", 1, 42},
		{"missing key", &catalog.Drive{Credentials: map[string]string{"a": "1"}}, "b", 99, 99},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := spider91IntCred(tc.d, tc.key, tc.def)
			if got != tc.want {
				t.Fatalf("spider91IntCred(%s) = %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}
