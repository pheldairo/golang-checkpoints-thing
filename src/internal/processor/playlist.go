package processor

import (
	"fmt"
	"sort"

	"setlist/internal/domain"
)

// Genre preference: +3 once; each distinct overlapping preferred mood: +2.
// Required genres filter candidates, preferences never exclude candidates.
func (p *Engine) playlist(input domain.PlaylistInput) *domain.PlaylistResult {
	candidates := []domain.Selection{}
	for _, t := range p.library.FilterGenres(input.RequiredGenres) {
		choice := domain.Selection{Track: t, Reasons: []string{}}
		if domain.Contains(input.PreferredGenres, t.Genre) {
			choice.Score += 3
			choice.Reasons = append(choice.Reasons, "preferred genre: "+t.Genre+" (+3)")
		}
		for _, mood := range domain.CleanTags(input.PreferredMoodTags) {
			if domain.Contains(t.MoodTags, mood) {
				choice.Score += 2
				choice.Reasons = append(choice.Reasons, "preferred mood: "+mood+" (+2)")
			}
		}
		if len(choice.Reasons) == 0 {
			choice.Reasons = append(choice.Reasons, "eligible track; no preference matches (+0)")
		}
		candidates = append(candidates, choice)
	}
	rank(candidates)
	result := &domain.PlaylistResult{
		Tracks:                   []domain.Selection{},
		RemainingDurationSeconds: input.MaxDurationSeconds,
	}
	for _, c := range candidates {
		if len(result.Tracks) >= input.MaxTracks {
			break
		}
		if c.Track.DurationSeconds > result.RemainingDurationSeconds {
			continue
		}
		c.Reasons = append(c.Reasons, fmt.Sprintf("fits remaining duration (%d <= %d seconds)", c.Track.DurationSeconds, result.RemainingDurationSeconds))
		result.Tracks = append(result.Tracks, c)
		result.RemainingDurationSeconds -= c.Track.DurationSeconds
	}
	result.TotalDurationSeconds = input.MaxDurationSeconds - result.RemainingDurationSeconds
	result.SelectedCount = len(result.Tracks)
	return result
}

func rank(items []domain.Selection) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Score != items[j].Score {
			return items[i].Score > items[j].Score
		}
		return items[i].Track.ID < items[j].Track.ID
	})
}
