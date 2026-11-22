package main

import (
	"bytes"
	"net"
	"strconv"
	"strings"
	"testing"

	"setlist/internal/domain"
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

func TestPoolFlagsAreValidated(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"-workers", "0"}, &out); err == nil {
		t.Fatal("zero workers accepted")
	}
	if err := run([]string{"-queue", "0"}, &out); err == nil {
		t.Fatal("zero queue capacity accepted")
	}
	if err := run([]string{"-player-actions", "status"}, &out); err == nil {
		t.Fatal("player actions without a player accepted")
	}
}

func closedPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func TestUnreachablePlayerFailsOnlyPlayerTasks(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"-player", "127.0.0.1", "-player-data-port", closedPort(t), "-player-actions", "status"}, &out)
	if err == nil || !strings.Contains(err.Error(), "1 of 4 tasks failed") {
		t.Fatalf("run error %v", err)
	}
	for _, text := range []string{"task-0001 analyze_library: completed", "task-0004 player_control (status): failed", "open player session"} {
		if !strings.Contains(out.String(), text) {
			t.Fatalf("missing %q in %s", text, out.String())
		}
	}
}

func TestParsePlayerActions(t *testing.T) {
	inputs, err := parsePlayerActions("status, volume:-2, volume-set:20, volume-up, seek, mode:4, play-track:3, library, next")
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.PlayerInput{
		{Action: domain.ActionStatus},
		{Action: domain.ActionSetVolume, Volume: -2, RelativeVolume: true},
		{Action: domain.ActionSetVolume, Volume: 20},
		{Action: domain.ActionSetVolume, Volume: 1, RelativeVolume: true},
		{Action: domain.ActionSeek, PositionMs: 30000},
		{Action: domain.ActionSetPlayMode, PlayMode: 4},
		{Action: domain.ActionPlayTrack, TrackIndex: 3},
		{Action: domain.ActionLibrary, Limit: 10},
		{Action: domain.ActionNext},
	}
	if len(inputs) != len(want) {
		t.Fatalf("parsed %d actions, want %d", len(inputs), len(want))
	}
	for index, input := range inputs {
		if input.Kind != domain.PlayerControl || *input.Player != want[index] {
			t.Errorf("action %d is %+v, want %+v", index, *input.Player, want[index])
		}
	}
	for _, bad := range []string{"", "dance", "status:1", "volume", "seek:soon", "volume-up:2"} {
		if _, err := parsePlayerActions(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
