# Plan: Dispatcher Backpressure Fix (issue #20)

**Design**: Replace non-blocking send in the event dispatcher with a blocking send
(`select { case ch <- event: / case <-ctx.Done(): }`). Removes the `default: drop`
arm entirely. Provides correct backpressure — slow subscribers cause the FS walk to
slow down rather than losing events.

---

## Task 1 — Write failing test

**File modified**: `axon_test.go`

Add `TestDispatcher_NoEventsDroppedWhenSubscriberSlow` in `package axon`.

- Change `eventChannelBuffer` from `const` to `var` so tests can override it.
- In the test: set `eventChannelBuffer = 5`, create 20 Go source files (each with a
  `// TODO: item N` comment), register a custom slow indexer that sleeps 2 ms per event
  and counts received events.
- Index the directory; assert received count == 20.

**Verification**:
```
go test -run TestDispatcher_NoEventsDroppedWhenSubscriberSlow ./...
```
→ must FAIL (events dropped) before the fix.

---

## Task 2 — Fix the dispatcher

**File modified**: `axon.go`

1. Change `const eventChannelBuffer = 10000` → `var eventChannelBuffer = 10000`
2. In the dispatcher goroutine, replace:
   ```go
   select {
   case info.eventCh <- eventCopy:
   case <-ctx.Done():
       return
   default:
       log.Printf("axon: dispatcher: subscriber %s channel full, dropping event …")
   }
   ```
   with:
   ```go
   select {
   case info.eventCh <- eventCopy:
   case <-ctx.Done():
       return
   }
   ```

**Verification**:
```
go test -run TestDispatcher_NoEventsDroppedWhenSubscriberSlow ./...
```
→ must PASS after the fix.

---

## Task 3 — Full test suite

```
go test ./...
go vet ./...
```

→ all green.

---

**Estimated time**: ~15 minutes total
