package spider91

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDriverInitCreatesSubdirs(t *testing.T) {
	dir := t.TempDir()
	d := New(Config{ID: "test", RootDir: filepath.Join(dir, "drive1")})
	if err := d.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	for _, sub := range []string{"videos", "thumbs"} {
		info, err := os.Stat(filepath.Join(dir, "drive1", sub))
		if err != nil {
			t.Fatalf("stat %s: %v", sub, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a dir", sub)
		}
	}
}

func TestDriverInitRejectsEmptyRoot(t *testing.T) {
	d := New(Config{ID: "test", RootDir: ""})
	if err := d.Init(context.Background()); err == nil {
		t.Fatalf("expected error for empty root")
	}
}

func TestVideoPathRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	d := New(Config{ID: "test", RootDir: dir})
	if err := d.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	cases := []string{
		"",
		"   ",
		"../etc/passwd",
		"sub/dir.mp4",
		"./abc.mp4",
	}
	for _, c := range cases {
		if _, err := d.VideoPath(c); err == nil {
			t.Fatalf("VideoPath(%q) accepted, want error", c)
		}
		if _, err := d.ThumbPath(c); err == nil {
			t.Fatalf("ThumbPath(%q) accepted, want error", c)
		}
	}
}

func TestVideoPathHappy(t *testing.T) {
	dir := t.TempDir()
	d := New(Config{ID: "test", RootDir: dir})
	if err := d.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	got, err := d.VideoPath("abc.mp4")
	if err != nil {
		t.Fatalf("VideoPath: %v", err)
	}
	want := filepath.Join(dir, "videos", "abc.mp4")
	wantAbs, _ := filepath.Abs(want)
	if got != wantAbs {
		t.Fatalf("VideoPath: got %q want %q", got, wantAbs)
	}
}

func TestListReturnsFiles(t *testing.T) {
	dir := t.TempDir()
	d := New(Config{ID: "test", RootDir: dir})
	if err := d.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustWrite(t, filepath.Join(d.VideosDir(), "abc.mp4"), "data")
	mustWrite(t, filepath.Join(d.VideosDir(), "def.mp4"), "x")

	entries, err := d.List(context.Background(), "/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("List len = %d, want 2", len(entries))
	}
	names := map[string]int64{}
	for _, e := range entries {
		names[e.Name] = e.Size
	}
	if names["abc.mp4"] != 4 || names["def.mp4"] != 1 {
		t.Fatalf("unexpected entries: %+v", names)
	}
}

func TestStreamURLReturnsLocalPath(t *testing.T) {
	dir := t.TempDir()
	d := New(Config{ID: "test", RootDir: dir})
	if err := d.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustWrite(t, filepath.Join(d.VideosDir(), "abc.mp4"), "videodata")

	link, err := d.StreamURL(context.Background(), "abc.mp4")
	if err != nil {
		t.Fatalf("StreamURL: %v", err)
	}
	if !strings.HasSuffix(link.URL, "videos/abc.mp4") {
		t.Fatalf("StreamURL.URL = %q, want suffix videos/abc.mp4", link.URL)
	}
}

func TestStreamURLEmptyFile(t *testing.T) {
	dir := t.TempDir()
	d := New(Config{ID: "test", RootDir: dir})
	if err := d.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	mustWrite(t, filepath.Join(d.VideosDir(), "abc.mp4"), "")
	if _, err := d.StreamURL(context.Background(), "abc.mp4"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty file should return os.ErrNotExist, got %v", err)
	}
}

func TestBuildVideoIDStable(t *testing.T) {
	id1 := BuildVideoID("crawler1", "abc")
	id2 := BuildVideoID("crawler1", "abc")
	if id1 != id2 {
		t.Fatalf("BuildVideoID not deterministic")
	}
	if id1 != "spider91-crawler1-abc" {
		t.Fatalf("BuildVideoID format unexpected: %q", id1)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}
