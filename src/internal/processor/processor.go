package processor

import "setlist/internal/domain"

type PlayerControl interface {
	Control(domain.PlayerInput) (domain.PlayerResult, error)
}

type Engine struct {
	library *domain.Library
	player  PlayerControl
}

func New(library *domain.Library) *Engine {
	return &Engine{library: library}
}

func NewWithPlayer(library *domain.Library, player PlayerControl) *Engine {
	return &Engine{library: library, player: player}
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
	case domain.PlayerControl:
		player, err := p.controlPlayer(*input.Player)
		if err != nil {
			return domain.TaskResult{}, err
		}
		result.Player = &player
	}
	return result, nil
}

func (p *Engine) controlPlayer(input domain.PlayerInput) (domain.PlayerResult, error) {
	if p.player == nil {
		return domain.PlayerResult{}, domain.ErrPlayerUnavailable
	}
	return p.player.Control(input)
}
