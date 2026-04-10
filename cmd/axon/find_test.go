package main

import (
	"testing"
	"time"

	"github.com/codewandler/axon/graph"
	"github.com/codewandler/axon/types"
)

func TestCommitDisplay_CommitData(t *testing.T) {
	date := time.Date(2024, 12, 15, 0, 0, 0, 0, time.UTC)
	node := &graph.Node{
		Type: types.TypeCommit,
		Name: "ee48448e -- fix something",
		Key:  "ee48448eabcdef01",
		Data: types.CommitData{
			SHA:          "ee48448eabcdef01",
			Message:      "fix: prevent agent loss",
			AuthorName:   "timo",
			AuthorDate:   date,
			FilesChanged: 3,
		},
	}

	got := commitDisplay(node)
	want := "ee48448e -- fix: prevent agent loss  by timo (2024-12-15, 3 files)"
	if got != want {
		t.Errorf("commitDisplay(CommitData)\n  got:  %q\n  want: %q", got, want)
	}
}

func TestCommitDisplay_Map(t *testing.T) {
	node := &graph.Node{
		Type: types.TypeCommit,
		Name: "ee48448e -- fix something",
		Key:  "ee48448eabcdef01",
		Data: map[string]any{
			"sha":           "ee48448eabcdef01",
			"message":       "fix: prevent agent loss",
			"author_name":   "timo",
			"author_date":   "2024-12-15T00:00:00Z",
			"files_changed": float64(3),
		},
	}

	got := commitDisplay(node)
	want := "ee48448e -- fix: prevent agent loss  by timo (2024-12-15, 3 files)"
	if got != want {
		t.Errorf("commitDisplay(map)\n  got:  %q\n  want: %q", got, want)
	}
}

func TestCommitDisplay_FallsBackToName(t *testing.T) {
	node := &graph.Node{
		Type: types.TypeCommit,
		Name: "ee48448e -- fallback subject",
		Key:  "ee48448eabcdef01",
		Data: nil,
	}

	got := commitDisplay(node)
	if got != node.Name {
		t.Errorf("commitDisplay(nil data) = %q, want Name %q", got, node.Name)
	}
}

func TestNodeDisplay_Commit(t *testing.T) {
	date := time.Date(2024, 12, 15, 0, 0, 0, 0, time.UTC)
	node := &graph.Node{
		Type: types.TypeCommit,
		Key:  "ee48448eabcdef01",
		Data: types.CommitData{
			SHA:          "ee48448eabcdef01",
			Message:      "feat: watch mode",
			AuthorName:   "timo",
			AuthorDate:   date,
			FilesChanged: 5,
		},
	}

	got := nodeDisplay(node)
	want := "ee48448e -- feat: watch mode  by timo (2024-12-15, 5 files)"
	if got != want {
		t.Errorf("nodeDisplay(commit)\n  got:  %q\n  want: %q", got, want)
	}
}

func TestNodeDisplay_File(t *testing.T) {
	node := &graph.Node{
		Type: types.TypeFile,
		URI:  "file:///home/user/project/main.go",
		Key:  "/home/user/project/main.go",
		Name: "main.go",
	}

	got := nodeDisplay(node)
	want := "/home/user/project/main.go"
	if got != want {
		t.Errorf("nodeDisplay(file)\n  got:  %q\n  want: %q", got, want)
	}
}

func TestNodeDisplay_NonFileNoURI(t *testing.T) {
	node := &graph.Node{
		Type: types.TypeBranch,
		Key:  "main",
		Name: "main",
	}

	got := nodeDisplay(node)
	if got != "main" {
		t.Errorf("nodeDisplay(branch) = %q, want %q", got, "main")
	}
}

func TestGetNodeSummary_Commit(t *testing.T) {
	tests := []struct {
		name string
		node *graph.Node
		want string
	}{
		{
			name: "map path with sha and message",
			node: &graph.Node{
				Type: types.TypeCommit,
				Data: map[string]any{
					"sha":     "ee48448eabcdef01",
					"message": "fix: prevent agent loss",
				},
			},
			want: "ee48448e -- fix: prevent agent loss (vcs:commit)",
		},
		{
			name: "map path sha only",
			node: &graph.Node{
				Type: types.TypeCommit,
				Data: map[string]any{
					"sha": "ee48448eabcdef01",
				},
			},
			want: "ee48448e (vcs:commit)",
		},
		{
			name: "CommitData typed path",
			node: &graph.Node{
				Type: types.TypeCommit,
				Data: types.CommitData{
					SHA:     "ee48448eabcdef01",
					Message: "fix: prevent agent loss",
				},
			},
			want: "ee48448e -- fix: prevent agent loss (vcs:commit)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getNodeSummary(tt.node)
			if got != tt.want {
				t.Errorf("getNodeSummary()\n  got:  %q\n  want: %q", got, tt.want)
			}
		})
	}
}
