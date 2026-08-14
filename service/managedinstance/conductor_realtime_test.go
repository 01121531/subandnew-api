package managedinstance

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestConsumeConductorSSESupportsLargeSingleLine(t *testing.T) {
	padding := strings.Repeat("x", 96*1024)
	input := "event: account\ndata: {\"type\":\"account\",\"data\":{\"kind\":\"snapshot\",\"ignored\":\"" + padding + "\",\"accounts\":[]}}\n\n"
	var payloads [][]byte
	err := consumeConductorSSE(strings.NewReader(input), func(payload []byte) {
		payloads = append(payloads, append([]byte(nil), payload...))
	})
	if !errors.Is(err, io.EOF) {
		t.Fatalf("consumeConductorSSE() error = %v, want EOF", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("payload count = %d, want 1", len(payloads))
	}
	if !strings.Contains(string(payloads[0]), padding) {
		t.Fatal("large SSE payload was truncated")
	}
}

func TestConsumeConductorSSERejectsOversizedEvent(t *testing.T) {
	input := "data: " + strings.Repeat("x", 128) + "\n\n"
	err := consumeConductorSSEWithLimit(strings.NewReader(input), 64, func([]byte) {})
	if !errors.Is(err, ErrConnectorResponseLarge) {
		t.Fatalf("consumeConductorSSEWithLimit() error = %v, want ErrConnectorResponseLarge", err)
	}
}

func TestConductorRealtimeAppliesSnapshotDeltaAndRemoval(t *testing.T) {
	stream := newConductorRealtimeTestStream(42)
	stream.sources["1"] = InventorySource{ID: "1", Name: "worker-a", Status: "Connected"}

	stream.applyEvent([]byte(`{
		"type":"account",
		"data":{"kind":"snapshot","accounts":[
			{"account_id":"1","source":"1","label":"one","available":false,"rpm_current":10,"active_session_count":3},
			{"account_id":"2","source":"1","label":"two","available":true,"rpm_current":0,"active_session_count":2},
			{"account_id":"3","source":"1","label":"three","available":true,"rpm_current":-2},
			{"account_id":"4","source":"1","label":"four","available":true}
		]}}
	`))
	state := conductorRealtimeTestState(stream)
	if state.AccountsTotal != 4 || state.AccountsReporting != 2 {
		t.Fatalf("snapshot counts = total %d, reporting %d; want 4, 2", state.AccountsTotal, state.AccountsReporting)
	}
	if state.RPM.Value == nil || *state.RPM.Value != 10 {
		t.Fatalf("snapshot RPM = %v, want 10", state.RPM.Value)
	}
	if state.ActiveSessions != 5 {
		t.Fatalf("active sessions = %d, want 5", state.ActiveSessions)
	}
	if state.Accounts[0].Platform != "worker-a" {
		t.Fatalf("source mapping = %q, want worker-a", state.Accounts[0].Platform)
	}

	stream.applyEvent([]byte(`{"type":"account","data":{"account_id":"1","source":"1","label":"one","available":false,"rpm_current":14,"active_session_count":4}}`))
	state = conductorRealtimeTestState(stream)
	if state.RPM.Value == nil || *state.RPM.Value != 14 {
		t.Fatalf("delta RPM = %v, want 14", state.RPM.Value)
	}

	stream.applyEvent([]byte(`{"type":"account","data":{"account":{"account_id":"2","_removed":true}}}`))
	state = conductorRealtimeTestState(stream)
	if state.AccountsTotal != 3 || state.AccountsReporting != 1 {
		t.Fatalf("removal counts = total %d, reporting %d; want 3, 1", state.AccountsTotal, state.AccountsReporting)
	}
}

func TestConductorRealtimeHubSharesInstanceStream(t *testing.T) {
	hub := &conductorRealtimeHub{streams: map[int64]*conductorRealtimeStream{}}
	stream := hub.stream(7, true)
	stream.mu.Lock()
	stream.observedAt = 1
	stream.running = true
	stream.mu.Unlock()

	_, unsubscribeOne, err := hub.subscribe(7)
	if err != nil {
		t.Fatalf("first subscribe: %v", err)
	}
	_, unsubscribeTwo, err := hub.subscribe(7)
	if err != nil {
		t.Fatalf("second subscribe: %v", err)
	}
	if got := len(hub.streams); got != 1 {
		t.Fatalf("stream count = %d, want 1", got)
	}
	stream.mu.Lock()
	if got := len(stream.subscribers); got != 2 {
		stream.mu.Unlock()
		t.Fatalf("subscriber count = %d, want 2", got)
	}
	stream.mu.Unlock()

	unsubscribeOne()
	unsubscribeTwo()
	stream.mu.Lock()
	if stream.closeTimer != nil {
		stream.closeTimer.Stop()
		stream.closeTimer = nil
	}
	stream.running = false
	stream.mu.Unlock()
}

func newConductorRealtimeTestStream(instanceID int64) *conductorRealtimeStream {
	return &conductorRealtimeStream{
		instanceID:  instanceID,
		accounts:    map[int64]InventoryItem{},
		sources:     map[string]InventorySource{},
		status:      "connecting",
		stale:       true,
		subscribers: map[uint64]*conductorRealtimeSubscriber{},
	}
}

func conductorRealtimeTestState(stream *conductorRealtimeStream) ConductorRealtimeState {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	return stream.snapshotLocked()
}
