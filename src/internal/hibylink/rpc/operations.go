package rpc

import (
	"errors"
	"fmt"
	"slices"
)

var ErrArgumentRefused = errors.New("argument outside the reviewed scope, nothing was sent")

const (
	serviceSession  = 0xc500
	serviceDevice   = 0xc501
	serviceState    = 0xc502
	serviceControl  = 0xc503
	serviceLibrary  = 0xc504
	maxSeekMs       = 24 * 60 * 60 * 1000
	MaxLibraryRows  = 50
	maxLibraryIndex = 0x7fffffff
)

type Operation struct {
	Name     string
	Service  uint16
	Action   uint16
	Request  string
	Replies  []string
	Validate func(args []any) error
}

func (op Operation) acceptsReply(signature string) bool {
	return slices.Contains(op.Replies, signature)
}

var (
	GetModelNumber = Operation{Name: "getModelNumber", Service: serviceDevice, Action: 0x1002, Replies: []string{"is"}}
	GetMaxVolume   = Operation{Name: "getMaxVolume", Service: serviceDevice, Action: 0x1008, Replies: []string{"i"}}
	GetPlayState   = Operation{Name: "getPlayState", Service: serviceState, Action: 0x1101, Replies: []string{"i"}}
	GetPlayMode    = Operation{Name: "getPlayMode", Service: serviceState, Action: 0x1102, Replies: []string{"i"}}
	GetProgress    = Operation{Name: "getProgress", Service: serviceState, Action: 0x1103, Replies: []string{"i"}}
	GetVolume      = Operation{Name: "getVolume", Service: serviceState, Action: 0x1105, Replies: []string{"i"}}
	GetMetaInfo    = Operation{Name: "getMetaInfo", Service: serviceState, Action: 0x1108, Replies: []string{MetadataSignature}}
	GetCover       = Operation{Name: "getCover", Service: serviceState, Action: 0x1104, Replies: []string{CoverSignature}}

	GetPlaylistSize = Operation{
		Name: "getPlaylistSize", Service: serviceLibrary, Action: 0x1301, Request: "s",
		Replies: []string{"i"}, Validate: validateListSize,
	}
	GetPlaylistLists = Operation{
		Name: "getPlaylistLists", Service: serviceLibrary, Action: 0x1303, Request: "suu",
		Replies: []string{AlbumRowsSignature, "i"}, Validate: validateAlbumListPage,
	}
	GetPlaylistItems = Operation{
		Name: "getPlaylistItems", Service: serviceLibrary, Action: 0x1302, Request: "suu",
		Replies: []string{LibraryItemsSignature, "i"}, Validate: validateLibraryPage,
	}

	SetPlayState = Operation{
		Name: "setPlayState", Service: serviceControl, Action: 0x1201, Request: "y",
		Replies: []string{"i"}, Validate: allowedIntegers(PlayStateResume, PlayStatePause),
	}
	SetPlayMode = Operation{
		Name: "setPlayMode", Service: serviceControl, Action: 0x1202, Request: "y",
		Replies: []string{"i"}, Validate: allowedIntegers(1, 2, 3, 4),
	}
	SetVolume = Operation{
		Name: "setVolume", Service: serviceControl, Action: 0x1203, Request: "y",
		Replies: []string{"i"}, Validate: integerInRange(0, 0xff),
	}
	PlayUUID = Operation{
		Name: "playUuid", Service: serviceControl, Action: 0x1204, Request: "ss",
		Replies: []string{"i"}, Validate: validatePlayUUID,
	}
	PlayPrevious = Operation{Name: "playPrevious", Service: serviceControl, Action: 0x1205, Replies: []string{"i"}}
	PlayNext     = Operation{Name: "playNext", Service: serviceControl, Action: 0x1206, Replies: []string{"i"}}
	SetProgress  = Operation{
		Name: "setProgress", Service: serviceControl, Action: 0x1207, Request: "u",
		Replies: []string{"i"}, Validate: integerInRange(0, maxSeekMs),
	}
)

const (
	PlayStateResume       = 1
	PlayStatePause        = 2
	MetadataSignature     = "ia(sssssssuuuuqyyy)"
	LibraryItemsSignature = "ia(sssuuyy)"
	CoverSignature        = "iay"
	AlbumRowsSignature    = "ia(ssuu)"
)

func refused(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrArgumentRefused, fmt.Sprintf(format, args...))
}

func integerArguments(args []any, count int) ([]int, error) {
	if len(args) != count {
		return nil, refused("expected %d arguments, got %d", count, len(args))
	}
	numbers := make([]int, count)
	for index, arg := range args {
		number, ok := arg.(int)
		if !ok {
			return nil, refused("argument %d must be an int, got %T", index+1, arg)
		}
		numbers[index] = number
	}
	return numbers, nil
}

func allowedIntegers(allowed ...int) func([]any) error {
	return func(args []any) error {
		numbers, err := integerArguments(args, 1)
		if err != nil {
			return err
		}
		if !slices.Contains(allowed, numbers[0]) {
			return refused("value %d is not one of %v", numbers[0], allowed)
		}
		return nil
	}
}

func integerInRange(minimum, maximum int) func([]any) error {
	return func(args []any) error {
		numbers, err := integerArguments(args, 1)
		if err != nil {
			return err
		}
		if numbers[0] < minimum || numbers[0] > maximum {
			return refused("value %d is outside %d..%d", numbers[0], minimum, maximum)
		}
		return nil
	}
}

func requireTrackList(arg any) error {
	identifier, ok := arg.(string)
	if ok && (identifier == AllTracksList || IsAlbumIdentifier(identifier)) {
		return nil
	}
	return refused("only the all-tracks list or a single album is allowed")
}

func validateListSize(args []any) error {
	if len(args) != 1 {
		return refused("getPlaylistSize takes one list identifier")
	}
	if identifier, ok := args[0].(string); ok && identifier == AlbumsList {
		return nil
	}
	return requireTrackList(args[0])
}

func validatePageBounds(args []any) error {
	bounds, err := integerArguments(args, 2)
	if err != nil {
		return err
	}
	start, end := bounds[0], bounds[1]
	if start < 0 || start > end || end > maxLibraryIndex {
		return refused("list bounds need 0 <= start <= end")
	}
	if end-start+1 > MaxLibraryRows {
		return refused("at most %d rows per request", MaxLibraryRows)
	}
	return nil
}

func validateAlbumListPage(args []any) error {
	if len(args) != 3 {
		return refused("getPlaylistLists takes a list identifier, start and end")
	}
	if identifier, ok := args[0].(string); !ok || identifier != AlbumsList {
		return refused("only the album list %q is allowed", AlbumsList)
	}
	return validatePageBounds(args[1:])
}

func validateLibraryPage(args []any) error {
	if len(args) != 3 {
		return refused("getPlaylistItems takes a list identifier, start and end")
	}
	if err := requireTrackList(args[0]); err != nil {
		return err
	}
	return validatePageBounds(args[1:])
}

func validatePlayUUID(args []any) error {
	if len(args) != 2 {
		return refused("playUuid takes a list identifier and a locator")
	}
	if err := requireTrackList(args[0]); err != nil {
		return err
	}
	raw, ok := args[1].(string)
	if !ok {
		return refused("locator must be a string")
	}
	locator, err := ParseLocator(raw)
	if err != nil {
		return err
	}
	if locator.CueID != PlainFileCue {
		return refused("only plain-file locators (cue id %d) are allowed", PlainFileCue)
	}
	return nil
}
