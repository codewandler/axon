package commitparser

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    ParsedCommit
	}{
		{
			name:    "full conventional commit with scope",
			message: "feat(aql): add pattern matching",
			want: ParsedCommit{
				CommitType: "feat",
				Scope:      "aql",
				Breaking:   false,
				Subject:    "add pattern matching",
				Body:       "",
				Footers:    map[string][]string{},
				Refs:       nil,
			},
		},
		{
			name:    "fix without scope",
			message: "fix: handle nil pointer in walker",
			want: ParsedCommit{
				CommitType: "fix",
				Scope:      "",
				Breaking:   false,
				Subject:    "handle nil pointer in walker",
				Body:       "",
				Footers:    map[string][]string{},
				Refs:       nil,
			},
		},
		{
			name:    "breaking change via exclamation mark",
			message: "feat(api)!: remove deprecated endpoint",
			want: ParsedCommit{
				CommitType: "feat",
				Scope:      "api",
				Breaking:   true,
				Subject:    "remove deprecated endpoint",
				Body:       "",
				Footers:    map[string][]string{},
				Refs:       nil,
			},
		},
		{
			name:    "breaking change without scope",
			message: "refactor!: rename Storage interface",
			want: ParsedCommit{
				CommitType: "refactor",
				Scope:      "",
				Breaking:   true,
				Subject:    "rename Storage interface",
				Body:       "",
				Footers:    map[string][]string{},
				Refs:       nil,
			},
		},
		{
			name: "conventional commit with body and Refs footer",
			message: "feat(parser): add semantic commit parsing\n\nThis enables structured AQL queries on commit data.\n\nRefs: #8, DEV-100",
			want: ParsedCommit{
				CommitType: "feat",
				Scope:      "parser",
				Breaking:   false,
				Subject:    "add semantic commit parsing",
				Body:       "This enables structured AQL queries on commit data.",
				Footers:    map[string][]string{"Refs": {"#8, DEV-100"}},
				Refs:       []string{"#8", "DEV-100"},
			},
		},
		{
			name: "BREAKING CHANGE in footer sets Breaking=true",
			message: "feat: overhaul storage API\n\nComplete rewrite of the storage layer.\n\nBREAKING CHANGE: Storage.Get now returns (Node, error) instead of *Node",
			want: ParsedCommit{
				CommitType: "feat",
				Scope:      "",
				Breaking:   true,
				Subject:    "overhaul storage API",
				Body:       "Complete rewrite of the storage layer.",
				Footers:    map[string][]string{"BREAKING CHANGE": {"Storage.Get now returns (Node, error) instead of *Node"}},
				Refs:       nil,
			},
		},
		{
			name:    "non-conventional commit (lenient fallback)",
			message: "Initial commit",
			want: ParsedCommit{
				CommitType: "",
				Scope:      "",
				Breaking:   false,
				Subject:    "Initial commit",
				Body:       "",
				Footers:    map[string][]string{},
				Refs:       nil,
			},
		},
		{
			name:    "non-conventional multi-word first line",
			message: "Fix the thing that was broken",
			want: ParsedCommit{
				CommitType: "",
				Scope:      "",
				Breaking:   false,
				Subject:    "Fix the thing that was broken",
				Body:       "",
				Footers:    map[string][]string{},
				Refs:       nil,
			},
		},
		{
			name:    "empty message",
			message: "",
			want: ParsedCommit{
				CommitType: "",
				Scope:      "",
				Breaking:   false,
				Subject:    "",
				Body:       "",
				Footers:    map[string][]string{},
				Refs:       nil,
			},
		},
		{
			name:    "whitespace only message",
			message: "   \n\n   ",
			want: ParsedCommit{
				CommitType: "",
				Scope:      "",
				Breaking:   false,
				Subject:    "",
				Body:       "",
				Footers:    map[string][]string{},
				Refs:       nil,
			},
		},
		{
			name: "multiple trailers including Co-authored-by",
			message: "fix(sqlite): prevent duplicate nodes\n\nAdds a unique constraint and proper upsert logic.\n\nCo-authored-by: Alice <alice@example.com>\nRefs: DEV-42\nCloses: #7",
			want: ParsedCommit{
				CommitType: "fix",
				Scope:      "sqlite",
				Breaking:   false,
				Subject:    "prevent duplicate nodes",
				Body:       "Adds a unique constraint and proper upsert logic.",
				Footers: map[string][]string{
					"Co-authored-by": {"Alice <alice@example.com>"},
					"Refs":           {"DEV-42"},
					"Closes":         {"#7"},
				},
				Refs: []string{"DEV-42"},
			},
		},
		{
			name: "Refs with comma-separated values",
			message: "chore: update dependencies\n\nRefs: DEV-1, DEV-2, DEV-3",
			want: ParsedCommit{
				CommitType: "chore",
				Scope:      "",
				Breaking:   false,
				Subject:    "update dependencies",
				Body:       "",
				Footers:    map[string][]string{"Refs": {"DEV-1, DEV-2, DEV-3"}},
				Refs:       []string{"DEV-1", "DEV-2", "DEV-3"},
			},
		},
		{
			name: "Refs with hash prefixes",
			message: "fix: close open file handles\n\nRefs: #42, #99",
			want: ParsedCommit{
				CommitType: "fix",
				Scope:      "",
				Breaking:   false,
				Subject:    "close open file handles",
				Body:       "",
				Footers:    map[string][]string{"Refs": {"#42, #99"}},
				Refs:       []string{"#42", "#99"},
			},
		},
		{
			name: "duplicate Refs are deduplicated",
			message: "fix: dedup\n\nRefs: DEV-1, DEV-1, DEV-2",
			want: ParsedCommit{
				CommitType: "fix",
				Scope:      "",
				Breaking:   false,
				Subject:    "dedup",
				Body:       "",
				Footers:    map[string][]string{"Refs": {"DEV-1, DEV-1, DEV-2"}},
				Refs:       []string{"DEV-1", "DEV-2"},
			},
		},
		{
			name: "body without footers",
			message: "docs: update README\n\nAdded AQL tutorial section.\nImproved quickstart guide.",
			want: ParsedCommit{
				CommitType: "docs",
				Scope:      "",
				Breaking:   false,
				Subject:    "update README",
				Body:       "Added AQL tutorial section.\nImproved quickstart guide.",
				Footers:    map[string][]string{},
				Refs:       nil,
			},
		},
		{
			name: "chore with parenthesised scope containing hyphen",
			message: "chore(go-git): upgrade to v6",
			want: ParsedCommit{
				CommitType: "chore",
				Scope:      "go-git",
				Breaking:   false,
				Subject:    "upgrade to v6",
				Body:       "",
				Footers:    map[string][]string{},
				Refs:       nil,
			},
		},
		{
			name: "non-conventional with body",
			message: "Merge branch 'main' into feature/xyz\n\nResolved conflicts in aql/parser.go",
			want: ParsedCommit{
				CommitType: "",
				Scope:      "",
				Breaking:   false,
				Subject:    "Merge branch 'main' into feature/xyz",
				Body:       "Resolved conflicts in aql/parser.go",
				Footers:    map[string][]string{},
				Refs:       nil,
			},
		},
		{
			name: "BREAKING CHANGE with exclamation also sets Breaking",
			message: "feat(aql)!: replace query DSL\n\nNew DSL is incompatible with old queries.\n\nBREAKING CHANGE: old queries will fail",
			want: ParsedCommit{
				CommitType: "feat",
				Scope:      "aql",
				Breaking:   true,
				Subject:    "replace query DSL",
				Body:       "New DSL is incompatible with old queries.",
				Footers:    map[string][]string{"BREAKING CHANGE": {"old queries will fail"}},
				Refs:       nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(tt.message)
			if got.CommitType != tt.want.CommitType {
				t.Errorf("CommitType = %q, want %q", got.CommitType, tt.want.CommitType)
			}
			if got.Scope != tt.want.Scope {
				t.Errorf("Scope = %q, want %q", got.Scope, tt.want.Scope)
			}
			if got.Breaking != tt.want.Breaking {
				t.Errorf("Breaking = %v, want %v", got.Breaking, tt.want.Breaking)
			}
			if got.Subject != tt.want.Subject {
				t.Errorf("Subject = %q, want %q", got.Subject, tt.want.Subject)
			}
			if got.Body != tt.want.Body {
				t.Errorf("Body = %q, want %q", got.Body, tt.want.Body)
			}
			if !reflect.DeepEqual(got.Footers, tt.want.Footers) {
				t.Errorf("Footers = %v, want %v", got.Footers, tt.want.Footers)
			}
			if !reflect.DeepEqual(got.Refs, tt.want.Refs) {
				t.Errorf("Refs = %v, want %v", got.Refs, tt.want.Refs)
			}
		})
	}
}
