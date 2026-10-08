package adapters

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
)

// ZIP safety limits. Archives are only ever read in memory, never extracted
// to disk.
const (
	// MaxZipEntryBytes caps the decompressed size of one archive entry.
	MaxZipEntryBytes = 256 << 20
	// MaxZipTotalBytes caps the decompressed bytes read from one archive.
	MaxZipTotalBytes = 512 << 20
	// MaxZipEntries caps the number of entries an archive may list.
	MaxZipEntries = 100_000
)

// ErrUnsafeArchive is returned for archives with path-traversal or absolute
// entry names. Such archives are rejected outright.
var ErrUnsafeArchive = errors.New("unsafe zip archive")

func isZip(data []byte) bool {
	return bytes.HasPrefix(data, []byte("PK\x03\x04")) || bytes.HasPrefix(data, []byte("PK\x05\x06"))
}

// parseZip locates the export file inside an archive (ChatGPT or Claude
// conversations.json, or Gemini's MyActivity.json) and parses it.
func (r *Registry) parseZip(ctx context.Context, in imports.Input, adapterName string) (*imports.ParseResult, error) {
	zr, err := zip.NewReader(bytes.NewReader(in.Data), int64(len(in.Data)))
	if err != nil {
		if errors.Is(err, zip.ErrInsecurePath) {
			return nil, fmt.Errorf("%w: %v", ErrUnsafeArchive, err)
		}
		return nil, fmt.Errorf("%w: invalid zip archive: %v", imports.ErrUnsupportedFormat, err)
	}
	if len(zr.File) > MaxZipEntries {
		return nil, fmt.Errorf("%w: zip archive lists %d entries (limit %d)", imports.ErrTooLarge, len(zr.File), MaxZipEntries)
	}
	for _, f := range zr.File {
		if !safeEntryName(f.Name) {
			return nil, fmt.Errorf("%w: entry %q has an unsafe path", ErrUnsafeArchive, f.Name)
		}
	}
	entry := selectEntry(zr.File)
	if entry == nil {
		return nil, fmt.Errorf("%w: zip archive contains no supported export (expected conversations.json from ChatGPT/Claude or Gemini Apps MyActivity.json from Google Takeout); found: %s",
			imports.ErrUnsupportedFormat, listEntries(zr.File, 20))
	}
	budget := int64(MaxZipTotalBytes)
	data, err := readEntry(entry, &budget)
	if err != nil {
		return nil, err
	}
	if isZip(data) {
		return nil, fmt.Errorf("%w: nested zip archives are not supported", imports.ErrUnsupportedFormat)
	}
	sub := imports.Input{Filename: entry.Name, ContentType: "application/json", Data: data, URL: in.URL}
	res, err := r.parsePlain(ctx, sub, adapterName)
	if err != nil {
		return nil, err
	}
	res.Warnings = append(res.Warnings, "imported "+entry.Name+" from zip archive")
	return res, nil
}

// safeEntryName rejects absolute paths, drive letters, NUL bytes and any
// ".." path segment.
func safeEntryName(name string) bool {
	n := strings.ReplaceAll(name, `\`, "/")
	if n == "" || strings.HasPrefix(n, "/") || strings.ContainsRune(n, 0) {
		return false
	}
	if len(n) >= 2 && n[1] == ':' {
		return false
	}
	for _, seg := range strings.Split(n, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

// selectEntry picks the shallowest conversations.json, else the shallowest
// MyActivity.json under a Gemini/Bard folder.
func selectEntry(files []*zip.File) *zip.File {
	var convs, gemini []*zip.File
	for _, f := range files {
		name := strings.ReplaceAll(f.Name, `\`, "/")
		if f.FileInfo().IsDir() || strings.HasPrefix(name, "__MACOSX/") {
			continue
		}
		base := strings.ToLower(path.Base(name))
		lower := strings.ToLower(name)
		switch {
		case base == "conversations.json":
			convs = append(convs, f)
		case (base == "myactivity.json" || base == "my activity.json") &&
			(strings.Contains(lower, "gemini") || strings.Contains(lower, "bard")):
			gemini = append(gemini, f)
		}
	}
	for _, group := range [][]*zip.File{convs, gemini} {
		if len(group) == 0 {
			continue
		}
		sort.SliceStable(group, func(i, j int) bool {
			return strings.Count(group[i].Name, "/") < strings.Count(group[j].Name, "/")
		})
		return group[0]
	}
	return nil
}

// readEntry decompresses f, enforcing MaxZipEntryBytes and the remaining
// archive budget regardless of the (untrusted) sizes in the header.
func readEntry(f *zip.File, budget *int64) ([]byte, error) {
	limit := int64(MaxZipEntryBytes)
	if *budget < limit {
		limit = *budget
	}
	if f.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("%w: %s is %d MiB uncompressed (limit %d MiB)", imports.ErrTooLarge, f.Name, f.UncompressedSize64>>20, limit>>20)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: cannot open %s: %v", imports.ErrUnsupportedFormat, f.Name, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: cannot read %s: %v", imports.ErrUnsupportedFormat, f.Name, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: %s exceeds %d MiB uncompressed", imports.ErrTooLarge, f.Name, limit>>20)
	}
	*budget -= int64(len(data))
	return data, nil
}

func listEntries(files []*zip.File, max int) string {
	var names []string
	for _, f := range files {
		if f.FileInfo().IsDir() {
			continue
		}
		if len(names) == max {
			names = append(names, fmt.Sprintf("... (%d entries total)", len(files)))
			break
		}
		names = append(names, imports.Truncate(imports.SanitizeText(f.Name), 120))
	}
	if len(names) == 0 {
		return "no files"
	}
	return strings.Join(names, ", ")
}
