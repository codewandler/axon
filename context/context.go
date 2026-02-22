package context

import (
	"context"
	"io"

	"github.com/codewandler/axon/graph"
)

// Options configures the context gathering.
type Options struct {
	Task         string       // Task description (required)
	MaxTokens    int          // Token budget (default: 12000)
	MaxRing      Ring         // Maximum ring to expand (default: RingSiblings)
	IncludeTests bool         // Include test files (default: true)
	ScopeNodeID  string       // Optional: limit to descendants of this node
	Output       OutputFormat // Output format (default: FormatText)
	ManifestOnly bool         // Output manifest only, no source content
}

// DefaultOptions returns the default options.
func DefaultOptions() Options {
	return Options{
		MaxTokens:    12000,
		MaxRing:      RingSiblings,
		IncludeTests: true,
		Output:       FormatText,
	}
}

// Gather collects context for a task and formats it for output.
func Gather(ctx context.Context, storage graph.Storage, opts Options) (string, error) {
	// Initialize token counter
	counter, err := NewTokenCounter()
	if err != nil {
		return "", err
	}

	// Parse the task
	task := ParseTask(opts.Task)

	// Walk the graph to find relevant items
	walkOpts := WalkOptions{
		MaxRing:      opts.MaxRing,
		IncludeTests: opts.IncludeTests,
		ScopeNodeID:  opts.ScopeNodeID,
	}
	items, err := Walk(ctx, storage, task, walkOpts)
	if err != nil {
		return "", err
	}

	// Read source code
	sourceOpts := DefaultReadSourcesOptions()
	blocks, err := ReadSources(items, counter, sourceOpts)
	if err != nil {
		return "", err
	}

	// Fit to budget
	result := FitBudget(blocks, opts.MaxTokens, opts.Task, counter)

	// Format output
	if opts.ManifestOnly {
		return FormatManifest(result), nil
	}
	return Format(result, opts.Output), nil
}

// GatherToWriter collects context and writes directly to a writer.
func GatherToWriter(ctx context.Context, storage graph.Storage, opts Options, w io.Writer) error {
	output, err := Gather(ctx, storage, opts)
	if err != nil {
		return err
	}
	_, err = w.Write([]byte(output))
	return err
}
