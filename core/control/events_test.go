package control

import (
	"context"
	"testing"
	"time"
)

func TestEventStoreKeepsNewestBoundedEvents(t *testing.T) {
	store := NewEventStore(2)
	store.Record(context.Background(), AuditEvent{Action: "one", Success: true, Duration: time.Millisecond})
	store.Record(context.Background(), AuditEvent{Action: "two", Success: false, ErrorKind: ErrorConflict})
	store.Record(context.Background(), AuditEvent{Action: "three", Success: true})

	events := store.List()
	if len(events) != 2 || events[0].Action != "three" || events[1].Action != "two" {
		t.Fatalf("events = %#v", events)
	}
}

func TestEventStorePublishesRecordedEvents(t *testing.T) {
	store := NewEventStore(2)
	events, _, unsubscribe := store.SubscribeAndList()
	defer unsubscribe()

	store.Record(context.Background(), AuditEvent{Action: "start", Success: true})

	select {
	case event := <-events:
		if event.Action != "start" || !event.Success {
			t.Fatalf("event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("expected event")
	}
}
