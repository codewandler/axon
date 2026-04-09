package main

import (
	"sort"

	"github.com/codewandler/axon/graph"
)

// CountItem is an alias for graph.CountItem for CLI use.
type CountItem = graph.CountItem

// CountResult represents aggregated counts (for labels, types, edges).
type CountResult struct {
	Items []CountItem `json:"items"`
}

// FromMap creates a CountResult from a map of counts.
// Note: map iteration order is non-deterministic; prefer FromSlice when order matters.
func (r *CountResult) FromMap(m map[string]int) {
	r.Items = make([]CountItem, 0, len(m))
	for name, count := range m {
		r.Items = append(r.Items, CountItem{Name: name, Count: count})
	}
}

// FromSlice creates a CountResult from an ordered slice, preserving SQLite result order.
func (r *CountResult) FromSlice(items []graph.CountItem) {
	r.Items = items
}

// SortByCount sorts items by count descending, then by name ascending.
func (r *CountResult) SortByCount() {
	sort.Slice(r.Items, func(i, j int) bool {
		if r.Items[i].Count != r.Items[j].Count {
			return r.Items[i].Count > r.Items[j].Count
		}
		return r.Items[i].Name < r.Items[j].Name
	})
}

// SortByName sorts items by name ascending.
func (r *CountResult) SortByName() {
	sort.Slice(r.Items, func(i, j int) bool {
		return r.Items[i].Name < r.Items[j].Name
	})
}

// FilterByPrefix filters items to those with names starting with prefix.
func (r *CountResult) FilterByPrefix(prefix string) {
	if prefix == "" {
		return
	}
	filtered := make([]CountItem, 0)
	for _, item := range r.Items {
		if len(item.Name) >= len(prefix) && item.Name[:len(prefix)] == prefix {
			filtered = append(filtered, item)
		}
	}
	r.Items = filtered
}

// Total returns the sum of all counts.
func (r *CountResult) Total() int {
	total := 0
	for _, item := range r.Items {
		total += item.Count
	}
	return total
}
