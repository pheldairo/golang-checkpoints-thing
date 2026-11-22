package player

import (
	"fmt"
	"time"

	"setlist/internal/domain"
	"setlist/internal/hibylink/protocol"
	"setlist/internal/hibylink/rpc"
)

const (
	itemFieldTitle       = 0
	itemFieldArtist      = 1
	itemFieldLocator     = 2
	outcomeSelected      = "selected"
	outcomeAlready       = "already playing"
	alreadyPlayingSettle = 2 * time.Second
	outcomeWrong         = "wrong track"
)

type libraryItem struct {
	Title   string
	Artist  string
	Locator string
}

func (d device) library(offset, limit int) (domain.PlayerResult, error) {
	items, err := d.walkLibrary(offset + limit)
	if err != nil {
		return domain.PlayerResult{}, err
	}
	start := min(offset, len(items))
	page := toDomainLibrary(items[start:min(offset+limit, len(items))], start)
	return domain.PlayerResult{Action: domain.ActionLibrary, Outcome: domain.OutcomeRead, Library: page}, nil
}

func (d device) walkLibrary(limit int) ([]libraryItem, error) {
	walk := newLibraryWalk(limit)
	for !walk.finished {
		page, err := d.libraryPage(rpc.AllTracksList, walk.nextStart)
		if err != nil {
			return nil, err
		}
		if walk.emptyFirstPage(page) {
			return nil, d.explainEmptyLibrary()
		}
		walk.add(page)
	}
	return walk.items, nil
}

func (d device) explainEmptyLibrary() error {
	reply, err := d.caller.Call(rpc.GetPlaylistSize, rpc.AllTracksList)
	if err != nil {
		return err
	}
	if reply.Result() > 0 {
		return fmt.Errorf("library reports %d tracks but the first page is empty (sorted view or scan in progress)", reply.Result())
	}
	return nil
}

func (d device) libraryPage(list string, start int) ([]libraryItem, error) {
	reply, err := d.caller.Call(rpc.GetPlaylistItems, list, start, start+rpc.MaxLibraryRows-1)
	if err != nil {
		return nil, err
	}
	if reply.Signature == "i" {
		if reply.Result() < 0 {
			return nil, fmt.Errorf("getPlaylistItems returned error code %d at start %d", reply.Result(), start)
		}
		if reply.Result() > 0 {
			return nil, fmt.Errorf("%w: positive item count without items", protocol.ErrProtocol)
		}
		return nil, nil
	}
	rows, err := reply.Values.Rows(1)
	if err != nil {
		return nil, err
	}
	if reply.Result() != len(rows) {
		return nil, fmt.Errorf("%w: item count %d differs from %d rows", protocol.ErrProtocol, reply.Result(), len(rows))
	}
	return parseLibraryRows(rows)
}

func parseLibraryRows(rows []protocol.Values) ([]libraryItem, error) {
	items := make([]libraryItem, 0, len(rows))
	for _, row := range rows {
		fields := fieldReader{values: row}
		item := libraryItem{
			Title:   fields.text(itemFieldTitle),
			Artist:  fields.text(itemFieldArtist),
			Locator: fields.text(itemFieldLocator),
		}
		if fields.err != nil {
			return nil, fields.err
		}
		items = append(items, item)
	}
	return items, nil
}

func overlapLength(previous, current []libraryItem) int {
	for length := min(len(previous), len(current)); length > 0; length-- {
		if sameLocators(previous[len(previous)-length:], current[:length]) {
			return length
		}
	}
	return 0
}

func sameLocators(first, second []libraryItem) bool {
	for index := range first {
		if first[index].Locator != second[index].Locator {
			return false
		}
	}
	return true
}

func (d device) playTrack(input domain.PlayerInput) (domain.PlayerResult, error) {
	item, expectedPath, err := d.trackToPlay(input)
	if err != nil {
		return domain.PlayerResult{}, err
	}
	before, err := d.currentTrack()
	if err != nil {
		return domain.PlayerResult{}, err
	}
	change := &domain.TrackChange{Before: before.toDomain()}
	result := domain.PlayerResult{Action: domain.ActionPlayTrack, Track: change}
	sentAt := time.Now()
	list, err := playList(input.Album)
	if err != nil {
		return result, err
	}
	reply, err := d.caller.Call(rpc.PlayUUID, list, item.Locator)
	if err != nil {
		return result, err
	}
	alreadyPlaying := before != nil && before.Path == expectedPath
	after, progress, err := d.waitForPath(expectedPath, sentAt, alreadyPlaying)
	if err != nil {
		return result, err
	}
	change.After = after.toDomain()
	change.ProgressAfterMs = progress
	outcome := describeSelection(before, after, expectedPath, progress < resetBoundMs(sentAt))
	if reply.Result() != navigationQueued {
		return result, notConfirmed("playUuid replied %d", reply.Result())
	}
	if outcome != outcomeSelected && outcome != outcomeAlready {
		return result, notConfirmed("expected %q to play, observed %s within %s", item.Title, outcome, d.timing.ConfirmWait)
	}
	result.Outcome = outcome
	return result, nil
}

func (d device) libraryTrackToPlay(index int) (libraryItem, string, error) {
	items, err := d.walkLibrary(index + 1)
	if err != nil {
		return libraryItem{}, "", err
	}
	if index >= len(items) {
		return libraryItem{}, "", refuse("library has %d tracks, there is no track at position %d", len(items), index)
	}
	item := items[index]
	path, err := playablePath(item.Locator)
	if err != nil {
		return libraryItem{}, "", err
	}
	return item, path, nil
}

func (d device) waitForPath(expectedPath string, sentAt time.Time, alreadyPlaying bool) (*trackRecord, int, error) {
	deadline := sentAt.Add(d.timing.ConfirmWait)
	if alreadyPlaying {
		deadline = sentAt.Add(min(d.timing.ConfirmWait, alreadyPlayingSettle))
	}
	for {
		d.waitForPoll()
		after, err := d.currentTrack()
		if err != nil {
			return nil, 0, err
		}
		progress, err := d.progress()
		if err != nil {
			return nil, 0, err
		}
		onTarget := after != nil && after.Path == expectedPath
		if onTarget && progress < resetBoundMs(sentAt) {
			return after, progress, nil
		}
		if time.Now().After(deadline) {
			return after, progress, nil
		}
	}
}

func describeSelection(before, after *trackRecord, expectedPath string, progressReset bool) string {
	if after != nil && after.Path == expectedPath {
		if progressReset {
			return outcomeSelected
		}
		if before != nil && before.Path == expectedPath {
			return outcomeAlready
		}
		return outcomePending
	}
	if !sameFile(after, before) {
		return outcomeWrong
	}
	return outcomeUnchanged
}

func (d device) trackToPlay(input domain.PlayerInput) (libraryItem, string, error) {
	if input.Locator == "" {
		return d.libraryTrackToPlay(input.TrackIndex)
	}
	item := libraryItem{Title: fmt.Sprintf("library track %d", input.TrackIndex+1), Locator: input.Locator}
	path, err := playablePath(item.Locator)
	if err != nil {
		return libraryItem{}, "", err
	}
	return item, path, nil
}

func playablePath(raw string) (string, error) {
	locator, err := rpc.ParseLocator(raw)
	if err != nil {
		return "", refuse("the track locator is unreadable")
	}
	if locator.CueID != rpc.PlainFileCue {
		return "", refuse("the track is a cue sub-track, only plain files can be played")
	}
	path, err := rpc.MetadataPath(locator.Path)
	if err != nil {
		return "", refuse("the track has an unexpected path %q", locator.Path)
	}
	return path, nil
}

func playList(album string) (string, error) {
	if album == "" {
		return rpc.AllTracksList, nil
	}
	identifier, err := rpc.AlbumIdentifier(album)
	if err != nil {
		return "", refuse("album %q cannot be addressed on the player", album)
	}
	return identifier, nil
}
