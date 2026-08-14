package managedinstance

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/bytedance/gopkg/util/gopool"
)

const (
	conductorRealtimeEventPath     = "/api/v1/system/events"
	conductorRealtimeEventMaxBytes = 64 << 20
	conductorRealtimeCloseGrace    = time.Minute
	conductorSourceRefreshInterval = time.Minute
)

type ConductorRealtimeState struct {
	InstanceID        int64             `json:"instance_id"`
	ObservedAt        int64             `json:"observed_at"`
	StreamStatus      string            `json:"stream_status"`
	Stale             bool              `json:"stale"`
	ErrorCode         string            `json:"error_code,omitempty"`
	RPM               MetricSample      `json:"rpm"`
	AccountsTotal     int               `json:"accounts_total"`
	AccountsReporting int               `json:"accounts_reporting"`
	ActiveSessions    int               `json:"active_sessions"`
	Accounts          []InventoryItem   `json:"accounts"`
	Sources           []InventorySource `json:"sources"`
}

type ConductorRealtimeEvent struct {
	Type  string                 `json:"type"`
	State ConductorRealtimeState `json:"data"`
}

type conductorRealtimeSubscriber struct {
	id     uint64
	events chan ConductorRealtimeEvent
}

type conductorRealtimeStream struct {
	instanceID int64

	mu          sync.Mutex
	accounts    map[int64]InventoryItem
	sources     map[string]InventorySource
	observedAt  int64
	status      string
	stale       bool
	errorCode   string
	revision    uint64
	running     bool
	cancel      context.CancelFunc
	closeTimer  *time.Timer
	subscribers map[uint64]*conductorRealtimeSubscriber
}

type conductorRealtimeHub struct {
	mu      sync.Mutex
	streams map[int64]*conductorRealtimeStream
	nextID  atomic.Uint64
}

var defaultConductorRealtimeHub = &conductorRealtimeHub{streams: map[int64]*conductorRealtimeStream{}}

func SubscribeConductorRealtime(instanceID int64) (<-chan ConductorRealtimeEvent, func(), error) {
	instance, err := Get(instanceID)
	if err != nil {
		return nil, nil, err
	}
	if instance.Kind != model.ManagedInstanceKindConductor {
		return nil, nil, ErrUnsupportedCapability
	}
	return defaultConductorRealtimeHub.subscribe(instanceID)
}

func CurrentConductorRealtime(instanceID int64) (ConductorRealtimeState, bool) {
	stream := defaultConductorRealtimeHub.stream(instanceID, false)
	if stream == nil {
		stream = defaultConductorRealtimeHub.stream(instanceID, true)
		stream.bootstrap()
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	state := stream.snapshotLocked()
	return state, state.RPM.Value != nil
}

func (hub *conductorRealtimeHub) stream(instanceID int64, create bool) *conductorRealtimeStream {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	stream := hub.streams[instanceID]
	if stream == nil && create {
		stream = &conductorRealtimeStream{
			instanceID: instanceID,
			accounts:   map[int64]InventoryItem{}, sources: map[string]InventorySource{},
			status: "connecting", stale: true, subscribers: map[uint64]*conductorRealtimeSubscriber{},
		}
		hub.streams[instanceID] = stream
	}
	return stream
}

func (hub *conductorRealtimeHub) subscribe(instanceID int64) (<-chan ConductorRealtimeEvent, func(), error) {
	stream := hub.stream(instanceID, true)
	stream.bootstrap()
	subscriber := &conductorRealtimeSubscriber{id: hub.nextID.Add(1), events: make(chan ConductorRealtimeEvent, 32)}
	stream.mu.Lock()
	if stream.closeTimer != nil {
		stream.closeTimer.Stop()
		stream.closeTimer = nil
	}
	stream.subscribers[subscriber.id] = subscriber
	state := stream.snapshotLocked()
	if !stream.running {
		ctx, cancel := context.WithCancel(context.Background())
		stream.cancel = cancel
		stream.running = true
		gopool.Go(func() { stream.run(ctx) })
	}
	stream.mu.Unlock()
	for _, eventType := range []string{"status", "rpm", "accounts", "sources"} {
		subscriber.events <- ConductorRealtimeEvent{Type: eventType, State: state}
	}
	var once sync.Once
	unsubscribe := func() {
		once.Do(func() { stream.unsubscribe(subscriber.id) })
	}
	return subscriber.events, unsubscribe, nil
}

func (stream *conductorRealtimeStream) unsubscribe(id uint64) {
	stream.mu.Lock()
	subscriber := stream.subscribers[id]
	delete(stream.subscribers, id)
	if subscriber != nil {
		close(subscriber.events)
	}
	if len(stream.subscribers) == 0 && stream.running && stream.closeTimer == nil {
		stream.closeTimer = time.AfterFunc(conductorRealtimeCloseGrace, func() {
			stream.mu.Lock()
			defer stream.mu.Unlock()
			if len(stream.subscribers) == 0 && stream.cancel != nil {
				stream.cancel()
			}
			stream.closeTimer = nil
		})
	}
	stream.mu.Unlock()
}

func (stream *conductorRealtimeStream) bootstrap() {
	stream.mu.Lock()
	if len(stream.accounts) != 0 || stream.observedAt != 0 {
		stream.mu.Unlock()
		return
	}
	stream.mu.Unlock()
	if model.DB == nil {
		return
	}
	page := InventoryPage{}
	observedAt := int64(0)
	if model.DB.Migrator().HasTable(&model.ManagedAccountSnapshot{}) {
		var snapshot model.ManagedAccountSnapshot
		query := model.DB.Where("instance_id = ? AND snapshot_kind = ? AND range_key = ? AND payload <> ''",
			stream.instanceID, model.ManagedAccountSnapshotKindInventory, "inventory").Order("observed_at desc").Limit(1).Find(&snapshot)
		if query.Error == nil && query.RowsAffected > 0 && json.Unmarshal([]byte(snapshot.Payload), &page) == nil {
			observedAt = snapshot.ObservedAt
		}
	}
	if observedAt == 0 && model.DB.Migrator().HasTable(&model.ManagedInstanceSnapshot{}) {
		var snapshot model.ManagedInstanceSnapshot
		query := model.DB.Where("instance_id = ? AND snapshot_type = ? AND resource_kind = ? AND collection_status = ? AND payload <> ''",
			stream.instanceID, model.ManagedInstanceSnapshotTypeInventory, "account", model.ManagedInstanceCollectionSucceeded).
			Order("observed_at desc").Limit(1).Find(&snapshot)
		if query.Error == nil && query.RowsAffected > 0 && json.Unmarshal([]byte(snapshot.Payload), &page) == nil {
			observedAt = snapshot.ObservedAt
		}
	}
	if observedAt == 0 {
		return
	}
	stream.mu.Lock()
	if stream.observedAt == 0 {
		stream.replaceInventoryLocked(page.Items)
		stream.replaceSourcesLocked(page.Sources)
		stream.observedAt = observedAt
		stream.status = "cached"
		stream.stale = true
	}
	stream.mu.Unlock()
}

func (stream *conductorRealtimeStream) run(ctx context.Context) {
	gopool.Go(func() { stream.runSourceRefresh(ctx) })
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			stream.finish()
			return
		}
		stream.mu.Lock()
		beforeRevision := stream.revision
		stream.mu.Unlock()
		stream.setStatus("connecting", true, "")
		err := stream.consume(ctx)
		if ctx.Err() != nil {
			stream.finish()
			return
		}
		stream.mu.Lock()
		receivedEvent := stream.revision > beforeRevision
		stream.mu.Unlock()
		if receivedEvent {
			backoff = time.Second
		}
		stream.setStatus("reconnecting", true, managedInstanceObservationErrorCode(err))
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			stream.finish()
			return
		case <-timer.C:
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

func (stream *conductorRealtimeStream) finish() {
	stream.mu.Lock()
	stream.running = false
	stream.cancel = nil
	stream.status = "cached"
	stream.stale = true
	stream.mu.Unlock()
}

func (stream *conductorRealtimeStream) consume(ctx context.Context) error {
	_, _, connector, credential, err := observationClient(stream.instanceID)
	if err != nil {
		return err
	}
	response, err := conductorOpenEventStream(ctx, connector, credential)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return probeHTTPError(response.StatusCode)
	}
	if contentType := strings.ToLower(response.Header.Get("Content-Type")); contentType != "" && !strings.Contains(contentType, "text/event-stream") {
		return &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	stream.setStatus("connected", true, "")
	return consumeConductorSSE(response.Body, func(payload []byte) {
		stream.applyEvent(payload)
	})
}

func conductorOpenEventStream(ctx context.Context, connector *Connector, credential *CredentialMaterial) (*ConnectorStream, error) {
	headers, err := conductorAuthHeaders(ctx, connector, credential)
	if err != nil {
		return nil, err
	}
	response, err := connector.OpenStream(ctx, http.MethodGet, conductorRealtimeEventPath, headers)
	if err != nil || credential.AuthType != "account_password" || response == nil || (response.StatusCode != http.StatusUnauthorized && response.StatusCode != http.StatusForbidden) {
		return response, err
	}
	response.Body.Close()
	invalidateConductorSession(connector, credential, strings.TrimPrefix(headers.Get("Authorization"), "Bearer "))
	headers, err = conductorAuthHeaders(ctx, connector, credential)
	if err != nil {
		return nil, err
	}
	return connector.OpenStream(ctx, http.MethodGet, conductorRealtimeEventPath, headers)
}

func consumeConductorSSE(reader io.Reader, apply func([]byte)) error {
	return consumeConductorSSEWithLimit(reader, conductorRealtimeEventMaxBytes, apply)
}

func consumeConductorSSEWithLimit(reader io.Reader, maxBytes int, apply func([]byte)) error {
	buffered := bufio.NewReaderSize(reader, 64*1024)
	data := make([]byte, 0, 64*1024)
	for {
		line, err := readConductorSSELine(buffered, maxBytes)
		if len(line) > 0 {
			line = bytes.TrimSuffix(line, []byte{'\n'})
			line = bytes.TrimSuffix(line, []byte{'\r'})
			if len(line) == 0 {
				if len(data) > 0 {
					apply(bytes.TrimSuffix(data, []byte{'\n'}))
					data = data[:0]
				}
			} else if bytes.HasPrefix(line, []byte("data:")) {
				part := bytes.TrimPrefix(line, []byte("data:"))
				part = bytes.TrimPrefix(part, []byte{' '})
				if len(data)+len(part)+1 > maxBytes {
					return ErrConnectorResponseLarge
				}
				data = append(data, part...)
				data = append(data, '\n')
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) && len(data) > 0 {
				apply(bytes.TrimSuffix(data, []byte{'\n'}))
			}
			return err
		}
	}
}

func readConductorSSELine(reader *bufio.Reader, maxBytes int) ([]byte, error) {
	line := make([]byte, 0, 64*1024)
	for {
		fragment, err := reader.ReadSlice('\n')
		if len(line)+len(fragment) > maxBytes {
			return nil, ErrConnectorResponseLarge
		}
		line = append(line, fragment...)
		if !errors.Is(err, bufio.ErrBufferFull) {
			return line, err
		}
	}
}

func (stream *conductorRealtimeStream) applyEvent(payload []byte) {
	envelope := struct {
		Type string `json:"type"`
		Data struct {
			Kind string `json:"kind"`
			conductorAccount
			Account  *conductorAccount  `json:"account"`
			Accounts []conductorAccount `json:"accounts"`
		} `json:"data"`
	}{}
	if json.Unmarshal(payload, &envelope) != nil || envelope.Type != "account" {
		return
	}
	stream.mu.Lock()
	if envelope.Data.Kind == "snapshot" {
		stream.accounts = make(map[int64]InventoryItem, len(envelope.Data.Accounts))
	}
	accounts := envelope.Data.Accounts
	if envelope.Data.Account != nil {
		accounts = append(accounts, *envelope.Data.Account)
	}
	if strings.TrimSpace(envelope.Data.AccountID) != "" {
		accounts = append(accounts, envelope.Data.conductorAccount)
	}
	for _, account := range accounts {
		id, err := strconv.ParseInt(strings.TrimSpace(account.AccountID), 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		if account.Removed {
			delete(stream.accounts, id)
			continue
		}
		if item, ok := conductorAccountItem(account); ok {
			stream.accounts[id] = item
		}
	}
	stream.observedAt = common.GetTimestamp()
	stream.revision++
	stream.status = "connected"
	stream.stale = false
	stream.errorCode = ""
	state := stream.snapshotLocked()
	stream.broadcastLocked("accounts", state)
	stream.broadcastLocked("rpm", state)
	stream.mu.Unlock()
}

func (stream *conductorRealtimeStream) runSourceRefresh(ctx context.Context) {
	stream.refreshSources(ctx)
	ticker := time.NewTicker(conductorSourceRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stream.refreshSources(ctx)
		}
	}
}

func (stream *conductorRealtimeStream) refreshSources(ctx context.Context) {
	_, _, connector, credential, err := observationClient(stream.instanceID)
	if err != nil {
		return
	}
	sources, err := conductorInventorySources(ctx, connector, credential)
	if err != nil {
		return
	}
	stream.mu.Lock()
	stream.replaceSourcesLocked(sources)
	state := stream.snapshotLocked()
	stream.broadcastLocked("sources", state)
	stream.mu.Unlock()
}

func (stream *conductorRealtimeStream) replaceInventoryLocked(items []InventoryItem) {
	stream.accounts = make(map[int64]InventoryItem, len(items))
	for _, item := range items {
		if item.ID > 0 {
			stream.accounts[item.ID] = item
		}
	}
}

func (stream *conductorRealtimeStream) replaceSourcesLocked(sources []InventorySource) {
	stream.sources = make(map[string]InventorySource, len(sources))
	for _, source := range sources {
		if strings.TrimSpace(source.ID) != "" {
			stream.sources[source.ID] = source
		}
	}
}

func (stream *conductorRealtimeStream) setStatus(status string, stale bool, errorCode string) {
	stream.mu.Lock()
	stream.status = status
	stream.stale = stale
	stream.errorCode = errorCode
	state := stream.snapshotLocked()
	stream.broadcastLocked("status", state)
	stream.mu.Unlock()
}

func (stream *conductorRealtimeStream) snapshotLocked() ConductorRealtimeState {
	state := ConductorRealtimeState{
		InstanceID: stream.instanceID, ObservedAt: stream.observedAt, StreamStatus: stream.status,
		Stale: stream.stale, ErrorCode: stream.errorCode, RPM: unsupportedMetric("request/min"),
	}
	state.Accounts = make([]InventoryItem, 0, len(stream.accounts))
	for _, account := range stream.accounts {
		if source, ok := stream.sources[account.SourceID]; ok && strings.TrimSpace(source.Name) != "" {
			account.Platform = source.Name
		}
		state.Accounts = append(state.Accounts, account)
		if account.RPM != nil && *account.RPM >= 0 {
			state.AccountsReporting++
		}
		if account.ActiveSessions != nil && *account.ActiveSessions >= 0 {
			state.ActiveSessions += *account.ActiveSessions
		}
	}
	sort.Slice(state.Accounts, func(i, j int) bool { return state.Accounts[i].ID < state.Accounts[j].ID })
	state.AccountsTotal = len(state.Accounts)
	if state.AccountsReporting > 0 || state.AccountsTotal == 0 && stream.observedAt != 0 {
		total := 0.0
		for _, account := range state.Accounts {
			if account.RPM != nil && *account.RPM >= 0 {
				total += float64(*account.RPM)
			}
		}
		state.RPM = supportedMetric(total, "request/min")
	}
	state.Sources = make([]InventorySource, 0, len(stream.sources))
	for _, source := range stream.sources {
		state.Sources = append(state.Sources, source)
	}
	sort.Slice(state.Sources, func(i, j int) bool { return state.Sources[i].ID < state.Sources[j].ID })
	return state
}

func (stream *conductorRealtimeStream) broadcastLocked(eventType string, state ConductorRealtimeState) {
	event := ConductorRealtimeEvent{Type: eventType, State: state}
	for _, subscriber := range stream.subscribers {
		select {
		case subscriber.events <- event:
		default:
			select {
			case <-subscriber.events:
			default:
			}
			select {
			case subscriber.events <- event:
			default:
			}
		}
	}
}

func conductorRealtimeMetrics(instanceID int64) *RealtimeMetricsResult {
	state, ok := CurrentConductorRealtime(instanceID)
	result := &RealtimeMetricsResult{
		RPM: state.RPM, AccountsTotal: state.AccountsTotal, AccountsReporting: state.AccountsReporting,
		ActiveSessions: state.ActiveSessions, StreamStatus: state.StreamStatus, Stale: state.Stale,
	}
	if !ok {
		result.RPM = unsupportedMetric("request/min")
	}
	return result
}

func resetConductorRealtimeHubForTest() {
	defaultConductorRealtimeHub.mu.Lock()
	defer defaultConductorRealtimeHub.mu.Unlock()
	for _, stream := range defaultConductorRealtimeHub.streams {
		stream.mu.Lock()
		if stream.cancel != nil {
			stream.cancel()
		}
		if stream.closeTimer != nil {
			stream.closeTimer.Stop()
		}
		for id, subscriber := range stream.subscribers {
			close(subscriber.events)
			delete(stream.subscribers, id)
		}
		stream.mu.Unlock()
	}
	defaultConductorRealtimeHub.streams = map[int64]*conductorRealtimeStream{}
}
