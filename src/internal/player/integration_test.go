package player

import (
	"os"
	"testing"

	"setlist/internal/domain"
	"setlist/internal/hibylink/session"
)

const minimumSeekTrackMs = 40000

type integrationRun struct {
	t          *testing.T
	controller *Controller
}

func (r integrationRun) control(input domain.PlayerInput) domain.PlayerResult {
	r.t.Helper()
	result, err := r.controller.Control(input)
	if err != nil {
		r.t.Fatalf("%s: %v", input.Action, err)
	}
	r.t.Logf("%s: %s", input.Action, result.Outcome)
	return result
}

func (r integrationRun) status() *domain.PlayerStatus {
	r.t.Helper()
	return r.control(domain.PlayerInput{Action: domain.ActionStatus}).Status
}

func TestIntegrationAgainstPlayer(t *testing.T) {
	host := os.Getenv("HIBYLINK_TARGET")
	if host == "" {
		t.Skip("set HIBYLINK_TARGET to a player or fake player host to run")
	}
	run := integrationRun{t: t, controller: NewController(session.DefaultConfig(host))}
	defer run.controller.Close()

	baseline := run.status()
	t.Logf("player %s: volume %d/%d, %s, %s, now playing %+v",
		baseline.Model, baseline.Volume, baseline.MaxVolume, baseline.PlayState, baseline.PlayMode, baseline.NowPlaying)

	step := -1
	if baseline.Volume == 0 {
		step = 1
	}
	run.control(domain.PlayerInput{Action: domain.ActionSetVolume, Volume: step, RelativeVolume: true})
	run.control(domain.PlayerInput{Action: domain.ActionSetVolume, Volume: baseline.Volume})

	originalMode, known := domain.PlayModeFromName(baseline.PlayMode)
	if !known {
		originalMode = domain.PlayModeInOrder
	}
	otherMode := domain.PlayModeRepeatAll
	if originalMode == domain.PlayModeRepeatAll {
		otherMode = domain.PlayModeInOrder
	}
	run.control(domain.PlayerInput{Action: domain.ActionSetPlayMode, PlayMode: otherMode})
	run.control(domain.PlayerInput{Action: domain.ActionSetPlayMode, PlayMode: originalMode})

	if baseline.PlayState == "playing" || baseline.PlayState == "paused" {
		run.control(domain.PlayerInput{Action: domain.ActionToggle})
		run.control(domain.PlayerInput{Action: domain.ActionToggle})
	}

	track := baseline.NowPlaying
	if track != nil && track.DurationMs > minimumSeekTrackMs && (track.Format == "FLAC" || track.Format == "MPEG audio") {
		run.control(domain.PlayerInput{Action: domain.ActionSeek, PositionMs: 30000})
		run.control(domain.PlayerInput{Action: domain.ActionRestart})
	}

	run.control(domain.PlayerInput{Action: domain.ActionNext})
	run.control(domain.PlayerInput{Action: domain.ActionPrevious})

	page := run.control(domain.PlayerInput{Action: domain.ActionLibrary, Limit: 3})
	if len(page.Library) > 0 {
		run.control(domain.PlayerInput{Action: domain.ActionPlayTrack, TrackIndex: 0})
	}

	final := run.status()
	if final.Volume != baseline.Volume || final.PlayMode != baseline.PlayMode || final.PlayState != baseline.PlayState {
		t.Fatalf("player state not restored: before %+v, after %+v", baseline, final)
	}
}
