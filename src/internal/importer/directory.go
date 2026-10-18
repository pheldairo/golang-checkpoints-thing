// Package importer reads music metadata without changing source files.
package importer

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"setlist/internal/domain"
)

var (
	ErrDirectoryUnavailable = errors.New("import directory unavailable")
	ErrMetadata             = errors.New("unusable audio metadata")
	ErrReaderUnavailable    = errors.New("metadata reader unavailable")
)

type Metadata struct {
	Title, Artist, Album, Genre string
	DurationSeconds             int
	TrackNumber                 int
	MoodTags                    []string
}

type MetadataReader interface {
	Read(path string) (Metadata, error)
}

type FileError struct {
	Path  string
	Error string
}

type Summary struct {
	ScannedFiles    int
	ImportedTracks  int
	SkippedFiles    int
	MetadataErrors  []FileError
	TraversalErrors []FileError
}

type ScanResult struct {
	Tracks  []domain.Track
	Summary Summary
}

type Directory struct {
	Reader MetadataReader
}

func Supported(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3", ".flac", ".m4a", ".ogg", ".wav":
		return true
	}
	return false
}

// Nested symlinks are skipped, preventing loops and escapes.
func (d Directory) Scan(root string) (ScanResult, error) {
	result := ScanResult{
		Tracks: []domain.Track{},
		Summary: Summary{
			MetadataErrors:  []FileError{},
			TraversalErrors: []FileError{},
		},
	}
	if strings.TrimSpace(root) == "" {
		return result, fmt.Errorf("%w: directory path is required", ErrDirectoryUnavailable)
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return result, fmt.Errorf("%w: %v", ErrDirectoryUnavailable, err)
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return result, fmt.Errorf("%w: %v", ErrDirectoryUnavailable, err)
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() {
		return result, fmt.Errorf("%w: %q must be an accessible mounted directory", ErrDirectoryUnavailable, root)
	}
	// Catch an unreadable root before beginning a partial scan.
	if _, err = os.ReadDir(absolute); err != nil {
		return result, fmt.Errorf("%w: %v", ErrDirectoryUnavailable, err)
	}
	if d.Reader == nil {
		return result, ErrReaderUnavailable
	}
	seen := map[string]bool{}
	identities := []fs.FileInfo{}
	err = filepath.WalkDir(absolute, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			result.Summary.TraversalErrors = append(result.Summary.TraversalErrors, FileError{path, walkErr.Error()})
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		result.Summary.ScannedFiles++
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() || !Supported(path) {
			result.Summary.SkippedFiles++
			return nil
		}
		relative, err := filepath.Rel(absolute, path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(filepath.Clean(relative))
		if seen[key] {
			result.Summary.SkippedFiles++
			return nil
		}
		seen[key] = true
		identity, err := entry.Info()
		if err != nil {
			recordMetadataError(&result.Summary, key, err)
			return nil
		}
		for _, previous := range identities {
			if os.SameFile(previous, identity) {
				result.Summary.SkippedFiles++
				return nil
			}
		}
		identities = append(identities, identity)
		metadata, err := d.Reader.Read(path)
		if err != nil {
			recordMetadataError(&result.Summary, key, err)
			return nil
		}
		track := makeTrack(key, path, metadata)
		if err = track.Validate(); err != nil {
			recordMetadataError(&result.Summary, key, err)
			return nil
		}
		result.Tracks = append(result.Tracks, track)
		result.Summary.ImportedTracks++
		return nil
	})
	if err != nil {
		return result, fmt.Errorf("scan %q: %w", root, err)
	}
	sort.Slice(result.Tracks, func(i, j int) bool { return result.Tracks[i].ID < result.Tracks[j].ID })
	return result, nil
}

func recordMetadataError(summary *Summary, path string, err error) {
	summary.SkippedFiles++
	summary.MetadataErrors = append(summary.MetadataErrors, FileError{path, err.Error()})
}

func makeTrack(relative, path string, m Metadata) domain.Track {
	id := sha256.Sum256([]byte(relative))
	t := domain.Track{
		ID: fmt.Sprintf("track-%x", id), Title: domain.Clean(m.Title), Artist: domain.Clean(m.Artist),
		Album: domain.Clean(m.Album), Genre: domain.Clean(m.Genre), DurationSeconds: m.DurationSeconds,
		TrackNumber: m.TrackNumber, MoodTags: domain.CleanTags(m.MoodTags), SourcePath: path,
	}
	for _, field := range []struct{ name, value string }{
		{"title", t.Title}, {"artist", t.Artist}, {"album", t.Album}, {"genre", t.Genre},
	} {
		if !domain.Known(field.value) {
			t.MissingMetadata = append(t.MissingMetadata, field.name)
		}
	}
	if !domain.Known(t.Title) {
		t.Title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if !domain.Known(t.Album) {
		t.Album = filepath.Base(filepath.Dir(path))
	}
	if !domain.Known(t.Artist) {
		t.Artist = "Unknown"
	}
	if !domain.Known(t.Genre) {
		t.Genre = "Unknown"
	}
	return t
}
