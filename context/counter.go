// Package context provides context window optimization for AI agents.
package context

import (
	"sync"

	tiktoken "github.com/pkoukk/tiktoken-go"
	tiktoken_loader "github.com/pkoukk/tiktoken-go-loader"
)

// TokenCounter provides token counting using cl100k_base encoding.
// This is an approximation for Claude tokenization (~5-15% variance).
type TokenCounter struct {
	enc  *tiktoken.Tiktoken
	once sync.Once
	err  error
}

// NewTokenCounter creates a new token counter.
// Uses the offline BPE loader to avoid network calls at runtime.
func NewTokenCounter() (*TokenCounter, error) {
	tc := &TokenCounter{}
	tc.once.Do(func() {
		// Use offline loader - BPE data is embedded in the binary
		tiktoken.SetBpeLoader(tiktoken_loader.NewOfflineLoader())
		tc.enc, tc.err = tiktoken.GetEncoding("cl100k_base")
	})
	if tc.err != nil {
		return nil, tc.err
	}
	return tc, nil
}

// Count returns the number of tokens in the given text.
func (tc *TokenCounter) Count(text string) int {
	if tc.enc == nil {
		// Fallback to character-based estimate if encoder not available
		return len(text) / 4
	}
	tokens := tc.enc.Encode(text, nil, nil)
	return len(tokens)
}

// CountLines returns the total token count for a slice of lines.
func (tc *TokenCounter) CountLines(lines []string) int {
	total := 0
	for _, line := range lines {
		total += tc.Count(line)
		total++ // newline
	}
	return total
}

// EstimateFromChars provides a rough token estimate from character count.
// Useful when you want to avoid the overhead of actual tokenization.
// Rule of thumb: ~4 characters per token for English/code.
func EstimateFromChars(chars int) int {
	return (chars + 3) / 4 // round up
}
