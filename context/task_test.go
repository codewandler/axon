package context

import (
	"reflect"
	"testing"
)

func TestParseTask(t *testing.T) {
	tests := []struct {
		name         string
		description  string
		wantSymbols  []string
		wantKeywords []string
		wantIntent   TaskIntent
	}{
		{
			name:        "simple symbol",
			description: "add caching to Storage",
			wantSymbols: []string{"Storage"},
			wantIntent:  IntentModify,
		},
		{
			name:        "backtick quoted",
			description: "fix the `NewNode` function",
			wantSymbols: []string{"NewNode"},
			wantIntent:  IntentFix,
		},
		{
			name:        "multiple symbols",
			description: "refactor Storage and Query interfaces",
			wantSymbols: []string{"Storage", "Query"},
			wantIntent:  IntentModify,
		},
		{
			name:        "dotted path",
			description: "implement graph.Storage interface",
			wantSymbols: []string{"Storage"},
			wantIntent:  IntentAdd,
		},
		{
			name:        "camelCase",
			description: "debug the queryResult parsing",
			wantSymbols: []string{"queryResult"},
			wantIntent:  IntentFix,
		},
		{
			name:        "understand intent",
			description: "explain how the Indexer works",
			wantSymbols: []string{"Indexer"},
			wantIntent:  IntentUnderstand,
		},
		{
			name:        "remove intent",
			description: "delete the deprecated Handler type",
			wantSymbols: []string{"Handler"},
			wantIntent:  IntentRemove,
		},
		{
			name:        "complex task",
			description: "add error handling to the `Flush` method in Storage",
			wantSymbols: []string{"Flush", "Storage"},
			wantIntent:  IntentModify,
		},
		{
			name:         "keywords extraction",
			description:  "implement caching for database queries",
			wantKeywords: []string{"implement", "caching", "database", "queries"},
			wantIntent:   IntentAdd,
		},
		{
			name:        "no symbols",
			description: "improve performance",
			wantSymbols: nil,
			wantIntent:  IntentModify,
		},
		{
			name:        "mixed case sensitivity",
			description: "The Storage interface should handle errors",
			wantSymbols: []string{"Storage"},
			wantIntent:  IntentGeneral, // "should" is not an intent verb
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseTask(tt.description)

			if got.Raw != tt.description {
				t.Errorf("Raw = %q, want %q", got.Raw, tt.description)
			}

			if tt.wantSymbols != nil {
				if !reflect.DeepEqual(got.Symbols, tt.wantSymbols) {
					t.Errorf("Symbols = %v, want %v", got.Symbols, tt.wantSymbols)
				}
			}

			if tt.wantKeywords != nil {
				// Check that all expected keywords are present
				for _, kw := range tt.wantKeywords {
					found := false
					for _, gkw := range got.Keywords {
						if gkw == kw {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Keywords missing %q, got %v", kw, got.Keywords)
					}
				}
			}

			if got.Intent != tt.wantIntent {
				t.Errorf("Intent = %v, want %v", got.Intent, tt.wantIntent)
			}
		})
	}
}

func TestParseTaskSymbolUniqueness(t *testing.T) {
	// Symbols should be unique
	task := ParseTask("fix Storage and Storage issues with Storage")

	count := 0
	for _, s := range task.Symbols {
		if s == "Storage" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Expected Storage to appear once, got %d times in %v", count, task.Symbols)
	}
}

func TestParseTaskEmptyDescription(t *testing.T) {
	task := ParseTask("")
	if len(task.Symbols) != 0 {
		t.Errorf("Expected no symbols for empty description, got %v", task.Symbols)
	}
	if task.Intent != IntentGeneral {
		t.Errorf("Expected IntentGeneral for empty description, got %v", task.Intent)
	}
}

func TestTaskIntentString(t *testing.T) {
	tests := []struct {
		intent TaskIntent
		want   string
	}{
		{IntentGeneral, "general"},
		{IntentModify, "modify"},
		{IntentFix, "fix"},
		{IntentUnderstand, "understand"},
		{IntentAdd, "add"},
		{IntentRemove, "remove"},
	}

	for _, tt := range tests {
		got := tt.intent.String()
		if got != tt.want {
			t.Errorf("%v.String() = %q, want %q", tt.intent, got, tt.want)
		}
	}
}
