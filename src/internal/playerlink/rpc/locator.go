package rpc

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	AllTracksList = "1#1@[01]"
	PlainFileCue  = -1
)

var locatorPattern = regexp.MustCompile(`(?s)^<path>([^<]+)<cue_id>(-?[0-9]+)<name>(.*)$`)

type Locator struct {
	Path  string
	CueID int
	Name  string
}

func ParseLocator(raw string) (Locator, error) {
	match := locatorPattern.FindStringSubmatch(raw)
	if match == nil {
		return Locator{}, fmt.Errorf("%w: locator %q is not <path>...<cue_id>N<name>...", ErrArgumentRefused, raw)
	}
	cue, err := strconv.Atoi(match[2])
	if err != nil {
		return Locator{}, fmt.Errorf("%w: locator cue id %q is not a number", ErrArgumentRefused, match[2])
	}
	return Locator{Path: match[1], CueID: cue, Name: match[3]}, nil
}

func MetadataPath(locatorPath string) (string, error) {
	if len(locatorPath) < 3 || locatorPath[1:3] != `:\` {
		return "", fmt.Errorf("%w: locator path %q does not start with a drive letter", ErrArgumentRefused, locatorPath)
	}
	return `\` + locatorPath[:1] + locatorPath[2:], nil
}

const AlbumsList = "1#1@[05]"

func AlbumIdentifier(name string) (string, error) {
	if name == "" || strings.ContainsAny(name, "@\x00") {
		return "", fmt.Errorf("%w: album name %q is empty or contains '@'", ErrArgumentRefused, name)
	}
	return AlbumsList + name, nil
}

func IsAlbumIdentifier(identifier string) bool {
	name, found := strings.CutPrefix(identifier, AlbumsList)
	return found && name != "" && !strings.ContainsAny(name, "@\x00")
}
