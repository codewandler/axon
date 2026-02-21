// Package progress provides event types and coordination for tracking indexer progress.
package progress

import "time"

// EventType represents the type of progress event.
type EventType int

const (
	// EventStarted indicates an indexer has started.
	EventStarted EventType = iota
	// EventProgress indicates progress has been made.
	EventProgress
	// EventCompleted indicates an indexer has finished successfully.
	EventCompleted
	// EventError indicates an indexer encountered an error.
	EventError
)

// Event represents a progress update from an indexer.
type Event struct {
	// Indexer is the name of the indexer (e.g., "fs", "git").
	Indexer string

	// Type is the event type.
	Type EventType

	// Current is the number of items processed so far.
	Current int

	// Total is the total number of items to process (0 if unknown).
	Total int

	// Item is the current item being processed (optional, for display).
	Item string

	// Error is set when Type is EventError.
	Error error

	// Timestamp is when the event occurred.
	Timestamp time.Time
}

// NewEvent creates a new progress event with the current timestamp.
func NewEvent(indexer string, eventType EventType) Event {
	return Event{
		Indexer:   indexer,
		Type:      eventType,
		Timestamp: time.Now(),
	}
}

// Started creates a started event.
func Started(indexer string) Event {
	return NewEvent(indexer, EventStarted)
}

// Progress creates a progress event.
func Progress(indexer string, current int, item string) Event {
	e := NewEvent(indexer, EventProgress)
	e.Current = current
	e.Item = item
	return e
}

// ProgressWithTotal creates a progress event with a known total.
func ProgressWithTotal(indexer string, current, total int, item string) Event {
	e := NewEvent(indexer, EventProgress)
	e.Current = current
	e.Total = total
	e.Item = item
	return e
}

// Completed creates a completed event.
func Completed(indexer string, total int) Event {
	e := NewEvent(indexer, EventCompleted)
	e.Current = total
	e.Total = total
	return e
}

// Error creates an error event.
func Error(indexer string, err error) Event {
	e := NewEvent(indexer, EventError)
	e.Error = err
	return e
}
