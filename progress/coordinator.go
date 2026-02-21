// Package progress provides progress reporting for indexing operations.
//
// The Coordinator collects progress events from indexers and aggregates them
// for display. It tracks per-indexer state (running, done, errored) and counts
// of processed items.
//
// The UI component (ui.go) provides a bubbletea-based terminal UI that displays
// real-time progress with spinners, counters, and ETA estimates.
//
// Usage:
//
//	coord := progress.NewCoordinator()
//	// Pass coord.Events() to indexers
//	// Run bubbletea with progress.NewModel(coord)
//	// Call coord.Close() when indexing completes
package progress

import (
	"sync"
	"time"
)

// IndexerState tracks the current state of an indexer.
type IndexerState struct {
	Name      string
	Status    string // "running", "completed", "error"
	Current   int
	Total     int
	Item      string
	Error     error
	StartedAt time.Time
	EndedAt   time.Time

	// Rate tracking (sliding window of samples)
	rateSamples []rateSample
}

// rateSample records a count at a point in time for rate calculation.
type rateSample struct {
	count int
	time  time.Time
}

// maxRateSamples is the number of samples to keep for rate smoothing.
const maxRateSamples = 5

// Elapsed returns the duration since the indexer started.
// If completed/errored, returns the total duration.
func (s *IndexerState) Elapsed() time.Duration {
	if s.StartedAt.IsZero() {
		return 0
	}
	if s.Status == "running" {
		return time.Since(s.StartedAt)
	}
	if s.EndedAt.IsZero() {
		return time.Since(s.StartedAt)
	}
	return s.EndedAt.Sub(s.StartedAt)
}

// Rate returns the current items/second rate (smoothed over recent samples).
func (s *IndexerState) Rate() float64 {
	if len(s.rateSamples) < 2 {
		// Not enough samples - use overall rate
		elapsed := s.Elapsed().Seconds()
		if elapsed <= 0 {
			return 0
		}
		return float64(s.Current) / elapsed
	}

	// Use oldest and newest samples for rate calculation
	oldest := s.rateSamples[0]
	newest := s.rateSamples[len(s.rateSamples)-1]

	duration := newest.time.Sub(oldest.time).Seconds()
	if duration <= 0 {
		return 0
	}

	items := newest.count - oldest.count
	return float64(items) / duration
}

// ETA returns the estimated time remaining based on current rate.
// Returns 0 if total is unknown or rate is too low.
func (s *IndexerState) ETA() time.Duration {
	if s.Total <= 0 || s.Status != "running" {
		return 0
	}

	rate := s.Rate()
	if rate <= 0 {
		return 0
	}

	remaining := s.Total - s.Current
	if remaining <= 0 {
		return 0
	}

	return time.Duration(float64(remaining)/rate) * time.Second
}

// addRateSample adds a new sample for rate calculation.
func (s *IndexerState) addRateSample(count int, t time.Time) {
	s.rateSamples = append(s.rateSamples, rateSample{count: count, time: t})

	// Keep only the last maxRateSamples samples
	if len(s.rateSamples) > maxRateSamples {
		s.rateSamples = s.rateSamples[len(s.rateSamples)-maxRateSamples:]
	}
}

// IndexerSummary contains final statistics for an indexer.
type IndexerSummary struct {
	Name     string
	Status   string // "completed", "error"
	Duration time.Duration
	Items    int
	Rate     float64
	Error    error
}

// Coordinator collects progress events from indexers and tracks their state.
type Coordinator struct {
	events   chan Event
	states   map[string]*IndexerState
	order    []string // Track order of first appearance
	mu       sync.RWMutex
	done     chan struct{}
	allDone  chan struct{}
	errors   []IndexerError
	errorsMu sync.Mutex
}

// IndexerError records an error from a specific indexer.
type IndexerError struct {
	Indexer string
	Err     error
}

// NewCoordinator creates a new progress coordinator.
func NewCoordinator() *Coordinator {
	c := &Coordinator{
		events:  make(chan Event, 100),
		states:  make(map[string]*IndexerState),
		done:    make(chan struct{}),
		allDone: make(chan struct{}),
	}
	go c.run()
	return c
}

// Events returns the channel to send events to.
func (c *Coordinator) Events() chan<- Event {
	return c.events
}

// State returns a copy of the current indexer states in order of first appearance.
func (c *Coordinator) State() []*IndexerState {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]*IndexerState, 0, len(c.order))
	for _, name := range c.order {
		if state, ok := c.states[name]; ok {
			stateCopy := *state
			result = append(result, &stateCopy)
		}
	}
	return result
}

// Close stops the coordinator's event processing.
func (c *Coordinator) Close() {
	// Signal we're done
	close(c.done)

	// Wait for run() to finish processing
	<-c.allDone
}

// Errors returns all errors collected from indexers.
func (c *Coordinator) Errors() []IndexerError {
	c.errorsMu.Lock()
	defer c.errorsMu.Unlock()
	return c.errors
}

// IsRunning returns true if any indexer is still running.
func (c *Coordinator) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, state := range c.states {
		if state.Status == "running" {
			return true
		}
	}
	return false
}

// Summary returns final statistics for all indexers in order of appearance.
func (c *Coordinator) Summary() []IndexerSummary {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]IndexerSummary, 0, len(c.order))
	for _, name := range c.order {
		state, ok := c.states[name]
		if !ok {
			continue
		}

		summary := IndexerSummary{
			Name:     state.Name,
			Status:   state.Status,
			Duration: state.Elapsed(),
			Items:    state.Total,
			Error:    state.Error,
		}

		// Calculate final rate
		if summary.Duration > 0 && summary.Items > 0 {
			summary.Rate = float64(summary.Items) / summary.Duration.Seconds()
		}

		result = append(result, summary)
	}

	return result
}

// run processes events from indexers.
func (c *Coordinator) run() {
	defer close(c.allDone)

	for {
		select {
		case event, ok := <-c.events:
			if !ok {
				return
			}
			c.processEvent(event)

		case <-c.done:
			// Drain remaining events
			for {
				select {
				case event := <-c.events:
					c.processEvent(event)
				default:
					return
				}
			}
		}
	}
}

func (c *Coordinator) processEvent(event Event) {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, exists := c.states[event.Indexer]
	if !exists {
		state = &IndexerState{
			Name:      event.Indexer,
			Status:    "running",
			StartedAt: event.Timestamp,
		}
		c.states[event.Indexer] = state
		c.order = append(c.order, event.Indexer) // Track order of first appearance
	}

	switch event.Type {
	case EventStarted:
		state.Status = "running"
		state.StartedAt = event.Timestamp

	case EventProgress:
		state.Current = event.Current
		if event.Total > 0 {
			state.Total = event.Total
		}
		state.Item = event.Item
		state.addRateSample(event.Current, event.Timestamp)

	case EventCompleted:
		state.Status = "completed"
		state.Current = event.Current
		state.Total = event.Total
		state.Item = ""
		state.EndedAt = event.Timestamp

	case EventError:
		state.Status = "error"
		state.Error = event.Error
		state.EndedAt = event.Timestamp

		c.errorsMu.Lock()
		c.errors = append(c.errors, IndexerError{
			Indexer: event.Indexer,
			Err:     event.Error,
		})
		c.errorsMu.Unlock()
	}
}
