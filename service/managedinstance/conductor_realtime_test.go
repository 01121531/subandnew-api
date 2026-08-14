package managedinstance

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"
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

func TestConsumeConductorSSEDecodedAppliesAccountsBeforeLargeIgnoredFieldsFinish(t *testing.T) {
	reader, writer := io.Pipe()
	applied := make(chan conductorRealtimeEnvelope, 1)
	finished := make(chan error, 1)
	go func() {
		finished <- consumeConductorSSEDecoded(reader, func(envelope conductorRealtimeEnvelope) {
			applied <- envelope
		})
	}()
	go func() {
		_, _ = io.WriteString(writer, `data: {"type":"account","data":{"kind":"snapshot","accounts":[{"account_id":"1","rpm_current":7}],"session_remaps":"`+strings.Repeat("x", 70*1024))
	}()

	select {
	case envelope := <-applied:
		if len(envelope.Data.Accounts) != 1 || envelope.Data.Accounts[0].RPMCurrent == nil || *envelope.Data.Accounts[0].RPMCurrent != 7 {
			t.Fatalf("decoded accounts = %#v", envelope.Data.Accounts)
		}
	case <-time.After(time.Second):
		t.Fatal("account snapshot waited for ignored fields to finish")
	}
	_ = writer.Close()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("stream decoder did not stop after the source closed")
	}
}

func TestDecodeConductorAccountInventoryStopsBeforeLargeIgnoredFields(t *testing.T) {
	reader := &countingReader{Reader: strings.NewReader(
		`{"code":200,"data":{"accounts":[{"account_id":"1","label":"one","rpm_current":3}],"total":1,"session_remaps":"` + strings.Repeat("x", 2<<20) + `"}}`,
	)}
	accounts, total, err := decodeConductorAccountInventory(reader, 200)
	if err != nil {
		t.Fatalf("decodeConductorAccountInventory() error = %v", err)
	}
	if total != 1 || len(accounts) != 1 || accounts[0].AccountID != "1" {
		t.Fatalf("decoded inventory = total %d, accounts %#v", total, accounts)
	}
	if reader.readBytes >= 64*1024 {
		t.Fatalf("decoder read %d bytes, expected it to stop before the large ignored field", reader.readBytes)
	}
}

type countingReader struct {
	io.Reader
	readBytes int
}

func (reader *countingReader) Read(target []byte) (int, error) {
	count, err := reader.Reader.Read(target)
	reader.readBytes += count
	return count, err
}

func TestConductorRealtimeAppliesSnapshotDeltaAndRemoval(t *testing.T) {
	stream := newConductorRealtimeTestStream(42)
	stream.sources["1"] = InventorySource{ID: "1", Name: "worker-a", Status: "Connected"}
	capacityPerAccount := 400.0
	stream.rpmPerAccount = &capacityPerAccount

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
	if state.AccountsTotal != 4 || state.AccountsAvailable != 3 || state.AccountsReporting != 2 {
		t.Fatalf("snapshot counts = total %d, available %d, reporting %d; want 4, 3, 2", state.AccountsTotal, state.AccountsAvailable, state.AccountsReporting)
	}
	if state.RPM.Value == nil || *state.RPM.Value != 10 {
		t.Fatalf("snapshot RPM = %v, want 10", state.RPM.Value)
	}
	if state.RPMCapacity.Value == nil || *state.RPMCapacity.Value != 1200 {
		t.Fatalf("snapshot RPM capacity = %v, want 1200", state.RPMCapacity.Value)
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
	if state.AccountsTotal != 3 || state.AccountsAvailable != 2 || state.AccountsReporting != 1 {
		t.Fatalf("removal counts = total %d, available %d, reporting %d; want 3, 2, 1", state.AccountsTotal, state.AccountsAvailable, state.AccountsReporting)
	}
	if state.RPMCapacity.Value == nil || *state.RPMCapacity.Value != 800 {
		t.Fatalf("RPM capacity after removal = %v, want 800", state.RPMCapacity.Value)
	}
}

func TestConductorRPMCapacityFromQuota(t *testing.T) {
	capacity, ok := conductorRPMCapacityFromData([]byte(`{"per_account":{"min_interval_ms":400}}`))
	if !ok || capacity != 400 {
		t.Fatalf("capacity = %v, ok = %v; want 400, true", capacity, ok)
	}
	for _, payload := range []string{
		`{"per_account":{"min_interval_ms":0}}`,
		`{"per_account":{}}`,
		`not-json`,
	} {
		if value, valid := conductorRPMCapacityFromData([]byte(payload)); valid {
			t.Fatalf("invalid payload %q returned capacity %v", payload, value)
		}
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
