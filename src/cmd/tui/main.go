package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"setlist/internal/hibylink/session"
	"setlist/internal/player"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "tui:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("tui", flag.ContinueOnError)
	host := flags.String("player", "", "HiBy player (or fake player) host, required")
	dataPort := flags.Int("player-data-port", session.DefaultDataPort, "player HiByLink data port")
	heartbeatPort := flags.Int("player-heartbeat-port", session.DefaultHeartbeatPort, "player HiByLink heartbeat port")
	refresh := flags.Duration("refresh", time.Second, "how often the player status is read")
	cachePath := flags.String("library-cache", "", "file the player library is saved to (default hiby-library-HOST.json)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *host == "" {
		return errors.New("-player HOST is required")
	}
	if *refresh < 100*time.Millisecond {
		return errors.New("-refresh must be at least 100ms")
	}
	if *cachePath == "" {
		*cachePath = defaultCachePath(*host)
	}
	config := session.DefaultConfig(*host)
	config.DataPort = *dataPort
	config.HeartbeatPort = *heartbeatPort
	controller := player.NewController(config)
	defer controller.Close()
	_, err := tea.NewProgram(newModel(controller, *host, *cachePath, *refresh), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}
