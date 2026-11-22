package player

import (
	"fmt"

	"setlist/internal/domain"
	"setlist/internal/playerlink/protocol"
	"setlist/internal/playerlink/rpc"
)

const (
	formatFLAC         = 0xf1ac
	formatMPEG         = 0x55
	flacEndGuardMs     = 2000
	mpegEndGuardMs     = 5000
	mpegEndGuardShare  = 20
	metadataFieldCount = 15
)

const (
	fieldTitle    = 0
	fieldArtist   = 1
	fieldAlbum    = 2
	fieldPath     = 5
	fieldDuration = 7
	fieldFormat   = 11
)

type trackRecord struct {
	Title        string
	Artist       string
	Album        string
	Path         string
	DurationMs   int
	FormatMarker int
}

func (d device) currentTrack() (*trackRecord, error) {
	reply, err := d.caller.Call(rpc.GetMetaInfo)
	if err != nil {
		return nil, err
	}
	if reply.Result() < 0 {
		return nil, fmt.Errorf("getMetaInfo returned error code %d", reply.Result())
	}
	rows, err := reply.Values.Rows(1)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return parseTrackRecord(rows[0])
}

func parseTrackRecord(values protocol.Values) (*trackRecord, error) {
	if len(values) != metadataFieldCount {
		return nil, fmt.Errorf("metadata record has %d fields, expected %d", len(values), metadataFieldCount)
	}
	fields := fieldReader{values: values}
	record := &trackRecord{
		Title:        fields.text(fieldTitle),
		Artist:       fields.text(fieldArtist),
		Album:        fields.text(fieldAlbum),
		Path:         fields.text(fieldPath),
		DurationMs:   fields.number(fieldDuration),
		FormatMarker: fields.number(fieldFormat),
	}
	if fields.err != nil {
		return nil, fields.err
	}
	return record, nil
}

func (r *trackRecord) toDomain() *domain.PlayerTrack {
	if r == nil {
		return nil
	}
	return &domain.PlayerTrack{
		Title:      r.Title,
		Artist:     r.Artist,
		Album:      r.Album,
		Path:       r.Path,
		DurationMs: r.DurationMs,
		Format:     formatName(r.FormatMarker),
	}
}

func formatName(marker int) string {
	switch marker {
	case formatFLAC:
		return "FLAC"
	case formatMPEG:
		return "MPEG audio"
	}
	return fmt.Sprintf("unknown (%#x)", marker)
}

func (r *trackRecord) seekLimitMs() (int, bool) {
	switch r.FormatMarker {
	case formatFLAC:
		return r.DurationMs - flacEndGuardMs, true
	case formatMPEG:
		return r.DurationMs - max(mpegEndGuardMs, r.DurationMs/mpegEndGuardShare), true
	}
	return 0, false
}

func sameFile(first, second *trackRecord) bool {
	if first == nil || second == nil {
		return first == nil && second == nil
	}
	return first.Path == second.Path
}
