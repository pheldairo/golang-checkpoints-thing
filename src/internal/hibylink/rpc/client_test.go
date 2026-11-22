package rpc

import (
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	"setlist/internal/hibylink/protocol"
	"setlist/internal/hibylink/scripted"
)

func connectClient(t *testing.T, handler scripted.Handler) (*Client, *scripted.Server) {
	t.Helper()
	server := scripted.Start(t, handler)
	conn, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(server.DataPort())))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return NewClient(conn, time.Second), server
}

func volumeReply(volume int) scripted.Handler {
	return func(call scripted.Call) scripted.Reply {
		return scripted.Reply{Signature: "i", Values: []any{volume}}
	}
}

func TestCallReturnsTheMatchingReply(t *testing.T) {
	client, _ := connectClient(t, volumeReply(42))
	for range 3 {
		reply, err := client.Call(GetVolume)
		if err != nil || reply.Result() != 42 {
			t.Fatalf("reply %+v, %v", reply, err)
		}
	}
}

func TestArgumentOutsideScopeIsRefusedWithoutSending(t *testing.T) {
	client, server := connectClient(t, volumeReply(0))
	refusals := []struct {
		op   Operation
		args []any
	}{
		{SetVolume, []any{300}},
		{SetPlayState, []any{3}},
		{SetPlayMode, []any{0}},
		{GetPlaylistItems, []any{AllTracksList, 0, 80}},
		{GetPlaylistItems, []any{"1#1@[05]", 0, 1}},
		{PlayUUID, []any{AllTracksList, `<path>a:\x.cue<cue_id>2<name>x`}},
		{GetVolume, []any{1}},
	}
	for _, refusal := range refusals {
		if _, err := client.Call(refusal.op, refusal.args...); !errors.Is(err, ErrArgumentRefused) {
			t.Errorf("%s %v was not refused: %v", refusal.op.Name, refusal.args, err)
		}
	}
	if server.Calls.Load() != 0 || client.Failure() != nil {
		t.Fatalf("refusals reached the player (%d calls) or poisoned the client", server.Calls.Load())
	}
	if _, err := client.Call(SetVolume, 20); err != nil {
		t.Fatalf("a refused argument must not poison the connection: %v", err)
	}
}

func TestWrongSerialPoisonsTheConnection(t *testing.T) {
	client, server := connectClient(t, func(scripted.Call) scripted.Reply {
		return scripted.Reply{Signature: "i", Values: []any{1}, SerialShift: 1}
	})
	if _, err := client.Call(GetVolume); !errors.Is(err, protocol.ErrProtocol) {
		t.Fatalf("mismatched serial accepted: %v", err)
	}
	if _, err := client.Call(GetVolume); !errors.Is(err, ErrPoisoned) {
		t.Fatalf("poisoned client sent again: %v", err)
	}
	if server.Calls.Load() != 1 {
		t.Fatalf("player saw %d calls, want 1", server.Calls.Load())
	}
}

func TestUnexpectedSuccessSignatureIsRejected(t *testing.T) {
	client, _ := connectClient(t, func(scripted.Call) scripted.Reply {
		return scripted.Reply{Signature: "is", Values: []any{0, "surprise"}}
	})
	if _, err := client.Call(GetVolume); !errors.Is(err, protocol.ErrProtocol) {
		t.Fatalf("unexpected signature accepted: %v", err)
	}
}

func TestNegativeResultIsReturnedNotFailed(t *testing.T) {
	client, _ := connectClient(t, volumeReply(-1))
	reply, err := client.Call(GetMetaInfo)
	if err != nil || reply.Result() != -1 {
		t.Fatalf("reply %+v, %v", reply, err)
	}
}

func TestLocatorParsingAndMetadataPath(t *testing.T) {
	locator, err := ParseLocator(`<path>a:\Music\song.flac<cue_id>-1<name>machinegirl_<3`)
	if err != nil || locator.Path != `a:\Music\song.flac` || locator.CueID != -1 || locator.Name != "machinegirl_<3" {
		t.Fatalf("locator %+v, %v", locator, err)
	}
	path, err := MetadataPath(locator.Path)
	if err != nil || path != `\a\Music\song.flac` {
		t.Fatalf("metadata path %q, %v", path, err)
	}
	if _, err := ParseLocator("song.flac"); !errors.Is(err, ErrArgumentRefused) {
		t.Fatal("malformed locator accepted")
	}
}

func TestAlbumIdentifiersAreAllowedOnlyInTheirReviewedShape(t *testing.T) {
	album, err := AlbumIdentifier("Abbey Road")
	if err != nil || album != "1#1@[05]Abbey Road" || !IsAlbumIdentifier(album) {
		t.Fatalf("album identifier %q, %v", album, err)
	}
	if _, err := AlbumIdentifier("A@B"); !errors.Is(err, ErrArgumentRefused) {
		t.Fatal("album name with @ accepted")
	}
	allowed := []struct {
		op   Operation
		args []any
	}{
		{GetPlaylistSize, []any{AlbumsList}},
		{GetPlaylistSize, []any{album}},
		{GetPlaylistLists, []any{AlbumsList, 0, 49}},
		{GetPlaylistItems, []any{album, 10, 59}},
		{PlayUUID, []any{album, `<path>a:\x.flac<cue_id>-1<name>x`}},
	}
	for _, call := range allowed {
		if err := call.op.Validate(call.args); err != nil {
			t.Errorf("%s %v refused: %v", call.op.Name, call.args, err)
		}
	}
	refusedCalls := []struct {
		op   Operation
		args []any
	}{
		{GetPlaylistLists, []any{AllTracksList, 0, 9}},
		{GetPlaylistLists, []any{AlbumsList, 0, 60}},
		{GetPlaylistItems, []any{AlbumsList, 0, 9}},
		{PlayUUID, []any{"1#1@[03]Artist", `<path>a:\x.flac<cue_id>-1<name>x`}},
	}
	for _, call := range refusedCalls {
		if err := call.op.Validate(call.args); !errors.Is(err, ErrArgumentRefused) {
			t.Errorf("%s %v accepted", call.op.Name, call.args)
		}
	}
}
