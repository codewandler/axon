package types

import (
	"testing"
	"time"
)

func TestCommitData_Description(t *testing.T) {
	date := time.Date(2024, 12, 15, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		d    CommitData
		want string
	}{
		{
			name: "full commit",
			d: CommitData{
				SHA:          "ee48448eabcdef01",
				Message:      "fix: prevent agent loss on validation failure",
				AuthorName:   "timo",
				AuthorDate:   date,
				FilesChanged: 3,
			},
			want: "ee48448e -- fix: prevent agent loss on validation failure  by timo (2024-12-15, 3 files)",
		},
		{
			name: "no subject",
			d: CommitData{
				SHA:          "ee48448eabcdef01",
				Message:      "",
				AuthorName:   "timo",
				AuthorDate:   date,
				FilesChanged: 1,
			},
			want: "ee48448e  by timo (2024-12-15, 1 files)",
		},
		{
			name: "no author",
			d: CommitData{
				SHA:          "ee48448eabcdef01",
				Message:      "fix something",
				AuthorName:   "",
				AuthorDate:   date,
				FilesChanged: 2,
			},
			want: "ee48448e -- fix something (2024-12-15, 2 files)",
		},
		{
			name: "no date no files",
			d: CommitData{
				SHA:        "ee48448eabcdef01",
				Message:    "fix something",
				AuthorName: "timo",
				AuthorDate: time.Time{},
			},
			want: "ee48448e -- fix something  by timo",
		},
		{
			name: "empty commit",
			d: CommitData{
				SHA: "ee48448eabcdef01",
			},
			want: "ee48448e",
		},
		{
			name: "short SHA handled gracefully",
			d: CommitData{
				SHA:     "abc",
				Message: "something",
			},
			want: "abc -- something",
		},
		{
			name: "zero files not shown",
			d: CommitData{
				SHA:          "ee48448eabcdef01",
				Message:      "chore: tidy",
				AuthorDate:   date,
				FilesChanged: 0,
			},
			want: "ee48448e -- chore: tidy (2024-12-15)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.d.Description()
			if got != tt.want {
				t.Errorf("Description() =\n  %q\nwant:\n  %q", got, tt.want)
			}
		})
	}
}
