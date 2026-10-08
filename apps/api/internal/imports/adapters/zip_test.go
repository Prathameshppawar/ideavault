package adapters

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"hash/crc32"
	"sort"
	"strings"
	"testing"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports/internal/fixtures"
)

// zipOf builds an in-memory zip archive (entries written in sorted order).
// Export archives are generated here from the JSON fixtures, so no binary
// fixture needs to be committed.
func zipOf(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, n := range names {
		f, err := w.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(files[n]); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestParseZipExports(t *testing.T) {
	r := NewRegistry()
	chatgpt := fixtures.Read(t, "chatgpt", "conversations.json")
	claude := fixtures.Read(t, "claude", "conversations.json")
	gemini := fixtures.Read(t, "gemini", "MyActivity.json")
	tests := []struct {
		name    string
		files   map[string][]byte
		adapter string
		convs   int
		entry   string
	}{
		{"chatgpt export", map[string][]byte{
			"conversations.json":    chatgpt,
			"chat.html":             []byte("<html></html>"),
			"user.json":             []byte(`{"id":"user-1"}`),
			"message_feedback.json": []byte(`[]`),
		}, "chatgpt/v1", 2, "conversations.json"},
		{"claude export", map[string][]byte{
			"conversations.json": claude,
			"users.json":         []byte(`[]`),
			"projects.json":      []byte(`[]`),
		}, "claude/v1", 2, "conversations.json"},
		{"google takeout", map[string][]byte{
			"Takeout/archive_browser.html":                    []byte("<html></html>"),
			"Takeout/My Activity/Search/MyActivity.json":      []byte(`[{"header":"Search","title":"Searched"}]`),
			"Takeout/My Activity/Gemini Apps/MyActivity.json": gemini,
		}, "gemini/v1", 2, "Takeout/My Activity/Gemini Apps/MyActivity.json"},
		{"shallowest conversations.json wins", map[string][]byte{
			"export/conversations.json":            chatgpt,
			"export/old/backup/conversations.json": []byte("not json"),
			"__MACOSX/export/._conversations.json": []byte("junk"),
		}, "chatgpt/v1", 2, "export/conversations.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := r.Parse(context.Background(), imports.Input{Filename: "export.zip", Data: zipOf(t, tt.files)}, "")
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if res.Adapter != tt.adapter || len(res.Conversations) != tt.convs {
				t.Fatalf("adapter=%s conversations=%d", res.Adapter, len(res.Conversations))
			}
			want := "imported " + tt.entry + " from zip archive"
			if len(res.Warnings) == 0 || res.Warnings[len(res.Warnings)-1] != want {
				t.Errorf("warnings = %v, want %q", res.Warnings, want)
			}
		})
	}
}

func TestParseZipExplicitAdapter(t *testing.T) {
	data := zipOf(t, map[string][]byte{"conversations.json": fixtures.Read(t, "chatgpt", "conversations.json")})
	res, err := NewRegistry().Parse(context.Background(), imports.Input{Data: data}, "chatgpt/v1")
	if err != nil || len(res.Conversations) != 2 {
		t.Fatalf("res=%v err=%v", res, err)
	}
}

func TestParseZipRejections(t *testing.T) {
	r := NewRegistry()
	valid := fixtures.Read(t, "chatgpt", "conversations.json")
	tests := []struct {
		name    string
		data    []byte
		want    error
		message string
	}{
		{"no supported entry", zipOf(t, map[string][]byte{"notes.txt": []byte("x"), "photos/ride.jpg": {0xff}}),
			imports.ErrUnsupportedFormat, "found: notes.txt, photos/ride.jpg"},
		{"activity of another product only", zipOf(t, map[string][]byte{"Takeout/My Activity/Search/MyActivity.json": []byte("[]")}),
			imports.ErrUnsupportedFormat, "MyActivity.json"},
		{"path traversal", zipOf(t, map[string][]byte{"conversations.json": valid, "../../etc/cron.d/evil": []byte("x")}),
			ErrUnsafeArchive, "unsafe path"},
		{"backslash traversal", zipOf(t, map[string][]byte{`..\evil.json`: []byte("x"), "conversations.json": valid}),
			ErrUnsafeArchive, ""},
		{"absolute path", zipOf(t, map[string][]byte{"/conversations.json": valid}), ErrUnsafeArchive, ""},
		{"drive letter", zipOf(t, map[string][]byte{"C:/conversations.json": valid}), ErrUnsafeArchive, ""},
		{"nested zip", zipOf(t, map[string][]byte{"conversations.json": zipOf(t, map[string][]byte{"a": []byte("b")})}),
			imports.ErrUnsupportedFormat, "nested"},
		{"corrupt archive", append([]byte("PK\x03\x04"), bytes.Repeat([]byte{0x01}, 64)...), imports.ErrUnsupportedFormat, "invalid zip"},
		{"empty archive", zipOf(t, map[string][]byte{}), imports.ErrUnsupportedFormat, "no files"},
		{"declared bomb", bombZip(t), imports.ErrTooLarge, "uncompressed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := r.Parse(context.Background(), imports.Input{Data: tt.data}, "")
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if tt.message != "" && !strings.Contains(err.Error(), tt.message) {
				t.Errorf("error %q does not mention %q", err, tt.message)
			}
		})
	}
}

// bombZip declares a 1 TiB entry; it must be rejected before decompression.
func bombZip(t *testing.T) []byte {
	t.Helper()
	data := []byte("[]")
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.CreateRaw(&zip.FileHeader{
		Name:               "conversations.json",
		Method:             zip.Store,
		CRC32:              crc32.ChecksumIEEE(data),
		CompressedSize64:   uint64(len(data)),
		UncompressedSize64: 1 << 40,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestReadEntryEnforcesBudget(t *testing.T) {
	data := zipOf(t, map[string][]byte{"conversations.json": bytes.Repeat([]byte("a"), 100)})
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	budget := int64(10)
	if _, err := readEntry(zr.File[0], &budget); !errors.Is(err, imports.ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	budget = 1000
	got, err := readEntry(zr.File[0], &budget)
	if err != nil || len(got) != 100 || budget != 900 {
		t.Fatalf("got %d bytes, budget %d, err %v", len(got), budget, err)
	}
}

func TestReadEntryRejectsLyingHeader(t *testing.T) {
	// The header claims 5 bytes but 100 are stored: reading must fail rather
	// than trust the header.
	payload := bytes.Repeat([]byte("b"), 100)
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.CreateRaw(&zip.FileHeader{Name: "conversations.json", Method: zip.Store, CRC32: crc32.ChecksumIEEE(payload),
		CompressedSize64: uint64(len(payload)), UncompressedSize64: 5})
	if err != nil {
		t.Fatal(err)
	}
	f.Write(payload)
	w.Close()
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	budget := int64(MaxZipTotalBytes)
	if _, err := readEntry(zr.File[0], &budget); err == nil {
		t.Fatal("lying header accepted")
	}
}

func TestSafeEntryName(t *testing.T) {
	tests := map[string]bool{
		"conversations.json":                    true,
		"Takeout/My Activity/x/MyActivity.json": true,
		"a/..b/c":                               true,
		"../x":                                  false,
		"a/../../x":                             false,
		`a\..\x`:                                false,
		"/etc/passwd":                           false,
		`\\server\share`:                        false,
		"C:/x":                                  false,
		"":                                      false,
		"a\x00b":                                false,
	}
	for name, want := range tests {
		if got := safeEntryName(name); got != want {
			t.Errorf("safeEntryName(%q) = %v, want %v", name, got, want)
		}
	}
}
