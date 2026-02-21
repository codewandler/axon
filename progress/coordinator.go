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
