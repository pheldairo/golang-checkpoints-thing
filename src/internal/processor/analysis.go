package processor

import (
	"sort"

	"setlist/internal/domain"
)

func (p *Engine) analysis(input domain.AnalysisInput) *domain.AnalysisResult {
	report := &domain.AnalysisResult{
		Genres:          []domain.GenreCount{},
		MissingMetadata: []domain.IncompleteTrack{},
		DuplicateGroups: [][]string{},
	}
	artists := map[string]bool{}
	albums := map[string]bool{}
	genres := map[string]domain.GenreCount{}
	// A tuple avoids collisions so here we are.
	duplicates := map[[3]string][]string{}
	for _, t := range p.library.List() {
		report.TotalTracks++
		report.TotalDurationSeconds += t.DurationSeconds
		if domain.Known(t.Artist) {
			artists[domain.Normalize(t.Artist)] = true
		}
		if domain.Known(t.Album) {
			albums[domain.Normalize(t.Album)] = true
		}
		genre := t.Genre
		if !domain.Known(genre) {
			genre = "Unknown"
		}
		key := domain.Normalize(genre)
		entry := genres[key]
		if entry.Count == 0 {
			entry.Genre = genre
		}
		entry.Count++
		genres[key] = entry
		if input.IncludeCompleteness {
			missing := append([]string(nil), t.MissingMetadata...)
			for _, field := range []struct{ name, value string }{
				{"title", t.Title}, {"artist", t.Artist}, {"album", t.Album}, {"genre", t.Genre},
			} {
				if !domain.Known(field.value) {
					missing = append(missing, field.name)
				}
			}
			missing = domain.CleanTags(missing)
			if len(missing) > 0 {
				report.MissingMetadata = append(report.MissingMetadata, domain.IncompleteTrack{TrackID: t.ID, Fields: missing})
			}
		}
		if input.DetectDuplicates && duplicateEligible(t) {
			key := [3]string{domain.Normalize(t.Artist), domain.Normalize(t.Album), domain.Normalize(t.Title)}
			duplicates[key] = append(duplicates[key], t.ID)
		}
	}
	report.UniqueArtists = len(artists)
	report.UniqueAlbums = len(albums)
	for _, entry := range genres {
		report.Genres = append(report.Genres, entry)
	}
	sort.Slice(report.Genres, func(i, j int) bool {
		return domain.Normalize(report.Genres[i].Genre) < domain.Normalize(report.Genres[j].Genre)
	})
	for _, ids := range duplicates {
		if len(ids) > 1 {
			report.DuplicateGroups = append(report.DuplicateGroups, ids)
		}
	}
	sort.Slice(report.DuplicateGroups, func(i, j int) bool {
		return report.DuplicateGroups[i][0] < report.DuplicateGroups[j][0]
	})
	return report
}

func duplicateEligible(t domain.Track) bool {
	return domain.Known(t.Artist) && domain.Known(t.Album) && domain.Known(t.Title) &&
		!domain.Contains(t.MissingMetadata, "title") && !domain.Contains(t.MissingMetadata, "album")
}
