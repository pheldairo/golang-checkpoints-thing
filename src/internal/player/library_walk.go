package player

import "setlist/internal/hibylink/rpc"

type libraryWalk struct {
	items        []libraryItem
	previousPage []libraryItem
	seen         map[string]bool
	limit        int
	nextStart    int
	finished     bool
}

func newLibraryWalk(limit int) *libraryWalk {
	return &libraryWalk{seen: map[string]bool{}, limit: limit}
}

func (w *libraryWalk) emptyFirstPage(page []libraryItem) bool {
	return w.nextStart == 0 && len(page) == 0
}

func (w *libraryWalk) add(page []libraryItem) {
	for _, item := range page[overlapLength(w.previousPage, page):] {
		if w.seen[item.Locator] {
			continue
		}
		w.seen[item.Locator] = true
		w.items = append(w.items, item)
		if len(w.items) >= w.limit {
			w.finished = true
			return
		}
	}
	if len(page) < rpc.MaxLibraryRows {
		w.finished = true
		return
	}
	w.previousPage = page
	w.nextStart += rpc.MaxLibraryRows
}
