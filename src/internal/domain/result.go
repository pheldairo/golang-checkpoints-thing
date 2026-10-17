package domain

type Selection struct {
	Track   Track
	Score   int
	Reasons []string
}

type PlaylistResult struct {
	Tracks                   []Selection
	TotalDurationSeconds     int
	RemainingDurationSeconds int
	SelectedCount            int
}

type GenreCount struct {
	Genre string
	Count int
}

type IncompleteTrack struct {
	TrackID string
	Fields  []string
}

type AnalysisResult struct {
	TotalTracks          int
	UniqueArtists        int
	UniqueAlbums         int
	TotalDurationSeconds int
	Genres               []GenreCount
	MissingMetadata      []IncompleteTrack
	DuplicateGroups      [][]string
}

type SimilarityResult struct {
	ReferenceTrackID string
	Matches          []Selection
}

// Kind discriminates exactly one populated result branch.
type TaskResult struct {
	Kind       TaskType
	Playlist   *PlaylistResult   `json:",omitempty"`
	Analysis   *AnalysisResult   `json:",omitempty"`
	Similarity *SimilarityResult `json:",omitempty"`
}

func (r TaskResult) Validate(kind TaskType) error {
	count := 0
	if r.Playlist != nil {
		count++
	}
	if r.Analysis != nil {
		count++
	}
	if r.Similarity != nil {
		count++
	}
	if count != 1 || r.Kind != kind {
		return ErrInvalidResult
	}
	switch kind {
	case GeneratePlaylist:
		if r.Playlist != nil {
			return nil
		}
	case AnalyzeLibrary:
		if r.Analysis != nil {
			return nil
		}
	case DiscoverSimilarTracks:
		if r.Similarity != nil {
			return nil
		}
	}
	return ErrInvalidResult
}

func cloneSelections(items []Selection) []Selection {
	if items == nil {
		return nil
	}
	out := make([]Selection, len(items))
	for i, item := range items {
		out[i] = item
		out[i].Track = item.Track.Clone()
		out[i].Reasons = append([]string(nil), item.Reasons...)
	}
	return out
}

func (r TaskResult) Clone() TaskResult {
	if r.Playlist != nil {
		result := *r.Playlist
		result.Tracks = cloneSelections(result.Tracks)
		r.Playlist = &result
	}
	if r.Similarity != nil {
		result := *r.Similarity
		result.Matches = cloneSelections(result.Matches)
		r.Similarity = &result
	}
	if r.Analysis != nil {
		result := *r.Analysis
		result.Genres = append([]GenreCount(nil), result.Genres...)
		result.MissingMetadata = make([]IncompleteTrack, len(r.Analysis.MissingMetadata))
		for i, item := range r.Analysis.MissingMetadata {
			result.MissingMetadata[i] = IncompleteTrack{
				TrackID: item.TrackID,
				Fields:  append([]string(nil), item.Fields...),
			}
		}
		result.DuplicateGroups = make([][]string, len(r.Analysis.DuplicateGroups))
		for i, ids := range r.Analysis.DuplicateGroups {
			result.DuplicateGroups[i] = append([]string(nil), ids...)
		}
		r.Analysis = &result
	}
	return r
}
