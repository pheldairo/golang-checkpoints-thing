package processor

import "setlist/internal/domain"

func same(a, b string) bool {
	return domain.Known(a) && domain.Known(b) && domain.Normalize(a) == domain.Normalize(b)
}

func (p *Engine) discovery(input domain.SimilarityInput) (*domain.SimilarityResult, error) {
	reference, err := p.library.Get(input.ReferenceTrackID)
	if err != nil {
		return nil, err
	}
	result := &domain.SimilarityResult{ReferenceTrackID: reference.ID, Matches: []domain.Selection{}}
	for _, t := range p.library.List() {
		if t.ID == reference.ID {
			continue
		}
		match := domain.Selection{Track: t, Reasons: []string{}}
		if same(reference.Genre, t.Genre) {
			match.Score += 3
			match.Reasons = append(match.Reasons, "same genre (+3)")
		}
		for _, mood := range domain.CleanTags(reference.MoodTags) {
			if domain.Contains(t.MoodTags, mood) {
				match.Score += 2
				match.Reasons = append(match.Reasons, "shared mood: "+mood+" (+2)")
			}
		}
		if same(reference.Artist, t.Artist) {
			match.Score += 2
			match.Reasons = append(match.Reasons, "same artist (+2)")
		}
		if same(reference.Album, t.Album) {
			match.Score++
			match.Reasons = append(match.Reasons, "same album (+1)")
		}
		if match.Score > 0 {
			result.Matches = append(result.Matches, match)
		}
	}
	rank(result.Matches)
	if len(result.Matches) > input.MaxMatches {
		result.Matches = result.Matches[:input.MaxMatches]
	}
	return result, nil
}
