// Package embeddings provides the Provider interface and built-in implementations
// for generating vector embeddings from text.
//
// # Library boundary
//
// This package is designed to be extractable as a standalone library.
// All files except indexer.go are free of axon-internal dependencies
// and import only the standard library and optional third-party backends.
//
// Provider implementations:
//   - [NullProvider]  – zero vectors, for testing
//   - [OllamaProvider] – calls a local Ollama daemon's HTTP API
//   - [HugotProvider]  – runs ONNX models in-process via Hugot (pure Go, no daemon)
package embeddings
