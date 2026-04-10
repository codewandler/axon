package embeddings

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/pipelines"
)

const (
	// DefaultHugotModel is the HuggingFace repo slug for the default ONNX model.
	// KnightsAnalytics maintain a pre-exported ONNX version of all-MiniLM-L6-v2
	// that works with Hugot's pure-Go backend out of the box.
	DefaultHugotModel = "KnightsAnalytics/all-MiniLM-L6-v2"

	// DefaultHugotDims is the embedding dimension produced by the default model.
	DefaultHugotDims = 384
)

// HugotProvider runs ONNX sentence-embedding models fully in-process using
// the Hugot pure-Go backend (GoMLX). No daemon, no CGO, no data leaves the host.
//
// The provider is lazy: the Hugot session and pipeline are created on the first
// call to [HugotProvider.Embed]. If the model files are not present at modelPath
// they are downloaded from HuggingFace on that first call (~90 MB, cached).
//
// Thread-safe: multiple goroutines may call Embed concurrently after the first
// call has completed initialization.
type HugotProvider struct {
	modelPath string // local directory containing model.onnx + tokenizer.json
	modelName string // HuggingFace repo slug
	dims      int

	once     sync.Once
	session  *hugot.Session
	pipeline *pipelines.FeatureExtractionPipeline
	initErr  error
}

// NewHugot creates a HugotProvider.
//
//   - modelPath: local directory containing the ONNX model files.
//     If empty, defaults to ~/.axon/models/<model-slug>.
//     The directory is created and the model downloaded on first Embed call
//     if model.onnx is not already present.
//
//   - model: HuggingFace repo slug, e.g. "KnightsAnalytics/all-MiniLM-L6-v2".
//     Defaults to [DefaultHugotModel] if empty.
func NewHugot(modelPath, model string) *HugotProvider {
	if model == "" {
		model = DefaultHugotModel
	}
	if modelPath == "" {
		home, _ := os.UserHomeDir()
		// Match hugot's own naming convention: replace "/" with "_", keep full slug.
		// e.g. "KnightsAnalytics/all-MiniLM-L6-v2" → "KnightsAnalytics_all-MiniLM-L6-v2"
		slug := strings.ReplaceAll(model, "/", "_")
		modelPath = filepath.Join(home, ".axon", "models", slug)
	}
	return &HugotProvider{
		modelPath: modelPath,
		modelName: model,
		dims:      DefaultHugotDims,
	}
}

func (p *HugotProvider) Name() string    { return "hugot/" + p.modelName }
func (p *HugotProvider) Dimensions() int { return p.dims }

// Close destroys the underlying Hugot session and releases all resources.
// Safe to call on an uninitialized or already-closed provider.
func (p *HugotProvider) Close() error {
	if p.session != nil {
		return p.session.Destroy()
	}
	return nil
}

// initialize creates the Hugot session and feature-extraction pipeline exactly once.
// Errors are stored in p.initErr and returned on every subsequent Embed call.
func (p *HugotProvider) initialize() {
	p.once.Do(func() {
		// Download model if model.onnx is absent.
		onnxPath := filepath.Join(p.modelPath, "model.onnx")
		if _, err := os.Stat(onnxPath); errors.Is(err, os.ErrNotExist) {
			fmt.Printf("hugot: downloading model %q to %s (one-time, ~90 MB)…\n",
				p.modelName, p.modelPath)
			destDir := filepath.Dir(p.modelPath)
			if err := os.MkdirAll(destDir, 0o755); err != nil {
				p.initErr = fmt.Errorf("hugot: mkdir %s: %w", destDir, err)
				return
			}
			// Use the path hugot actually created — it may differ from our default.
			downloadedPath, err := hugot.DownloadModel(p.modelName, destDir,
				hugot.NewDownloadOptions())
			if err != nil {
				p.initErr = fmt.Errorf("hugot: download %q: %w", p.modelName, err)
				return
			}
			p.modelPath = downloadedPath
		}

		session, err := hugot.NewGoSession()
		if err != nil {
			p.initErr = fmt.Errorf("hugot: new session: %w", err)
			return
		}

		pipe, err := hugot.NewPipeline(session, hugot.FeatureExtractionConfig{
			ModelPath: p.modelPath,
			Name:      "axon-embed",
		})
		if err != nil {
			_ = session.Destroy()
			p.initErr = fmt.Errorf("hugot: new pipeline: %w", err)
			return
		}

		p.session = session
		p.pipeline = pipe
	})
}

// Embed generates an embedding for a single text. Delegates to EmbedBatch.
//
// Note: ctx is not forwarded to the pipeline — hugot's pure-Go backend does
// not yet support context cancellation in RunPipeline.
func (p *HugotProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	out, err := p.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return out[0], nil
}

// EmbedBatch generates embeddings for all texts in a single model forward pass.
// Hugot's RunPipeline natively accepts a []string, so the whole batch is
// tokenized and inferred together — no extra overhead vs a single call.
//
// Note: ctx is not forwarded — hugot's pure-Go backend does not yet support
// context cancellation in RunPipeline.
func (p *HugotProvider) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	p.initialize()
	if p.initErr != nil {
		return nil, p.initErr
	}

	out, err := p.pipeline.RunPipeline(texts)
	if err != nil {
		return nil, fmt.Errorf("hugot: run pipeline: %w", err)
	}
	if len(out.Embeddings) == 0 {
		return nil, fmt.Errorf("hugot: empty embedding output")
	}
	return out.Embeddings, nil
}
