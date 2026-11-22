package player

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"setlist/internal/domain"
	"setlist/internal/playerlink/rpc"
)

func volumePlayer(volume *int, maximum int) *stubPlayer {
	return newStub(map[string]handler{
		"getMaxVolume": constant(maximum),
		"getVolume":    reading(volume),
		"setVolume": func(args []any) rpc.Reply {
			*volume = args[0].(int)
			return number(0)
		},
	})
}

func TestSetVolumeConfirmsByReadback(t *testing.T) {
	volume := 30
	stub := volumePlayer(&volume, 100)
	result, err := stub.device().setVolume(domain.PlayerInput{Action: domain.ActionSetVolume, Volume: -2, RelativeVolume: true})
	if err != nil || result.Outcome != domain.OutcomeConfirmed || result.Volume.After != 28 || volume != 28 {
		t.Fatalf("result %+v %+v, %v", result, result.Volume, err)
	}
}

func TestSetVolumeRefusesUnsafeTargetsWithoutSending(t *testing.T) {
	for _, input := range []domain.PlayerInput{
		{Action: domain.ActionSetVolume, Volume: 40},
		{Action: domain.ActionSetVolume, Volume: 120},
	} {
		volume := 30
		stub := volumePlayer(&volume, 100)
		if _, err := stub.device().setVolume(input); !errors.Is(err, ErrRefused) {
			t.Fatalf("volume %d accepted: %v", input.Volume, err)
		}
		if stub.wasSent("setVolume") {
			t.Fatalf("volume %d was sent to the player", input.Volume)
		}
	}
}

func TestRelativeVolumeIsClampedAndEqualVolumeSendsNothing(t *testing.T) {
	volume := 1
	stub := volumePlayer(&volume, 100)
	result, err := stub.device().setVolume(domain.PlayerInput{Action: domain.ActionSetVolume, Volume: -5, RelativeVolume: true})
	if err != nil || volume != 0 || result.Volume.Requested != 0 {
		t.Fatalf("clamped volume %d, %+v, %v", volume, result.Volume, err)
	}
	stub = volumePlayer(&volume, 100)
	result, err = stub.device().setVolume(domain.PlayerInput{Action: domain.ActionSetVolume, Volume: 0})
	if err != nil || result.Outcome != domain.OutcomeAlreadyApplied || stub.wasSent("setVolume") {
		t.Fatalf("equal volume %+v, %v", result, err)
	}
}

func TestSetVolumeNotConfirmedWhenReadbackDiffers(t *testing.T) {
	stub := newStub(map[string]handler{
		"getMaxVolume": constant(100),
		"getVolume":    constant(30),
		"setVolume":    constant(0),
	})
	if _, err := stub.device().setVolume(domain.PlayerInput{Action: domain.ActionSetVolume, Volume: 28}); !errors.Is(err, ErrNotConfirmed) {
		t.Fatalf("ignored volume change confirmed: %v", err)
	}
}

func TestPlayModeIsConfirmedByReadback(t *testing.T) {
	mode := domain.PlayModeInOrder
	stub := newStub(map[string]handler{
		"getPlayMode": reading(&mode),
		"setPlayMode": func(args []any) rpc.Reply {
			mode = args[0].(int)
			return number(0)
		},
	})
	result, err := stub.device().setPlayMode(domain.PlayModeShuffle)
	if err != nil || result.PlayMode.After != "shuffle" || result.PlayMode.Before != "in order" {
		t.Fatalf("result %+v, %v", result.PlayMode, err)
	}
	ignoring := newStub(map[string]handler{"getPlayMode": constant(1), "setPlayMode": constant(0)})
	if _, err := ignoring.device().setPlayMode(domain.PlayModeShuffle); !errors.Is(err, ErrNotConfirmed) {
		t.Fatalf("ignored mode change confirmed: %v", err)
	}
}

func playbackPlayer(state *int, progress handler) *stubPlayer {
	return newStub(map[string]handler{
		"getPlayState": reading(state),
		"getProgress":  progress,
		"setPlayState": func(args []any) rpc.Reply {
			if args[0].(int) == rpc.PlayStatePause {
				*state = domain.PlayStatePaused
			} else {
				*state = domain.PlayStatePlaying
			}
			return number(0)
		},
	})
}

func TestPauseNeedsFrozenProgress(t *testing.T) {
	state := domain.PlayStatePlaying
	stub := playbackPlayer(&state, constant(5000))
	result, err := stub.device().changePlayState(domain.ActionPause, pauseChange)
	if err != nil || result.Playback.StateAfter != "paused" {
		t.Fatalf("pause %+v, %v", result.Playback, err)
	}
	if _, err := stub.device().changePlayState(domain.ActionPause, pauseChange); !errors.Is(err, ErrRefused) {
		t.Fatalf("pausing a paused player was not refused: %v", err)
	}
}

func TestResumeNeedsAdvancingProgress(t *testing.T) {
	state := domain.PlayStatePaused
	stub := playbackPlayer(&state, advancing(1000, 400))
	result, err := stub.device().toggle()
	if err != nil || result.Action != domain.ActionToggle || result.Playback.StateAfter != "playing" {
		t.Fatalf("resume %+v, %v", result.Playback, err)
	}
	frozenState := domain.PlayStatePaused
	frozen := playbackPlayer(&frozenState, constant(1000))
	if _, err := frozen.device().changePlayState(domain.ActionResume, resumeChange); !errors.Is(err, ErrNotConfirmed) {
		t.Fatalf("resume without moving progress confirmed: %v", err)
	}
}

func TestToggleRefusesWhenStopped(t *testing.T) {
	stub := newStub(map[string]handler{"getPlayState": constant(3)})
	if _, err := stub.device().toggle(); !errors.Is(err, ErrRefused) || stub.wasSent("setPlayState") {
		t.Fatalf("toggle while stopped: %v", err)
	}
}

func seekPlayer(path *string, format int, position *int) *stubPlayer {
	return newStub(map[string]handler{
		"getPlayState": constant(domain.PlayStatePlaying),
		"getMetaInfo":  metadata(path, 240000, format),
		"getProgress": func([]any) rpc.Reply {
			*position += 300
			return number(*position)
		},
		"setProgress": func(args []any) rpc.Reply {
			*position = args[0].(int)
			return number(0)
		},
	})
}

func TestSeekConfirmsLandingAndContinuation(t *testing.T) {
	path, position := `\a\1.flac`, 5000
	stub := seekPlayer(&path, formatFLAC, &position)
	result, err := stub.device().seek(60000)
	if err != nil || result.Outcome != domain.OutcomeConfirmed || len(result.Playback.ProgressSamplesMs) < 2 {
		t.Fatalf("seek %+v, %v", result.Playback, err)
	}
}

func TestSeekRefusesUnreviewedFormatsAndTrackEnd(t *testing.T) {
	path, position := `\a\1.wav`, 0
	if _, err := seekPlayer(&path, 0x1234, &position).device().seek(1000); !errors.Is(err, ErrRefused) {
		t.Fatalf("unknown format seek: %v", err)
	}
	stub := seekPlayer(&path, formatFLAC, &position)
	if _, err := stub.device().seek(239000); !errors.Is(err, ErrRefused) || stub.wasSent("setProgress") {
		t.Fatalf("seek past the end guard: %v", err)
	}
}

func navigationPlayer(path *string, next string) *stubPlayer {
	return newStub(map[string]handler{
		"getMetaInfo":  metadata(path, 240000, formatFLAC),
		"getProgress":  constant(200),
		"getPlayState": constant(domain.PlayStatePlaying),
		"playNext": func([]any) rpc.Reply {
			*path = next
			return number(1)
		},
		"setProgress": constant(0),
	})
}

func TestNextConfirmsADifferentFile(t *testing.T) {
	path := `\a\1.flac`
	result, err := navigationPlayer(&path, `\a\2.flac`).device().next()
	if err != nil || result.Outcome != "changed" || result.Track.After.Path != `\a\2.flac` {
		t.Fatalf("next %+v, %v", result.Track, err)
	}
	stuck := `\a\1.flac`
	if _, err := navigationPlayer(&stuck, stuck).device().next(); !errors.Is(err, ErrNotConfirmed) {
		t.Fatalf("next without a track change confirmed: %v", err)
	}
}

func TestRestartKeepsTheSameFile(t *testing.T) {
	path := `\a\1.flac`
	result, err := navigationPlayer(&path, path).device().restart()
	if err != nil || result.Outcome != "restarted" {
		t.Fatalf("restart %+v, %v", result, err)
	}
}

func TestLibraryWalkRemovesPageOverlap(t *testing.T) {
	ids := append(rowids(1, 50), rowids(48, 70)...)
	pages := 0
	stub := newStub(map[string]handler{
		"getPlaylistItems": func(args []any) rpc.Reply {
			pages++
			if pages == 1 {
				return libraryOf(ids[:50])(args)
			}
			return libraryOf(ids[50:])([]any{args[0], 0, 49})
		},
	})
	items, err := stub.device().walkLibrary(500)
	if err != nil || len(items) != 70 {
		t.Fatalf("walked %d items, %v", len(items), err)
	}
	if items[49].Title != "T50" || items[50].Title != "T51" {
		t.Fatalf("overlap not removed: %q then %q", items[49].Title, items[50].Title)
	}
}

func TestLibraryPageUsesOffsetAndLimit(t *testing.T) {
	stub := newStub(map[string]handler{"getPlaylistItems": libraryOf(rowids(1, 120))})
	result, err := stub.device().library(10, 5)
	if err != nil || len(result.Library) != 5 || result.Library[0].Position != 10 || result.Library[0].Title != "T11" {
		t.Fatalf("library %+v, %v", result.Library, err)
	}
}

func TestPlayTrackConfirmsTheRequestedFile(t *testing.T) {
	path := `\a\9.flac`
	stub := newStub(map[string]handler{
		"getPlaylistItems": libraryOf(rowids(1, 20)),
		"getMetaInfo":      metadata(&path, 240000, formatFLAC),
		"getProgress":      constant(100),
		"playUuid": func(args []any) rpc.Reply {
			locator, _ := rpc.ParseLocator(args[1].(string))
			path, _ = rpc.MetadataPath(locator.Path)
			return number(1)
		},
	})
	result, err := stub.device().playTrack(domain.PlayerInput{Action: domain.ActionPlayTrack, TrackIndex: 4})
	if err != nil || result.Outcome != "selected" || result.Track.After.Path != `\a\5.flac` {
		t.Fatalf("play track %+v, %v", result.Track, err)
	}
	if _, err := stub.device().playTrack(domain.PlayerInput{Action: domain.ActionPlayTrack, TrackIndex: 40}); !errors.Is(err, ErrRefused) {
		t.Fatalf("track beyond the library accepted: %v", err)
	}
}

func TestControllerSerializesControlsAndReopensLostSessions(t *testing.T) {
	volume := 60
	var stubLock sync.Mutex
	opened := 0
	var current *stubPlayer
	controller := NewControllerWithOpener(func() (Connection, error) {
		stubLock.Lock()
		defer stubLock.Unlock()
		opened++
		current = volumePlayer(&volume, 100)
		return current, nil
	}, testTiming)
	defer controller.Close()
	var controls sync.WaitGroup
	errs := make(chan error, 10)
	for range 10 {
		controls.Add(1)
		go func() {
			defer controls.Done()
			_, err := controller.Control(domain.PlayerInput{Action: domain.ActionSetVolume, Volume: -1, RelativeVolume: true})
			errs <- err
		}()
	}
	controls.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("interleaved control was not confirmed: %v", err)
		}
	}
	if volume != 50 {
		t.Fatalf("volume is %d after ten serialized steps, want 50", volume)
	}
	current.lock.Lock()
	current.usable = false
	current.lock.Unlock()
	if _, err := controller.Control(domain.PlayerInput{Action: domain.ActionSetVolume, Volume: 49}); err != nil {
		t.Fatal(err)
	}
	if opened != 2 {
		t.Fatalf("controller opened %d sessions, want a reopen after the loss", opened)
	}
}

func TestClosedControllerRefusesControls(t *testing.T) {
	controller := NewControllerWithOpener(func() (Connection, error) {
		return newStub(nil), nil
	}, testTiming)
	controller.Close()
	if _, err := controller.Control(domain.PlayerInput{Action: domain.ActionStatus}); !errors.Is(err, ErrControllerClosed) {
		t.Fatalf("closed controller accepted a control: %v", err)
	}
	if _, err := controller.Control(domain.PlayerInput{Action: "dance"}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("invalid input reached the player: %v", err)
	}
}

func TestCoverReturnsImageBytesOrNothing(t *testing.T) {
	art := []byte("\xff\xd8image\xff\xd9")
	withCover := newStub(map[string]handler{"getCover": func([]any) rpc.Reply {
		return rpc.Reply{Signature: rpc.CoverSignature, Values: []any{int32(len(art)), art}}
	}})
	controller := NewControllerWithOpener(func() (Connection, error) { return withCover, nil }, testTiming)
	defer controller.Close()
	image, err := controller.Cover()
	if err != nil || string(image) != string(art) {
		t.Fatalf("cover %q, %v", image, err)
	}
	notReady := newStub(map[string]handler{"getCover": constant(-1)})
	if image, err := notReady.device().cover(); err != nil || image != nil {
		t.Fatalf("missing cover gave %q, %v", image, err)
	}
	mismatched := newStub(map[string]handler{"getCover": func([]any) rpc.Reply {
		return rpc.Reply{Signature: rpc.CoverSignature, Values: []any{int32(99), art}}
	}})
	if _, err := mismatched.device().cover(); err == nil {
		t.Fatal("cover with a wrong length accepted")
	}
}

func TestPlayTrackByLocatorSkipsTheLibraryWalk(t *testing.T) {
	path := `\a\9.flac`
	stub := newStub(map[string]handler{
		"getMetaInfo": metadata(&path, 240000, formatFLAC),
		"getProgress": constant(100),
		"playUuid": func(args []any) rpc.Reply {
			path = `\a\77.flac`
			return number(1)
		},
	})
	input := domain.PlayerInput{Action: domain.ActionPlayTrack, TrackIndex: 76, Locator: `<path>a:\77.flac<cue_id>-1<name>T77`}
	result, err := stub.device().playTrack(input)
	if err != nil || result.Track.After.Path != `\a\77.flac` || stub.wasSent("getPlaylistItems") {
		t.Fatalf("play by locator %+v, %v", result.Track, err)
	}
	input.Locator = `<path>a:\x.cue<cue_id>3<name>x`
	if _, err := stub.device().playTrack(input); !errors.Is(err, ErrRefused) {
		t.Fatalf("cue locator accepted: %v", err)
	}
}

func TestWalkLibraryReportsProgressAndKeepsLocators(t *testing.T) {
	stub := newStub(map[string]handler{
		"getPlaylistSize":  constant(120),
		"getPlaylistItems": libraryOf(rowids(1, 120)),
	})
	controller := NewControllerWithOpener(func() (Connection, error) { return stub, nil }, testTiming)
	defer controller.Close()
	var reports [][2]int
	tracks, err := controller.WalkLibrary(func(loaded, total int) { reports = append(reports, [2]int{loaded, total}) })
	if err != nil || len(tracks) != 120 {
		t.Fatalf("walked %d tracks, %v", len(tracks), err)
	}
	if tracks[119].Position != 119 || tracks[119].Locator != `<path>a:\120.flac<cue_id>-1<name>T120` {
		t.Fatalf("last track %+v", tracks[119])
	}
	want := [][2]int{{50, 120}, {100, 120}, {120, 120}}
	if len(reports) != len(want) || reports[0] != want[0] || reports[2] != want[2] {
		t.Fatalf("progress reports %v, want %v", reports, want)
	}
}

func albumRows(count int) handler {
	return func(args []any) rpc.Reply {
		start, end := args[1].(int), args[2].(int)
		var rows [][]any
		for position := start; position <= end && position < count; position++ {
			name := fmt.Sprintf("Album %03d", position)
			rows = append(rows, []any{[]byte(name), []byte("<path>" + name + "<cue_id>0<name>" + name), uint32(position), uint32(position%12 + 1)})
		}
		if len(rows) == 0 {
			return number(0)
		}
		return rpc.Reply{Signature: rpc.AlbumRowsSignature, Values: []any{int32(len(rows)), rows}}
	}
}

func TestWalkAlbumsUsesOffsetPaging(t *testing.T) {
	stub := newStub(map[string]handler{"getPlaylistSize": constant(73), "getPlaylistLists": albumRows(73)})
	controller := NewControllerWithOpener(func() (Connection, error) { return stub, nil }, testTiming)
	defer controller.Close()
	var reports []int
	albums, err := controller.WalkAlbums(func(loaded, total int) { reports = append(reports, loaded) })
	if err != nil || len(albums) != 73 || albums[72].Name != "Album 072" || albums[72].TrackCount != 1 {
		t.Fatalf("albums %d, last %+v, %v", len(albums), albums[len(albums)-1], err)
	}
	if len(reports) != 2 || reports[1] != 73 {
		t.Fatalf("progress %v", reports)
	}
}

func TestAlbumTracksAndAlbumContextPlay(t *testing.T) {
	var lists []string
	path := `\a\1.flac`
	stub := newStub(map[string]handler{
		"getPlaylistItems": func(args []any) rpc.Reply {
			lists = append(lists, args[0].(string))
			return libraryOf(rowids(101, 110))([]any{args[0], 100 + args[1].(int), 100 + args[2].(int)})
		},
		"getMetaInfo": metadata(&path, 240000, formatFLAC),
		"getProgress": constant(100),
		"playUuid": func(args []any) rpc.Reply {
			lists = append(lists, args[0].(string))
			path = `\a\105.flac`
			return number(1)
		},
	})
	controller := NewControllerWithOpener(func() (Connection, error) { return stub, nil }, testTiming)
	defer controller.Close()
	tracks, err := controller.AlbumTracks("Abbey Road")
	if err != nil || len(tracks) != 10 || tracks[4].Title != "T105" {
		t.Fatalf("album tracks %+v, %v", tracks, err)
	}
	input := domain.PlayerInput{Action: domain.ActionPlayTrack, TrackIndex: 4, Locator: tracks[4].Locator, Album: "Abbey Road"}
	if _, err := controller.Control(input); err != nil {
		t.Fatal(err)
	}
	for _, list := range lists {
		if list != "1#1@[05]Abbey Road" {
			t.Fatalf("used list %q, want the album", list)
		}
	}
	if _, err := controller.AlbumTracks("bad@name"); !errors.Is(err, ErrRefused) {
		t.Fatalf("album with @ accepted: %v", err)
	}
}

func TestReplayingTheCurrentTrackCountsAsAlreadyPlaying(t *testing.T) {
	path := `\a\5.flac`
	stub := newStub(map[string]handler{
		"getMetaInfo": metadata(&path, 240000, formatFLAC),
		"getProgress": constant(90000),
		"playUuid":    constant(1),
	})
	input := domain.PlayerInput{Action: domain.ActionPlayTrack, TrackIndex: 4, Locator: `<path>a:\5.flac<cue_id>-1<name>T5`}
	result, err := stub.device().playTrack(input)
	if err != nil || result.Outcome != "already playing" {
		t.Fatalf("replaying the current track: %q, %v", result.Outcome, err)
	}
}
