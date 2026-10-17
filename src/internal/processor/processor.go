package processor

import "setlist/internal/domain"

type Engine struct {
	library *domain.Library
}

func New(library *domain.Library) *Engine {
	return &Engine{library: library}
}

func (p *Engine) Process(input domain.TaskInput) (domain.TaskResult, error) {
	if err := input.Validate(); err != nil {
		return domain.TaskResult{}, err
	}
	result := domain.TaskResult{Kind: input.Kind}
	switch input.Kind {
	case domain.GeneratePlaylist:
		result.Playlist = p.playlist(*input.Playlist)
	case domain.AnalyzeLibrary:
		result.Analysis = p.analysis(*input.Analysis)
	case domain.DiscoverSimilarTracks:
		var err error
		result.Similarity, err = p.discovery(*input.Similarity)
		if err != nil {
			return domain.TaskResult{}, err
		}
	}
	return result, nil
}
