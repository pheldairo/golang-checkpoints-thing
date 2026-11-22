package domain

import "fmt"

type PlayerAction string

const (
	ActionStatus      PlayerAction = "status"
	ActionSetVolume   PlayerAction = "set_volume"
	ActionPause       PlayerAction = "pause"
	ActionResume      PlayerAction = "resume"
	ActionToggle      PlayerAction = "toggle"
	ActionNext        PlayerAction = "next"
	ActionPrevious    PlayerAction = "previous"
	ActionRestart     PlayerAction = "restart"
	ActionSeek        PlayerAction = "seek"
	ActionSetPlayMode PlayerAction = "set_play_mode"
	ActionLibrary     PlayerAction = "library"
	ActionPlayTrack   PlayerAction = "play_track"
)

const (
	MaxPlayerVolume       = 255
	MaxSeekPositionMs     = 24 * 60 * 60 * 1000
	MaxLibraryPageSize    = 50
	MaxLibraryWalkTracks  = 1000
	PlayModeInOrder       = 1
	PlayModeRepeatAll     = 2
	PlayModeRepeatOne     = 3
	PlayModeShuffle       = 4
	PlayStatePlaying      = 1
	PlayStatePaused       = 2
	OutcomeConfirmed      = "confirmed"
	OutcomeAlreadyApplied = "already applied"
	OutcomeRead           = "read"
)

var playModeNames = map[int]string{
	PlayModeInOrder:   "in order",
	PlayModeRepeatAll: "repeat all",
	PlayModeRepeatOne: "repeat one",
	PlayModeShuffle:   "shuffle",
}

var playStateNames = map[int]string{
	PlayStatePlaying: "playing",
	PlayStatePaused:  "paused",
}

func PlayModeName(mode int) string {
	if name, ok := playModeNames[mode]; ok {
		return name
	}
	return fmt.Sprintf("unknown (%d)", mode)
}

func PlayModeFromName(name string) (int, bool) {
	for mode, modeName := range playModeNames {
		if modeName == name {
			return mode, true
		}
	}
	return 0, false
}

func NextPlayMode(mode int) int {
	return mode%PlayModeShuffle + 1
}

func PlayStateName(state int) string {
	if name, ok := playStateNames[state]; ok {
		return name
	}
	return fmt.Sprintf("other (%d)", state)
}

type PlayerInput struct {
	Action         PlayerAction
	Volume         int    `json:",omitempty"`
	RelativeVolume bool   `json:",omitempty"`
	PositionMs     int    `json:",omitempty"`
	PlayMode       int    `json:",omitempty"`
	TrackIndex     int    `json:",omitempty"`
	Offset         int    `json:",omitempty"`
	Limit          int    `json:",omitempty"`
	Locator        string `json:",omitempty"`
	Album          string `json:",omitempty"`
}

func (in PlayerInput) Validate() error {
	switch in.Action {
	case ActionStatus, ActionPause, ActionResume, ActionToggle, ActionNext, ActionPrevious, ActionRestart:
		return nil
	case ActionSetVolume:
		return in.validateVolume()
	case ActionSeek:
		if in.PositionMs < 0 || in.PositionMs > MaxSeekPositionMs {
			return fmt.Errorf("%w: seek position must be between 0 and %d ms", ErrInvalidInput, MaxSeekPositionMs)
		}
		return nil
	case ActionSetPlayMode:
		if _, ok := playModeNames[in.PlayMode]; !ok {
			return fmt.Errorf("%w: play mode must be 1 (in order), 2 (repeat all), 3 (repeat one) or 4 (shuffle)", ErrInvalidInput)
		}
		return nil
	case ActionLibrary:
		return in.validateLibraryPage()
	case ActionPlayTrack:
		return in.validatePlayTrack()
	default:
		return fmt.Errorf("%w: unsupported player action %q", ErrInvalidInput, in.Action)
	}
}

func (in PlayerInput) validateVolume() error {
	if in.RelativeVolume {
		if in.Volume == 0 || in.Volume < -MaxPlayerVolume || in.Volume > MaxPlayerVolume {
			return fmt.Errorf("%w: relative volume step must be non-zero and within ±%d", ErrInvalidInput, MaxPlayerVolume)
		}
		return nil
	}
	if in.Volume < 0 || in.Volume > MaxPlayerVolume {
		return fmt.Errorf("%w: volume must be between 0 and %d", ErrInvalidInput, MaxPlayerVolume)
	}
	return nil
}

func (in PlayerInput) validatePlayTrack() error {
	if in.Locator != "" {
		if in.TrackIndex < 0 {
			return fmt.Errorf("%w: track index must not be negative", ErrInvalidInput)
		}
		return nil
	}
	if in.Album != "" {
		return fmt.Errorf("%w: playing inside an album needs the track locator", ErrInvalidInput)
	}
	if in.TrackIndex < 0 || in.TrackIndex >= MaxLibraryWalkTracks {
		return fmt.Errorf("%w: track index must be between 0 and %d when no locator is given", ErrInvalidInput, MaxLibraryWalkTracks-1)
	}
	return nil
}

func (in PlayerInput) validateLibraryPage() error {
	if in.Offset < 0 || in.Limit < 1 || in.Limit > MaxLibraryPageSize {
		return fmt.Errorf("%w: library page needs offset >= 0 and limit 1..%d", ErrInvalidInput, MaxLibraryPageSize)
	}
	if in.Offset+in.Limit > MaxLibraryWalkTracks {
		return fmt.Errorf("%w: library page must end within the first %d tracks", ErrInvalidInput, MaxLibraryWalkTracks)
	}
	return nil
}

type PlayerTrack struct {
	Title      string
	Artist     string
	Album      string
	Path       string
	DurationMs int
	Format     string
}

type PlayerStatus struct {
	Model      string
	Volume     int
	MaxVolume  int
	PlayState  string
	PlayMode   string
	ProgressMs int
	NowPlaying *PlayerTrack `json:",omitempty"`
}

type VolumeChange struct {
	Requested int
	Before    int
	After     int
	Maximum   int
}

type PlaybackChange struct {
	StateBefore       string
	StateAfter        string
	TargetMs          int   `json:",omitempty"`
	ProgressSamplesMs []int `json:",omitempty"`
}

type TrackChange struct {
	Before          *PlayerTrack `json:",omitempty"`
	After           *PlayerTrack `json:",omitempty"`
	ProgressAfterMs int
}

type PlayModeChange struct {
	Requested string
	Before    string
	After     string
}

type PlayerLibraryItem struct {
	Position int
	Title    string
	Artist   string
	Locator  string `json:",omitempty"`
}

type PlayerAlbum struct {
	Position   int
	Name       string
	TrackCount int
	Tracks     []PlayerLibraryItem `json:",omitempty"`
}

type PlayerResult struct {
	Action   PlayerAction
	Outcome  string
	Status   *PlayerStatus       `json:",omitempty"`
	Volume   *VolumeChange       `json:",omitempty"`
	Playback *PlaybackChange     `json:",omitempty"`
	Track    *TrackChange        `json:",omitempty"`
	PlayMode *PlayModeChange     `json:",omitempty"`
	Library  []PlayerLibraryItem `json:",omitempty"`
}

func cloneTrack(track *PlayerTrack) *PlayerTrack {
	if track == nil {
		return nil
	}
	copied := *track
	return &copied
}

func (r PlayerResult) Clone() PlayerResult {
	if r.Status != nil {
		status := *r.Status
		status.NowPlaying = cloneTrack(status.NowPlaying)
		r.Status = &status
	}
	if r.Volume != nil {
		volume := *r.Volume
		r.Volume = &volume
	}
	if r.Playback != nil {
		playback := *r.Playback
		playback.ProgressSamplesMs = append([]int(nil), playback.ProgressSamplesMs...)
		r.Playback = &playback
	}
	if r.Track != nil {
		track := *r.Track
		track.Before = cloneTrack(track.Before)
		track.After = cloneTrack(track.After)
		r.Track = &track
	}
	if r.PlayMode != nil {
		mode := *r.PlayMode
		r.PlayMode = &mode
	}
	r.Library = append([]PlayerLibraryItem(nil), r.Library...)
	return r
}
