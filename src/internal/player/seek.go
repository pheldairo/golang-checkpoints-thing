package player

import (
	"time"

	"setlist/internal/domain"
	"setlist/internal/playerlink/rpc"
)

const (
	seekSlackMs          = 2500
	seekContinuationMs   = 1000
	outcomeTrackChanged  = "track changed"
	outcomeNotContinuing = "not continuing"
	outcomeNotObserved   = "not observed"
)

type seekAttempt struct {
	track    *trackRecord
	targetMs int
	paused   bool
	sentAt   time.Time
	playback *domain.PlaybackChange
}

func (a seekAttempt) slackMs() int {
	return seekSlackMs + millisecondsSince(a.sentAt)
}

func (a seekAttempt) landed(positionMs int) bool {
	return positionMs >= a.targetMs && positionMs <= a.targetMs+a.slackMs()
}

func (d device) seek(targetMs int) (domain.PlayerResult, error) {
	playback := &domain.PlaybackChange{TargetMs: targetMs}
	result := domain.PlayerResult{Action: domain.ActionSeek, Playback: playback}
	attempt, err := d.prepareSeek(targetMs, playback)
	if err != nil {
		return result, err
	}
	attempt.sentAt = time.Now()
	reply, err := d.caller.Call(rpc.SetProgress, targetMs)
	if err != nil {
		return result, err
	}
	outcome, err := d.waitForSeek(attempt)
	if err != nil {
		return result, err
	}
	state, err := d.playState()
	if err != nil {
		return result, err
	}
	playback.StateAfter = domain.PlayStateName(state)
	if reply.Result() != commandAccepted {
		return result, notConfirmed("setProgress replied %d", reply.Result())
	}
	if outcome != domain.OutcomeConfirmed {
		return result, notConfirmed("seek to %d ms %s", targetMs, outcome)
	}
	result.Outcome = outcome
	return result, nil
}

func (d device) prepareSeek(targetMs int, playback *domain.PlaybackChange) (seekAttempt, error) {
	state, err := d.playState()
	if err != nil {
		return seekAttempt{}, err
	}
	playback.StateBefore = domain.PlayStateName(state)
	if state != domain.PlayStatePlaying && state != domain.PlayStatePaused {
		return seekAttempt{}, refuse("seek needs the player to be playing or paused, it is %s", playback.StateBefore)
	}
	track, err := d.currentTrack()
	if err != nil {
		return seekAttempt{}, err
	}
	if track == nil {
		return seekAttempt{}, refuse("seek needs current track metadata, the player reported none")
	}
	limit, known := track.seekLimitMs()
	if !known {
		return seekAttempt{}, refuse("seek is only reviewed for FLAC and MPEG audio, current track is %s", formatName(track.FormatMarker))
	}
	if targetMs > limit {
		return seekAttempt{}, refuse("seek target %d ms is past the safe end %d ms of this track", targetMs, limit)
	}
	return seekAttempt{track: track, targetMs: targetMs, paused: state == domain.PlayStatePaused, playback: playback}, nil
}

func (d device) waitForSeek(attempt seekAttempt) (string, error) {
	started := time.Now()
	for {
		position, err := d.progress()
		if err != nil {
			return "", err
		}
		attempt.playback.ProgressSamplesMs = append(attempt.playback.ProgressSamplesMs, position)
		current, err := d.currentTrack()
		if err != nil {
			return "", err
		}
		stillSameTrack := sameFile(current, attempt.track)
		if stillSameTrack && attempt.landed(position) {
			continuing, err := d.seekContinues(attempt, position)
			if err != nil {
				return "", err
			}
			if continuing {
				return domain.OutcomeConfirmed, nil
			}
		}
		if time.Since(started) >= d.timing.ConfirmWait {
			return seekFailureOutcome(stillSameTrack, attempt.landed(position)), nil
		}
		d.waitForPoll()
	}
}

func seekFailureOutcome(stillSameTrack, landed bool) string {
	if !stillSameTrack {
		return outcomeTrackChanged
	}
	if landed {
		return outcomeNotContinuing
	}
	return outcomeNotObserved
}

func (d device) seekContinues(attempt seekAttempt, positionMs int) (bool, error) {
	d.waitForProbe()
	later, err := d.progress()
	if err != nil {
		return false, err
	}
	attempt.playback.ProgressSamplesMs = append(attempt.playback.ProgressSamplesMs, later)
	if !attempt.paused {
		return later > positionMs && later <= attempt.targetMs+attempt.slackMs()+seekContinuationMs, nil
	}
	state, err := d.playState()
	if err != nil {
		return false, err
	}
	frozen := later-positionMs <= frozenProgressMs && positionMs-later <= frozenProgressMs
	return frozen && state == domain.PlayStatePaused, nil
}
