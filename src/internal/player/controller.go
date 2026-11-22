package player

import (
	"errors"
	"fmt"
	"sync"

	"setlist/internal/domain"
	"setlist/internal/playerlink/session"
)

var ErrControllerClosed = errors.New("player controller is closed")

type Connection interface {
	Caller
	Usable() bool
	Close()
}

type Opener func() (Connection, error)

type Controller struct {
	lock       sync.Mutex
	open       Opener
	timing     Timing
	connection Connection
	closed     bool
}

func NewController(config session.Config) *Controller {
	return NewControllerWithOpener(func() (Connection, error) {
		opened, err := session.Open(config)
		if err != nil {
			return nil, err
		}
		return opened, nil
	}, DefaultTiming())
}

func NewControllerWithOpener(open Opener, timing Timing) *Controller {
	return &Controller{open: open, timing: timing}
}

func (c *Controller) Control(input domain.PlayerInput) (domain.PlayerResult, error) {
	if err := input.Validate(); err != nil {
		return domain.PlayerResult{}, err
	}
	var result domain.PlayerResult
	err := c.withDevice(func(d device) error {
		var err error
		result, err = d.perform(input)
		return err
	})
	return result, err
}

func (c *Controller) withDevice(action func(device) error) error {
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.closed {
		return ErrControllerClosed
	}
	connection, err := c.usableConnection()
	if err != nil {
		return fmt.Errorf("open player session: %w", err)
	}
	return action(device{caller: connection, timing: c.timing})
}

func (c *Controller) usableConnection() (Connection, error) {
	if c.connection != nil && c.connection.Usable() {
		return c.connection, nil
	}
	if c.connection != nil {
		c.connection.Close()
		c.connection = nil
	}
	connection, err := c.open()
	if err != nil {
		return nil, err
	}
	c.connection = connection
	return connection, nil
}

func (c *Controller) Close() {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.closed = true
	if c.connection != nil {
		c.connection.Close()
		c.connection = nil
	}
}

func (d device) perform(input domain.PlayerInput) (domain.PlayerResult, error) {
	switch input.Action {
	case domain.ActionStatus:
		return d.status()
	case domain.ActionSetVolume:
		return d.setVolume(input)
	case domain.ActionPause:
		return d.changePlayState(domain.ActionPause, pauseChange)
	case domain.ActionResume:
		return d.changePlayState(domain.ActionResume, resumeChange)
	case domain.ActionToggle:
		return d.toggle()
	case domain.ActionNext:
		return d.next()
	case domain.ActionPrevious:
		return d.previous()
	case domain.ActionRestart:
		return d.restart()
	case domain.ActionSeek:
		return d.seek(input.PositionMs)
	case domain.ActionSetPlayMode:
		return d.setPlayMode(input.PlayMode)
	case domain.ActionLibrary:
		return d.library(input.Offset, input.Limit)
	case domain.ActionPlayTrack:
		return d.playTrack(input)
	}
	return domain.PlayerResult{}, fmt.Errorf("%w: unsupported player action %q", domain.ErrInvalidInput, input.Action)
}
