# KBTU Golang Checkpoints

This program is a Go command-line demo for working with a music library. It reports
library statistics, missing metadata, and possible duplicates; generates a
playlist using genre, mood, and duration preferences; and finds similar tracks.
Tasks and results are stored in memory for the current run.

Requires **Go 1.27**. The Go module is in `src/`, so run commands from there:

```sh
cd src
go run ./cmd/setlist
```

The default run uses six built-in sample tracks and prints task statuses and
JSON results. To scan your own music directory, install FFmpeg with `ffprobe`
available on `PATH`, then run:

```sh
go run ./cmd/setlist -library /path/to/music
```

Imports recursively read metadata from MP3, FLAC, M4A, OGG, and WAV files without
modifying them. The demo runs the same three tasks with preset preferences for
both sample and imported libraries.

Build and verify before submission (from `src/`):

```sh
go version # Should be Go 1.27, remember
go build ./...
gofmt -l .
go vet ./...
go test ./...
```

`gofmt -l .` should produce no output; all other commands should succeed.
