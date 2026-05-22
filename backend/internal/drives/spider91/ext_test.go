package spider91

import "testing"

func TestDetectVideoExt(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"mp4 with token", "https://cdn.example.com/mp43/abc.mp4?st=xyz&e=12345", ".mp4"},
		{"webm", "https://cdn.example.com/path/video.webm?token=1", ".webm"},
		{"mkv", "https://cdn.example.com/path/foo.mkv", ".mkv"},
		{"mov", "https://cdn.example.com/path/foo.mov?x=1", ".mov"},
		{"flv", "https://cdn.example.com/path/foo.flv", ".flv"},
		{"m4v", "https://cdn.example.com/path/foo.m4v", ".m4v"},
		{"avi", "https://cdn.example.com/path/foo.avi", ".avi"},
		{"m3u8 fallback to mp4", "https://cdn.example.com/path/playlist.m3u8", ".mp4"},
		{"ts fallback to mp4", "https://cdn.example.com/path/seg001.ts", ".mp4"},
		{"unknown ext fallback", "https://cdn.example.com/path/foo.weird", ".mp4"},
		{"no ext fallback", "https://cdn.example.com/v.php?id=12345", ".mp4"},
		{"empty url", "", ".mp4"},
		{"uppercase", "https://cdn.example.com/path/FOO.MP4?token=1", ".mp4"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := detectVideoExt(tc.url)
			if got != tc.want {
				t.Fatalf("detectVideoExt(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}

func TestDetectThumbExt(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://cdn.example.com/thumb/foo.jpg", ".jpg"},
		{"https://cdn.example.com/thumb/foo.jpeg", ".jpeg"},
		{"https://cdn.example.com/thumb/foo.png", ".png"},
		{"https://cdn.example.com/thumb/foo.webp", ".webp"},
		{"https://cdn.example.com/thumb/foo.gif", ".gif"},
		{"https://cdn.example.com/thumb/foo.svg", ".jpg"}, // not in whitelist
		{"https://cdn.example.com/thumb/no-ext", ".jpg"},
		{"", ".jpg"},
	}
	for _, tc := range tests {
		got := detectThumbExt(tc.url)
		if got != tc.want {
			t.Fatalf("detectThumbExt(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}
