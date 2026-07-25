package control

import (
	"context"
	"slices"
	"strconv"
	"sync"
	"time"
)

const DefaultEventLimit = 200

type Event struct {
	ID        string    `json:"id"`
	Time      string    `json:"time"`
	Action    string    `json:"action"`
	Success   bool      `json:"success"`
	ErrorKind ErrorKind `json:"error_kind,omitempty"`
	Duration  string    `json:"duration,omitempty"`
}

type EventStore struct {
	mu     sync.Mutex
	events []Event
	limit  int
	subs   map[chan Event]struct{}
	nextID uint64
}

func NewEventStore(limit int) *EventStore {
	if limit <= 0 {
		limit = DefaultEventLimit
	}
	return &EventStore{limit: limit, subs: map[chan Event]struct{}{}}
}

func (s *EventStore) Record(_ context.Context, event AuditEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	recorded := Event{ID: strconv.FormatUint(s.nextID, 10), Time: time.Now().UTC().Format(time.RFC3339), Action: event.Action, Success: event.Success, ErrorKind: event.ErrorKind, Duration: event.Duration.String()}
	s.events = append(s.events, recorded)
	if len(s.events) > s.limit {
		s.events[0] = Event{}
		s.events = s.events[1:]
	}
	for ch := range s.subs {
		select {
		case ch <- recorded:
		default:
		}
	}
}

func (s *EventStore) List() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := slices.Clone(s.events)
	slices.Reverse(out)
	return out
}

func (s *EventStore) SubscribeAndList() (<-chan Event, []Event, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.subs == nil {
		s.subs = map[chan Event]struct{}{}
	}
	ch := make(chan Event, 100)
	s.subs[ch] = struct{}{}
	backlog := make([]Event, len(s.events))
	copy(backlog, s.events)
	return ch, backlog, func() {
		s.mu.Lock()
		if _, ok := s.subs[ch]; ok {
			delete(s.subs, ch)
			close(ch)
		}
		s.mu.Unlock()
	}
}

type MultiAuditSink []AuditSink

func (s MultiAuditSink) Record(ctx context.Context, event AuditEvent) {
	for _, sink := range s {
		if sink != nil {
			sink.Record(ctx, event)
		}
	}
}
