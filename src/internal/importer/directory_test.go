package importer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type readerFunc func(string) (Metadata, error)

func (f readerFunc) Read(path string) (Metadata, error) { return f(path) }
func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestRecursiveScanSummaryFallbackAndIdentity(t *testing.T) {
	root := t.TempDir()
	files := []string{"Album/01.MP3", "Album/02.flac", "Album/nested/03.M4A", "04.ogg", "05.WAV", "notes.txt", "bad.mp3", "unreadable.flac"}
	for _, file := range files {
		write(t, filepath.Join(root, file), []byte("synthetic"))
	}
	if err := os.Symlink(filepath.Join(root, "Album"), filepath.Join(root, "loop")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "Album/01.MP3"), filepath.Join(root, "alias.mp3")); err != nil {
		t.Fatal(err)
	}
	calls := map[string]int{}
	reader := readerFunc(func(path string) (Metadata, error) {
		calls[path]++
		switch filepath.Base(path) {
		case "bad.mp3":
			return Metadata{}, ErrMetadata
		case "unreadable.flac":
			return Metadata{}, os.ErrPermission
		}
		return Metadata{DurationSeconds: 42}, nil
	})
	d := Directory{Reader: reader}
	result, err := d.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.ScannedFiles != 10 || result.Summary.ImportedTracks != 5 || result.Summary.SkippedFiles != 5 || len(result.Summary.MetadataErrors) != 2 || len(result.Summary.TraversalErrors) != 0 {
		t.Fatalf("%+v", result.Summary)
	}
	for path, count := range calls {
		if count != 1 {
			t.Fatalf("read %s %d times", path, count)
		}
	}
	seen := map[string]bool{}
	for i, track := range result.Tracks {
		if seen[track.ID] {
			t.Fatal("duplicate ID")
		}
		seen[track.ID] = true
		if i > 0 && result.Tracks[i-1].ID >= track.ID {
			t.Fatal("not sorted")
		}
		if track.Artist != "Unknown" || track.Genre != "Unknown" || track.DurationSeconds != 42 || track.Title != strings.TrimSuffix(filepath.Base(track.SourcePath), filepath.Ext(track.SourcePath)) || track.Album != filepath.Base(filepath.Dir(track.SourcePath)) || len(track.MoodTags) != 0 || len(track.MissingMetadata) != 4 {
			t.Fatalf("%+v", track)
		}
	}
	again, err := d.Scan(root)
	if err != nil || !reflect.DeepEqual(result, again) {
		t.Fatalf("unstable scan: %v", err)
	}
	if result.Summary.ScannedFiles != result.Summary.ImportedTracks+result.Summary.SkippedFiles {
		t.Fatal("summary invariant")
	}
	// Same relative paths under another root have the same IDs.
	other := t.TempDir()
	write(t, filepath.Join(other, "Album/01.MP3"), []byte("synthetic"))
	moved, err := d.Scan(other)
	if err != nil {
		t.Fatal(err)
	}
	for _, track := range result.Tracks {
		if strings.HasSuffix(track.SourcePath, "Album/01.MP3") && track.ID != moved.Tracks[0].ID {
			t.Fatal("ID depends on mount path")
		}
	}
}
func TestDirectoryErrorsAndValidation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.mp3")
	write(t, path, []byte("bad"))
	d := Directory{Reader: readerFunc(func(string) (Metadata, error) { return Metadata{Title: "Bad", DurationSeconds: 0}, nil })}
	for _, path := range []string{filepath.Join(root, "absent"), path, "/dev/sda1"} {
		if _, err := d.Scan(path); !errors.Is(err, ErrDirectoryUnavailable) {
			t.Fatalf("%s: %v", path, err)
		}
	}
	result, err := d.Scan(root)
	if err != nil || result.Summary.ImportedTracks != 0 || result.Summary.SkippedFiles != 1 || len(result.Summary.MetadataErrors) != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	if _, err = (Directory{}).Scan(root); !errors.Is(err, ErrReaderUnavailable) {
		t.Fatal(err)
	}
}
func TestSupportedExtensions(t *testing.T) {
	for _, tc := range []struct {
		path      string
		supported bool
	}{{"a.MP3", true}, {"a.FLAC", true}, {"a.m4a", true}, {"a.OGG", true}, {"a.wav", true}, {"a.mp3.txt", false}, {"a", false}, {"a.aac", false}} {
		t.Run(tc.path, func(t *testing.T) {
			if Supported(tc.path) != tc.supported {
				t.Fatal(tc)
			}
		})
	}
}
func TestDecodeProbe(t *testing.T) {
	data := []byte(`{"format":{"duration":"10.2","tags":{"TITLE":"Embedded","artist":"Artist","track":"2/12","mood":"Calm; Warm,calm"}},"streams":[{"codec_type":"audio","duration":"11","tags":{"ALBUM":"Record","GENRE":"Jazz","title":"Stream title"}}]}`)
	m, err := decodeProbe(data)
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Embedded" || m.Artist != "Artist" || m.Album != "Record" || m.Genre != "Jazz" || m.TrackNumber != 2 || m.DurationSeconds != 11 {
		t.Fatalf("%+v", m)
	}
	track := makeTrack("a.flac", "/fixture/a.flac", m)
	if !reflect.DeepEqual(track.MoodTags, []string{"Calm", "Warm"}) || len(track.MissingMetadata) != 0 {
		t.Fatal(track)
	}
	fallback, err := decodeProbe([]byte(`{"streams":[{"codec_type":"audio","duration":"3.5"}]}`))
	if err != nil || fallback.DurationSeconds != 4 {
		t.Fatal(fallback, err)
	}
	for _, raw := range []string{`garbage`, `{}`, `{"format":{"duration":"NaN"},"streams":[{"codec_type":"audio"}]}`, `{"format":{"duration":"0"},"streams":[{"codec_type":"audio"}]}`, `{"format":{"duration":"3"},"streams":[{"codec_type":"video"}]}`, `{"format":{"duration":"3","tags":{"track":"oops"}},"streams":[{"codec_type":"audio"}]}`} {
		if _, err := decodeProbe([]byte(raw)); !errors.Is(err, ErrMetadata) {
			t.Fatalf("accepted %s: %v", raw, err)
		}
	}
}

// A one-second PCM silence WAV is generated in the test
func silenceWAV() []byte {
	var b bytes.Buffer
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+16000))
	b.WriteString("WAVEfmt ")
	_ = binary.Write(&b, binary.LittleEndian, uint32(16))
	for _, v := range []uint16{1, 1} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	for _, v := range []uint32{8000, 16000} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	for _, v := range []uint16{2, 16} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(16000))
	b.Write(make([]byte, 16000))
	return b.Bytes()
}
func TestFFprobeReadOnlyIntegration(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("optional FFprobe integration: ffprobe not installed")
	}
	reader, err := NewFFprobe()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	good := filepath.Join(root, "Album", "Silence.WAV")
	bad := filepath.Join(root, "bad.mp3")
	write(t, good, silenceWAV())
	write(t, bad, []byte("not an audio file"))
	before, err := os.Stat(good)
	if err != nil {
		t.Fatal(err)
	}
	result, err := (Directory{Reader: reader}).Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.ScannedFiles != 2 || result.Summary.ImportedTracks != 1 || result.Summary.SkippedFiles != 1 || len(result.Summary.MetadataErrors) != 1 {
		t.Fatalf("%+v", result.Summary)
	}
	track := result.Tracks[0]
	if track.Title != "Silence" || track.Album != "Album" || track.DurationSeconds != 1 {
		t.Fatalf("%+v", track)
	}
	after, err := os.Stat(good)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(good)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(contents, silenceWAV()) || !before.ModTime().Equal(after.ModTime()) || before.Mode() != after.Mode() {
		t.Fatal("source was modified")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 2 {
		t.Fatal("unexpected source writes", err)
	}
}

func TestHardLinkImportedOnce(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "a.mp3")
	write(t, first, []byte("synthetic"))
	if err := os.Link(first, filepath.Join(root, "b.mp3")); err != nil {
		t.Skipf("filesystem has no hard-link support: %v", err)
	}
	calls := 0
	result, err := (Directory{Reader: readerFunc(func(string) (Metadata, error) { calls++; return Metadata{DurationSeconds: 10}, nil })}).Scan(root)
	if err != nil || calls != 1 || result.Summary.ScannedFiles != 2 || result.Summary.ImportedTracks != 1 || result.Summary.SkippedFiles != 1 {
		t.Fatalf("%+v calls=%d err=%v", result.Summary, calls, err)
	}
}
