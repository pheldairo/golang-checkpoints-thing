package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"setlist/internal/domain"
	"setlist/internal/importer"
	"setlist/internal/player"
	"setlist/internal/playerlink/session"
	"setlist/internal/processor"
	"setlist/internal/service"
	"setlist/internal/storage"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "setlist:", err)
		os.Exit(1)
	}
}

type options struct {
	libraryRoot   string
	workers       int
	queueCapacity int
	playerHost    string
	dataPort      int
	heartbeatPort int
	playerActions string
}

func parseOptions(args []string, out io.Writer) (options, bool, error) {
	var parsed options
	flags := flag.NewFlagSet("setlist", flag.ContinueOnError)
	flags.SetOutput(out)
	flags.StringVar(&parsed.libraryRoot, "library", "", "explicit mounted music directory; default uses built-in sample")
	flags.IntVar(&parsed.workers, "workers", service.DefaultWorkers, "number of worker goroutines")
	flags.IntVar(&parsed.queueCapacity, "queue", service.DefaultQueueCapacity, "capacity of the bounded task queue")
	flags.StringVar(&parsed.playerHost, "player", "", "music player (or fake player) host; enables player control tasks")
	flags.IntVar(&parsed.dataPort, "player-data-port", session.DefaultDataPort, "player data port")
	flags.IntVar(&parsed.heartbeatPort, "player-heartbeat-port", session.DefaultHeartbeatPort, "player heartbeat port")
	flags.StringVar(&parsed.playerActions, "player-actions", defaultPlayerActions, "comma-separated player tasks, see README")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return parsed, true, nil
		}
		return parsed, false, err
	}
	if flags.NArg() != 0 {
		return parsed, false, fmt.Errorf("unexpected positional arguments; use -library /path/to/music")
	}
	actionsGiven := false
	flags.Visit(func(f *flag.Flag) { actionsGiven = actionsGiven || f.Name == "player-actions" })
	if actionsGiven && parsed.playerHost == "" {
		return parsed, false, fmt.Errorf("-player-actions needs -player HOST")
	}
	return parsed, false, nil
}

func run(args []string, out io.Writer) error {
	opts, helpShown, err := parseOptions(args, out)
	if err != nil || helpShown {
		return err
	}
	library, err := loadLibrary(opts.libraryRoot, out)
	if err != nil {
		return err
	}
	inputs := libraryInputs(library)
	engine := processor.New(library)
	if opts.playerHost != "" {
		playerInputs, err := parsePlayerActions(opts.playerActions)
		if err != nil {
			return err
		}
		inputs = append(inputs, playerInputs...)
		config := session.DefaultConfig(opts.playerHost)
		config.DataPort = opts.dataPort
		config.HeartbeatPort = opts.heartbeatPort
		controller := player.NewController(config)
		defer controller.Close()
		engine = processor.NewWithPlayer(library, controller)
	}
	tasks := service.New(storage.NewMemory(), engine)
	pool, err := service.StartPool(tasks, opts.workers, opts.queueCapacity)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Worker pool: %d workers, queue capacity %d\n", pool.Workers(), pool.QueueCapacity())
	if err := submitAll(pool, inputs, out); err != nil {
		pool.Shutdown()
		return err
	}
	pool.Shutdown()
	if len(library.List()) == 0 {
		fmt.Fprintln(out, "Discovery skipped: library has no reference track.")
	}
	return printResults(tasks.List(), out)
}

func submitAll(pool *service.Pool, inputs []domain.TaskInput, out io.Writer) error {
	for _, input := range inputs {
		task, err := pool.CreateAndSubmit(input)
		if task.ID == "" {
			return err
		}
		line := fmt.Sprintf("%s %s: %s", task.ID, describeInput(input), task.Status)
		if err != nil {
			line += fmt.Sprintf(" (not accepted: %v)", err)
		}
		fmt.Fprintln(out, line)
	}
	return nil
}

func describeInput(input domain.TaskInput) string {
	if input.Player != nil {
		return fmt.Sprintf("%s (%s)", input.Kind, input.Player.Action)
	}
	return string(input.Kind)
}

func printResults(tasks []domain.Task, out io.Writer) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	failed := 0
	for _, task := range tasks {
		fmt.Fprintf(out, "\n%s %s: %s\n", task.ID, describeInput(task.Input), task.Status)
		if err := encoder.Encode(task); err != nil {
			return err
		}
		if task.Status == domain.StatusFailed {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d tasks failed", failed, len(tasks))
	}
	return nil
}

func libraryInputs(library *domain.Library) []domain.TaskInput {
	inputs := []domain.TaskInput{
		{Kind: domain.AnalyzeLibrary, Analysis: &domain.AnalysisInput{DetectDuplicates: true, IncludeCompleteness: true}},
		{Kind: domain.GeneratePlaylist, Playlist: &domain.PlaylistInput{
			MaxDurationSeconds: 2700, PreferredGenres: []string{"Electronic", "Jazz"},
			PreferredMoodTags: []string{"Relaxed"}, MaxTracks: 12,
		}},
	}
	tracks := library.List()
	if len(tracks) > 0 {
		inputs = append(inputs, domain.TaskInput{
			Kind:       domain.DiscoverSimilarTracks,
			Similarity: &domain.SimilarityInput{ReferenceTrackID: tracks[0].ID, MaxMatches: 3},
		})
	}
	return inputs
}

func loadLibrary(root string, out io.Writer) (*domain.Library, error) {
	library := domain.NewLibrary()
	if root == "" {
		for _, t := range sampleTracks() {
			if err := library.Add(t); err != nil {
				return nil, err
			}
		}
		fmt.Fprintf(out, "Loaded sample library: %d tracks (no files scanned)\n", len(library.List()))
		return library, nil
	}
	reader, err := importer.NewFFprobe()
	if err != nil {
		return nil, err
	}
	scan, err := (importer.Directory{Reader: reader}).Scan(root)
	if err != nil {
		return nil, err
	}
	for _, t := range scan.Tracks {
		if err = library.Add(t); err != nil {
			return nil, err
		}
	}
	fmt.Fprintf(out, "Import: scanned=%d imported=%d skipped=%d metadata_errors=%d traversal_errors=%d\n",
		scan.Summary.ScannedFiles, scan.Summary.ImportedTracks, scan.Summary.SkippedFiles,
		len(scan.Summary.MetadataErrors), len(scan.Summary.TraversalErrors))
	for _, issue := range append(scan.Summary.MetadataErrors, scan.Summary.TraversalErrors...) {
		fmt.Fprintf(out, "  %s: %s\n", issue.Path, issue.Error)
	}
	return library, nil
}

func sampleTracks() []domain.Track {
	return []domain.Track{
		{ID: "sample-01", Title: "Evening Circuit", Artist: "Sample Ensemble", Album: "City Lights", Genre: "Electronic", DurationSeconds: 240, TrackNumber: 1, MoodTags: []string{"Relaxed"}},
		{ID: "sample-02", Title: "Blue Hour", Artist: "Demo Trio", Album: "After Hours", Genre: "Jazz", DurationSeconds: 300, TrackNumber: 1, MoodTags: []string{"Relaxed"}},
		{ID: "sample-03", Title: "Night Bus", Artist: "Sample Ensemble", Album: "City Lights", Genre: "Electronic", DurationSeconds: 210, TrackNumber: 2, MoodTags: []string{"Energetic"}},
		{ID: "sample-04", Title: "Open Road", Artist: "Fixture Band", Album: "Travels", Genre: "Rock", DurationSeconds: 180, TrackNumber: 1},
		{ID: "sample-05", Title: "Blue Hour", Artist: "Demo Trio", Album: "After Hours", Genre: "Jazz", DurationSeconds: 300, TrackNumber: 1, MoodTags: []string{"Relaxed"}},
		{ID: "sample-06", Title: "Untitled Sketch", Artist: "Unknown", Album: "Sketches", Genre: "Unknown", DurationSeconds: 120},
	}
}
