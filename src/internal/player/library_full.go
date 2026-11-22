package player

import (
	"setlist/internal/domain"
	"setlist/internal/playerlink/rpc"
)

const maxFullLibraryTracks = 200_000

func (c *Controller) LibrarySize() (int, error) {
	return c.listSize(rpc.AllTracksList)
}

func (c *Controller) listSize(list string) (int, error) {
	var size int
	err := c.withDevice(func(d device) error {
		reply, err := d.caller.Call(rpc.GetPlaylistSize, list)
		size = max(0, reply.Result())
		return err
	})
	return size, err
}

func (c *Controller) WalkLibrary(progress func(loaded, total int)) ([]domain.PlayerLibraryItem, error) {
	total, err := c.LibrarySize()
	if err != nil {
		return nil, err
	}
	walk := newLibraryWalk(maxFullLibraryTracks)
	for !walk.finished {
		page, err := c.libraryPage(rpc.AllTracksList, walk.nextStart)
		if err != nil {
			return nil, err
		}
		if walk.emptyFirstPage(page) && total > 0 {
			return nil, c.withDevice(func(d device) error { return d.explainEmptyLibrary() })
		}
		walk.add(page)
		if progress != nil {
			progress(len(walk.items), max(total, len(walk.items)))
		}
	}
	return toDomainLibrary(walk.items, 0), nil
}

func (c *Controller) WalkAlbums(progress func(loaded, total int)) ([]domain.PlayerAlbum, error) {
	total, err := c.listSize(rpc.AlbumsList)
	if err != nil {
		return nil, err
	}
	var albums []domain.PlayerAlbum
	for start := 0; ; start += rpc.MaxLibraryRows {
		var page []domain.PlayerAlbum
		err := c.withDevice(func(d device) error {
			var err error
			page, err = d.albumPage(start)
			return err
		})
		if err != nil {
			return nil, err
		}
		albums = append(albums, page...)
		if progress != nil {
			progress(len(albums), max(total, len(albums)))
		}
		if len(page) < rpc.MaxLibraryRows {
			return albums, nil
		}
	}
}

func (c *Controller) AlbumTracks(album string) ([]domain.PlayerLibraryItem, error) {
	list, err := playList(album)
	if err != nil {
		return nil, err
	}
	var tracks []libraryItem
	for start := 0; ; start += rpc.MaxLibraryRows {
		page, err := c.libraryPage(list, start)
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, page...)
		if len(page) < rpc.MaxLibraryRows {
			return toDomainLibrary(tracks, 0), nil
		}
	}
}

func (c *Controller) libraryPage(list string, start int) ([]libraryItem, error) {
	var page []libraryItem
	err := c.withDevice(func(d device) error {
		var err error
		page, err = d.libraryPage(list, start)
		return err
	})
	return page, err
}

func toDomainLibrary(items []libraryItem, firstPosition int) []domain.PlayerLibraryItem {
	converted := make([]domain.PlayerLibraryItem, 0, len(items))
	for offset, item := range items {
		converted = append(converted, domain.PlayerLibraryItem{
			Position: firstPosition + offset,
			Title:    item.Title,
			Artist:   item.Artist,
			Locator:  item.Locator,
		})
	}
	return converted
}
