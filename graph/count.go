package graph

// CountItem represents a single aggregated count result (key + count).
// Used in QueryResult.Counts for GROUP BY queries, preserving SQLite result order.
type CountItem struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}
