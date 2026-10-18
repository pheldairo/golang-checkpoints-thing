package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// FFprobe invokes the read-only FFmpeg metadata inspector directly, without a
// shell. Only local file access is allowed; no network protocols are enabled.
type FFprobe struct {
	executable string
}

func NewFFprobe() (*FFprobe, error) {
	path, err := exec.LookPath("ffprobe")
	if err != nil {
		return nil, fmt.Errorf("%w: install FFmpeg (ffprobe) for directory imports: %v", ErrReaderUnavailable, err)
	}
	return &FFprobe{executable: path}, nil
}

type probeSection struct {
	Duration  string
	Tags      map[string]string
	CodecType string `json:"codec_type"`
}

type probeData struct {
	Format  probeSection
	Streams []probeSection
}

func (f *FFprobe) Read(path string) (Metadata, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Metadata{}, fmt.Errorf("%w: %v", ErrMetadata, err)
	}
	if !info.Mode().IsRegular() {
		return Metadata{}, fmt.Errorf("%w: not a regular file", ErrMetadata)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.executable,
		"-v", "error",
		"-protocol_whitelist", "file",
		"-format_whitelist", "mp3,flac,mov,ogg,wav",
		"-select_streams", "a",
		"-show_entries", "format=duration:format_tags=title,artist,album,genre,track,mood:stream=codec_type,duration:stream_tags=title,artist,album,genre,track,mood",
		"-of", "json", "-i", path,
	)
	data, err := cmd.Output()
	if err != nil {
		return Metadata{}, fmt.Errorf("%w: ffprobe failed: %v", ErrMetadata, err)
	}
	return decodeProbe(data)
}

func decodeProbe(data []byte) (Metadata, error) {
	var decoded probeData
	if err := json.Unmarshal(data, &decoded); err != nil {
		return Metadata{}, fmt.Errorf("%w: invalid ffprobe JSON: %v", ErrMetadata, err)
	}
	tags := map[string]string{}
	merge := func(values map[string]string) {
		keys := make([]string, 0, len(values))
		for k := range values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			k := strings.ToLower(key)
			if strings.TrimSpace(tags[k]) == "" {
				tags[k] = values[key]
			}
		}
	}
	merge(decoded.Format.Tags)
	duration := decoded.Format.Duration
	audio := false
	for _, stream := range decoded.Streams {
		if stream.CodecType == "audio" {
			audio = true
			merge(stream.Tags)
			if duration == "" || duration == "N/A" {
				duration = stream.Duration
			}
		}
	}
	seconds, err := strconv.ParseFloat(duration, 64)
	// Round upward so fractional real durations are never understated.
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 || seconds > 7*24*3600 || !audio {
		return Metadata{}, fmt.Errorf("%w: missing/invalid duration or no audio stream", ErrMetadata)
	}
	number := 0
	if track := strings.SplitN(tags["track"], "/", 2)[0]; strings.TrimSpace(track) != "" {
		number, err = strconv.Atoi(strings.TrimSpace(track))
		if err != nil || number < 0 {
			return Metadata{}, fmt.Errorf("%w: invalid track number %q", ErrMetadata, track)
		}
	}
	moods := []string{}
	if mood := strings.TrimSpace(tags["mood"]); mood != "" {
		moods = strings.FieldsFunc(mood, func(r rune) bool { return r == ';' || r == ',' })
	}
	return Metadata{
		Title: tags["title"], Artist: tags["artist"], Album: tags["album"], Genre: tags["genre"],
		DurationSeconds: int(math.Ceil(seconds)), TrackNumber: number, MoodTags: moods,
	}, nil
}
