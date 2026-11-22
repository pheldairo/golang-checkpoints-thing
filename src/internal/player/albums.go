package player

import (
	"fmt"

	"setlist/internal/domain"
	"setlist/internal/playerlink/protocol"
	"setlist/internal/playerlink/rpc"
)

const (
	albumFieldName       = 0
	albumFieldTrackCount = 3
)

func (d device) albumPage(start int) ([]domain.PlayerAlbum, error) {
	reply, err := d.caller.Call(rpc.GetPlaylistLists, rpc.AlbumsList, start, start+rpc.MaxLibraryRows-1)
	if err != nil {
		return nil, err
	}
	if reply.Signature == "i" {
		if reply.Result() < 0 {
			return nil, fmt.Errorf("getPlaylistLists returned error code %d at start %d", reply.Result(), start)
		}
		if reply.Result() > 0 {
			return nil, fmt.Errorf("%w: positive album count without albums", protocol.ErrProtocol)
		}
		return nil, nil
	}
	rows, err := reply.Values.Rows(1)
	if err != nil {
		return nil, err
	}
	if reply.Result() != len(rows) {
		return nil, fmt.Errorf("%w: album count %d differs from %d rows", protocol.ErrProtocol, reply.Result(), len(rows))
	}
	albums := make([]domain.PlayerAlbum, 0, len(rows))
	for offset, row := range rows {
		fields := fieldReader{values: row}
		album := domain.PlayerAlbum{
			Position:   start + offset,
			Name:       fields.text(albumFieldName),
			TrackCount: fields.number(albumFieldTrackCount),
		}
		if fields.err != nil {
			return nil, fields.err
		}
		albums = append(albums, album)
	}
	return albums, nil
}
