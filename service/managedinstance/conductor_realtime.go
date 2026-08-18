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
	conductorRealtimeWireMaxBytes  = 256 << 20
	conductorRealtimeCloseGrace    = time.Minute
	conductorSourceRefreshInterval = time.Minute
)

type conductorRealtimeEnvelope struct {
	Type string `json:"type"`
	Data struct {
		Kind string `json:"kind"`
		conductorAccount
		Account  *conductorAccount  `json:"account"`
		Accounts []conductorAccount `json:"accounts"`
	} `json:"data"`
}

type ConductorRealtimeState struct {
	InstanceID          int64             `json:"instance_id"`
	ObservedAt          int64             `json:"observed_at"`
	StreamStatus        string            `json:"stream_status"`
	Stale               bool              `json:"stale"`
	ErrorCode           string            `json:"error_code,omitempty"`
	RPM                 MetricSample      `json:"rpm"`
	RPMCapacity         MetricSample      `json:"rpm_capacity"`
	TodayCost           MetricSample      `json:"today_cost"`
	AccountsTotal       int               `json:"accounts_total"`
	AccountsAvailable   int               `json:"accounts_available"`
	AccountsRateLimited int               `json:"accounts_rate_limited"`
	AccountsReporting   int               `json:"accounts_reporting"`
	ActiveSessions      int               `json:"active_sessions"`
	Accounts            []InventoryItem   `json:"accounts"`
	Sources             []InventorySource `json:"sources"`
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

	mu            sync.Mutex
	accounts      map[int64]InventoryItem
	sources       map[string]InventorySource
	observedAt    int64
	status        string
	stale         bool
	errorCode     string
	rpmPerAccount *float64
	todayCost     *float64
	revision      uint64
	running       bool
	cancel        context.CancelFunc
	closeTimer    *time.Timer
	subscribers   map[uint64]*conductorRealtimeSubscriber
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

func activeConductorRealtime(instanceID int64) (ConductorRealtimeState, bool) {
	stream := defaultConductorRealtimeHub.stream(instanceID, false)
	if stream == nil {
		return ConductorRealtimeState{}, false
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if !stream.running || stream.observedAt == 0 {
		return ConductorRealtimeState{}, false
	}
	return stream.snapshotLocked(), true
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
	gopool.Go(func() { stream.runHistorySampler(ctx) })
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
	return consumeConductorSSEDecoded(response.Body, func(envelope conductorRealtimeEnvelope) {
		stream.applyEnvelope(envelope)
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

// consumeConductorSSEDecoded decodes relevant account fields directly from the
// stream. Conductor snapshots can contain very large session_remaps and
// owner_settings values; decoding into the narrow envelope keeps those fields
// out of memory and avoids reconnecting on an otherwise valid snapshot.
func consumeConductorSSEDecoded(reader io.Reader, apply func(conductorRealtimeEnvelope)) error {
	buffered := bufio.NewReaderSize(reader, 64*1024)
	for {
		field, hasValue, err := readConductorSSEField(buffered)
		if err != nil {
			return err
		}
		if !hasValue {
			continue
		}
		line := &conductorSSELineReader{reader: buffered, maxBytes: conductorRealtimeWireMaxBytes}
		if field != "data" {
			if _, err = io.Copy(io.Discard, line); err != nil {
				return err
			}
			continue
		}
		envelope, decodeErr := decodeConductorRealtimeEnvelope(line, apply)
		_, drainErr := io.Copy(io.Discard, line)
		if decodeErr != nil {
			return decodeErr
		}
		if drainErr != nil {
			return drainErr
		}
		if !envelope.applied && envelope.value.Type == "account" {
			apply(envelope.value)
		}
	}
}

func readConductorSSEField(reader *bufio.Reader) (string, bool, error) {
	name := make([]byte, 0, 16)
	for len(name) <= 128 {
		value, err := reader.ReadByte()
		if err != nil {
			return "", false, err
		}
		switch value {
		case ':':
			if next, peekErr := reader.Peek(1); peekErr == nil && len(next) == 1 && next[0] == ' ' {
				_, _ = reader.ReadByte()
			}
			return string(name), true, nil
		case '\n':
			return strings.TrimSuffix(string(name), "\r"), false, nil
		default:
			name = append(name, value)
		}
	}
	return "", false, &ProbeError{Code: ProbeErrorInvalidResponse}
}

type conductorSSELineReader struct {
	reader    *bufio.Reader
	pending   []byte
	maxBytes  int
	readBytes int
	done      bool
}

func (reader *conductorSSELineReader) Read(target []byte) (int, error) {
	if len(target) == 0 {
		return 0, nil
	}
	for len(reader.pending) == 0 {
		if reader.done {
			return 0, io.EOF
		}
		fragment, err := reader.reader.ReadSlice('\n')
		reader.readBytes += len(fragment)
		if reader.readBytes > reader.maxBytes {
			return 0, ErrConnectorResponseLarge
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			reader.done = true
			fragment = bytes.TrimSuffix(fragment, []byte{'\n'})
			fragment = bytes.TrimSuffix(fragment, []byte{'\r'})
		}
		reader.pending = fragment
		if err != nil && !errors.Is(err, bufio.ErrBufferFull) && !errors.Is(err, io.EOF) {
			return 0, err
		}
		if len(reader.pending) == 0 && reader.done {
			return 0, io.EOF
		}
	}
	count := copy(target, reader.pending)
	reader.pending = reader.pending[count:]
	return count, nil
}

type decodedConductorRealtimeEnvelope struct {
	value   conductorRealtimeEnvelope
	applied bool
}

var conductorDirectAccountFields = map[string]struct{}{
	"account_id": {}, "source": {}, "email": {}, "label": {}, "auth_type": {}, "subscription_type": {},
	"status": {}, "health": {}, "available": {}, "blocked": {}, "blocked_reason": {}, "unavailable_kind": {},
	"dispatch_state": {}, "dispatch_state_changed_at": {}, "created_at": {}, "active_session_count": {},
	"rpm_current": {}, "utilization_5h": {}, "utilization_7d": {}, "utilization_7d_oi": {}, "cause": {}, "_removed": {},
}

func decodeConductorRealtimeEnvelope(reader io.Reader, apply func(conductorRealtimeEnvelope)) (decodedConductorRealtimeEnvelope, error) {
	result := decodedConductorRealtimeEnvelope{}
	decoder := json.NewDecoder(reader)
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return result, &ProbeError{Code: ProbeErrorInvalidResponse}
	}
	for decoder.More() {
		key, err := conductorJSONKey(decoder)
		if err != nil {
			return result, err
		}
		switch key {
		case "type":
			err = decoder.Decode(&result.value.Type)
		case "data":
			err = decodeConductorRealtimeData(decoder, &result, apply)
		default:
			err = discardConductorJSONValue(decoder)
		}
		if err != nil {
			return result, err
		}
	}
	_, err = decoder.Token()
	return result, err
}

func decodeConductorRealtimeData(decoder *json.Decoder, result *decodedConductorRealtimeEnvelope, apply func(conductorRealtimeEnvelope)) error {
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return &ProbeError{Code: ProbeErrorInvalidResponse}
	}
	direct := map[string]json.RawMessage{}
	for decoder.More() {
		key, keyErr := conductorJSONKey(decoder)
		if keyErr != nil {
			return keyErr
		}
		switch key {
		case "kind":
			err = decoder.Decode(&result.value.Data.Kind)
		case "accounts":
			result.value.Data.Accounts, err = decodeConductorAccounts(decoder)
			if err == nil && result.value.Type == "account" && result.value.Data.Kind != "" {
				apply(result.value)
				result.applied = true
			}
		case "account":
			err = decoder.Decode(&result.value.Data.Account)
		default:
			if _, ok := conductorDirectAccountFields[key]; ok {
				var raw json.RawMessage
				err = decoder.Decode(&raw)
				if len(raw) > conductorRealtimeEventMaxBytes {
					return ErrConnectorResponseLarge
				}
				direct[key] = raw
			} else {
				err = discardConductorJSONValue(decoder)
			}
		}
		if err != nil {
			return err
		}
	}
	if _, err = decoder.Token(); err != nil {
		return err
	}
	if len(direct) > 0 {
		encoded, marshalErr := json.Marshal(direct)
		if marshalErr != nil {
			return marshalErr
		}
		if unmarshalErr := json.Unmarshal(encoded, &result.value.Data.conductorAccount); unmarshalErr != nil {
			return unmarshalErr
		}
	}
	return nil
}

func conductorJSONKey(decoder *json.Decoder) (string, error) {
	token, err := decoder.Token()
	if err != nil {
		return "", err
	}
	key, ok := token.(string)
	if !ok {
		return "", &ProbeError{Code: ProbeErrorInvalidResponse}
	}
	return key, nil
}

func discardConductorJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if delimiter != '{' && delimiter != '[' {
		return &ProbeError{Code: ProbeErrorInvalidResponse}
	}
	for decoder.More() {
		if delimiter == '{' {
			if _, err = conductorJSONKey(decoder); err != nil {
				return err
			}
		}
		if err = discardConductorJSONValue(decoder); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

func (stream *conductorRealtimeStream) applyEvent(payload []byte) {
	envelope := conductorRealtimeEnvelope{}
	if json.Unmarshal(payload, &envelope) != nil || envelope.Type != "account" {
		return
	}
	stream.applyEnvelope(envelope)
}

func (stream *conductorRealtimeStream) applyEnvelope(envelope conductorRealtimeEnvelope) {
	if envelope.Type != "account" {
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
	sources, sourcesErr := conductorInventorySources(ctx, connector, credential)
	rpmPerAccount, capacityErr := conductorRPMCapacityPerAccount(ctx, connector, credential)
	todayCost, todayCostErr := conductorTodayCost(ctx, connector, credential)
	if sourcesErr != nil && capacityErr != nil && todayCostErr != nil {
		return
	}
	stream.mu.Lock()
	if sourcesErr == nil {
		stream.replaceSourcesLocked(sources)
	}
	if capacityErr == nil {
		stream.rpmPerAccount = &rpmPerAccount
	}
	if todayCostErr == nil {
		stream.todayCost = &todayCost
	}
	state := stream.snapshotLocked()
	if sourcesErr == nil {
		stream.broadcastLocked("sources", state)
	}
	if capacityErr == nil {
		stream.broadcastLocked("rpm", state)
	}
	if todayCostErr == nil {
		stream.broadcastLocked("status", state)
	}
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
		RPMCapacity: unsupportedMetric("request/min"), TodayCost: unsupportedMetric("USD"),
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
		if account.Enabled != nil && *account.Enabled {
			state.AccountsAvailable++
		}
		if account.RateLimited {
			state.AccountsRateLimited++
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
	if stream.rpmPerAccount != nil && *stream.rpmPerAccount >= 0 && stream.observedAt != 0 {
		perAccount := *stream.rpmPerAccount
		state.RPMCapacity = supportedMetric(float64(state.AccountsAvailable)*perAccount, "request/min")
	}
	if stream.todayCost != nil && *stream.todayCost >= 0 {
		state.TodayCost = supportedMetric(*stream.todayCost, "USD")
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
		RPM: state.RPM, RPMCapacity: state.RPMCapacity, AccountsTotal: state.AccountsTotal, AccountsAvailable: state.AccountsAvailable, AccountsRateLimited: state.AccountsRateLimited, AccountsReporting: state.AccountsReporting,
		TodayCost: state.TodayCost, ActiveSessions: state.ActiveSessions, StreamStatus: state.StreamStatus, Stale: state.Stale,
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
