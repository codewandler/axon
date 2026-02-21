package main

import "sort"

// CountResult represents aggregated counts (for labels, types, edges).
type CountResult struct {
	Items []CountItem `json:"items"`
}

// CountItem represents a single count entry.
type CountItem struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// FromMap creates a CountResult from a map of counts.
func (r *CountResult) FromMap(m map[string]int) {
	r.Items = make([]CountItem, 0, len(m))
	for name, count := range m {
		r.Items = append(r.Items, CountItem{Name: name, Count: count})
	}
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
