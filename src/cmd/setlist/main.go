package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"setlist/internal/domain"
	"setlist/internal/importer"
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

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("setlist", flag.ContinueOnError)
	flags.SetOutput(out)
	root := flags.String("library", "", "explicit mounted music directory; default uses built-in sample")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments; use -library /path/to/music")
	}
	library := domain.NewLibrary()
	if *root == "" {
		for _, t := range sampleTracks() {
			if err := library.Add(t); err != nil {
				return err
			}
		}
		fmt.Fprintf(out, "Loaded sample library: %d tracks (no files scanned)\n", len(library.List()))
	} else {
		reader, err := importer.NewFFprobe()
		if err != nil {
			return err
		}
		scan, err := (importer.Directory{Reader: reader}).Scan(*root)
		if err != nil {
			return err
		}
		for _, t := range scan.Tracks {
			if err = library.Add(t); err != nil {
				return err
			}
		}
		fmt.Fprintf(out, "Import: scanned=%d imported=%d skipped=%d metadata_errors=%d traversal_errors=%d\n",
			scan.Summary.ScannedFiles, scan.Summary.ImportedTracks, scan.Summary.SkippedFiles,
			len(scan.Summary.MetadataErrors), len(scan.Summary.TraversalErrors))
		for _, issue := range append(scan.Summary.MetadataErrors, scan.Summary.TraversalErrors...) {
			fmt.Fprintf(out, "  %s: %s\n", issue.Path, issue.Error)
		}
	}
	tasks := service.New(storage.NewMemory(), processor.New(library))
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
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	for _, input := range inputs {
		task, err := tasks.Create(input)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "\n%s %s: %s\n", task.ID, input.Kind, task.Status)
		task, err = tasks.Process(task.ID)
		if encodeErr := encoder.Encode(task); encodeErr != nil {
			return encodeErr
		}
		if err != nil {
			return err
		}
	}
	if len(tracks) == 0 {
		fmt.Fprintln(out, "Discovery skipped: library has no reference track.")
	}
	return nil
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
