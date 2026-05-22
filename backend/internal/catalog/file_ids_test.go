package catalog

import (
	"context"
	"sort"
	"testing"
	"time"
)

// TestListVideoFileIDsByDrive 校验 spider91 crawler 用到的轻量 file_id 查询：
// - 只返回指定 drive 的 file_id；不返回其它 drive 的
// - 跳过 file_id 为空的视频
// - 返回顺序无要求，但每个 file_id 只出现一次
func TestListVideoFileIDsByDrive(t *testing.T) {
	ctx := context.Background()
	cat, err := Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() { _ = cat.Close() })

	now := time.Now()
	insert := func(id, drive, fileID string) {
		if err := cat.UpsertVideo(ctx, &Video{
			ID:          id,
			DriveID:     drive,
			FileID:      fileID,
			Title:       id,
			PublishedAt: now,
		}); err != nil {
			t.Fatalf("upsert %s: %v", id, err)
		}
	}

	insert("spider91-A-vk001", "spider-a", "vk001.mp4")
	insert("spider91-A-vk002", "spider-a", "vk002.flv")
	insert("spider91-A-vk003", "spider-a", "vk003.mp4")
	// 不同 drive 的视频不应出现
	insert("quark-other-fid", "drive-quark", "abcdef")
	// 空 file_id 应被过滤
	insert("spider91-A-empty", "spider-a", "")

	got, err := cat.ListVideoFileIDsByDrive(ctx, "spider-a")
	if err != nil {
		t.Fatalf("ListVideoFileIDsByDrive: %v", err)
	}
	sort.Strings(got)
	want := []string{"vk001.mp4", "vk002.flv", "vk003.mp4"}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("got %d ids, want %d: got=%v", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// 空 drive 返回空列表，不报错
	other, err := cat.ListVideoFileIDsByDrive(ctx, "no-such-drive")
	if err != nil {
		t.Fatalf("ListVideoFileIDsByDrive empty: %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("non-existent drive: got %v, want empty", other)
	}
}
