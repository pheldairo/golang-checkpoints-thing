package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestOfflineDemo(t *testing.T) {
	var first, second bytes.Buffer
	if err := run(nil, &first); err != nil {
		t.Fatal(err)
	}
	if err := run(nil, &second); err != nil {
		t.Fatal(err)
	}
	if first.String() != second.String() {
		t.Fatal("nondeterministic CLI")
	}
	for _, text := range []string{"Loaded sample library: 6 tracks", "task-0001", "task-0002", "task-0003", `"Status": "completed"`, `"TotalTracks": 6`, `"TotalDurationSeconds": 1350`, `"Score": 5`, `"Score": 6`} {
		if !strings.Contains(first.String(), text) {
			t.Fatalf("missing %s in %s", text, first.String())
		}
	}
}
func TestUnavailableDirectory(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"-library", t.TempDir() + "/absent"}, &out); err == nil {
		t.Fatal("unavailable directory succeeded")
	}
}
