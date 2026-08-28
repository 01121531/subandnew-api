package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/assistant/channel/wechatilink"
	"github.com/01121531/subandnew-api/service/assistant/channelservice"
	"github.com/01121531/subandnew-api/service/assistant/processor"
	"github.com/01121531/subandnew-api/service/assistant/secrets"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	leaseDuration      = 60 * time.Second
	reconcilePeriod    = time.Second
	backlogPeriod      = 500 * time.Millisecond
	loginCleanupPeriod = time.Minute
	workerParallel     = 4
)

type Worker struct {
	db        *gorm.DB
	channels  *channelservice.Service
	processor *processor.Processor
	ownerID   string
	now       func() time.Time

	mu             sync.Mutex
	started        bool
	cancel         context.CancelFunc
	pollsInFlight  map[int64]struct{}
	eventsInFlight map[int64]struct{}
	outboxInFlight map[int64]struct{}
	workSlots      chan struct{}
	waitGroup      sync.WaitGroup
}

func New(db *gorm.DB, channels *channelservice.Service, messageProcessor *processor.Processor, ownerID string) (*Worker, error) {
	ownerID = strings.TrimSpace(ownerID)
	if db == nil || channels == nil || messageProcessor == nil || ownerID == "" {
		return nil, errors.New("assistant worker dependencies and owner are required")
	}
	return &Worker{
		db: db, channels: channels, processor: messageProcessor, ownerID: ownerID, now: time.Now,
		pollsInFlight: make(map[int64]struct{}), eventsInFlight: make(map[int64]struct{}),
		outboxInFlight: make(map[int64]struct{}), workSlots: make(chan struct{}, workerParallel),
	}, nil
}

func NewDefault(db *gorm.DB, nodeName string) (*Worker, error) {
	cipher, err := secrets.NewFromEnvironment()
	if err != nil {
		return nil, err
	}
	channels, err := channelservice.NewService(db, cipher, channelservice.Config{})
	if err != nil {
		return nil, err
	}
	messageProcessor, err := processor.NewDefault(db, cipher, channels)
	if err != nil {
		return nil, err
	}
	ownerID := strings.TrimSpace(nodeName) + "-" + uuid.NewString()
	return New(db, channels, messageProcessor, ownerID)
}

func (w *Worker) Start() {
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.started = true
	w.waitGroup.Add(1)
	w.mu.Unlock()
	go w.loop(ctx)
}

func (w *Worker) Stop(ctx context.Context) error {
	w.mu.Lock()
	if !w.started {
		w.mu.Unlock()
		return nil
	}
	w.started = false
	cancel := w.cancel
	w.mu.Unlock()
	cancel()
	done := make(chan struct{})
	go func() {
		w.waitGroup.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Worker) loop(ctx context.Context) {
	defer w.waitGroup.Done()
	reconcileTicker := time.NewTicker(reconcilePeriod)
	backlogTicker := time.NewTicker(backlogPeriod)
	loginCleanupTicker := time.NewTicker(loginCleanupPeriod)
	defer reconcileTicker.Stop()
	defer backlogTicker.Stop()
	defer loginCleanupTicker.Stop()
	w.cleanupPendingLogins(ctx)
	w.reconcileChannels(ctx)
	w.dispatchBacklog(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-reconcileTicker.C:
			w.reconcileChannels(ctx)
		case <-backlogTicker.C:
			w.dispatchBacklog(ctx)
		case <-loginCleanupTicker.C:
			w.cleanupPendingLogins(ctx)
		}
	}
}

func (w *Worker) cleanupPendingLogins(ctx context.Context) {
	if err := w.channels.CleanupExpiredLogins(ctx, w.now()); err != nil {
		common.SysError("assistant pending login cleanup failed: " + err.Error())
	}
}

func (w *Worker) reconcileChannels(ctx context.Context) {
	channels, err := w.channels.List(ctx)
	if err != nil {
		common.SysError("assistant channel reconcile failed: " + err.Error())
		return
	}
	for _, channel := range channels {
		if !channel.Enabled || channel.Status != model.AssistantChannelStatusConnected {
			continue
		}
		w.startPoll(ctx, channel.ID)
	}
}

func (w *Worker) startPoll(ctx context.Context, channelID int64) {
	w.mu.Lock()
	if _, exists := w.pollsInFlight[channelID]; exists || !w.started {
		w.mu.Unlock()
		return
	}
	w.pollsInFlight[channelID] = struct{}{}
	w.waitGroup.Add(1)
	w.mu.Unlock()
	go func() {
		defer w.waitGroup.Done()
		defer w.clearInFlight(w.pollsInFlight, channelID)
		acquired, _, err := w.acquireLease(ctx, channelID)
		if err != nil || !acquired {
			return
		}
		eventIDs, err := w.channels.PollOnce(ctx, channelID)
		if err != nil {
			if errors.Is(err, wechatilink.ErrSessionExpired) {
				_ = w.db.WithContext(ctx).Model(&model.AssistantChannel{}).Where("id = ?", channelID).Updates(map[string]any{
					"status": model.AssistantChannelStatusReauthRequired, "enabled": false, "reauth_reason": "session_expired",
				}).Error
			}
			return
		}
		for _, eventID := range eventIDs {
			w.dispatchEvent(ctx, eventID)
		}
	}()
}

func (w *Worker) dispatchBacklog(ctx context.Context) {
	now := w.now().Unix()
	var eventIDs []int64
	if err := w.db.WithContext(ctx).Model(&model.AssistantInboundEvent{}).
		Where("status IN ? AND next_attempt_at <= ? AND attempt < ?", []string{model.AssistantInboundStatusPending, model.AssistantInboundStatusFailed}, now, 3).
		Order("id ASC").Limit(50).Pluck("id", &eventIDs).Error; err == nil {
		for _, id := range eventIDs {
			w.dispatchEvent(ctx, id)
		}
	}
	var outboxIDs []int64
	if err := w.db.WithContext(ctx).Model(&model.AssistantOutbox{}).
		Where("status IN ? AND next_attempt_at <= ? AND attempt < ?", []string{model.AssistantOutboxStatusPending, model.AssistantOutboxStatusFailed}, now, 5).
		Order("id ASC").Limit(50).Pluck("id", &outboxIDs).Error; err == nil {
		for _, id := range outboxIDs {
			w.dispatchOutbox(ctx, id)
		}
	}
}

func (w *Worker) dispatchEvent(ctx context.Context, id int64) {
	if !w.markInFlight(w.eventsInFlight, id) {
		return
	}
	w.waitGroup.Add(1)
	go func() {
		defer w.waitGroup.Done()
		defer w.clearInFlight(w.eventsInFlight, id)
		select {
		case w.workSlots <- struct{}{}:
			defer func() { <-w.workSlots }()
		case <-ctx.Done():
			return
		}
		if err := w.processor.Process(ctx, id); err != nil && !errors.Is(err, processor.ErrEventNotPending) && !errors.Is(err, context.Canceled) {
			common.SysError(fmt.Sprintf("assistant inbound %d failed: %v", id, err))
		}
	}()
}

func (w *Worker) dispatchOutbox(ctx context.Context, id int64) {
	if !w.markInFlight(w.outboxInFlight, id) {
		return
	}
	w.waitGroup.Add(1)
	go func() {
		defer w.waitGroup.Done()
		defer w.clearInFlight(w.outboxInFlight, id)
		select {
		case w.workSlots <- struct{}{}:
			defer func() { <-w.workSlots }()
		case <-ctx.Done():
			return
		}
		_ = w.processor.Deliver(ctx, id)
	}()
}

func (w *Worker) acquireLease(ctx context.Context, channelID int64) (bool, int64, error) {
	now := w.now().Unix()
	lockedUntil := w.now().Add(leaseDuration).Unix()
	var token int64
	err := w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.AssistantChannelLease{ChannelID: channelID, OwnerID: "", LockedUntil: 0}).Error; err != nil {
			return err
		}
		result := tx.Model(&model.AssistantChannelLease{}).
			Where("channel_id = ? AND (locked_until <= ? OR owner_id = ?)", channelID, now, w.ownerID).
			Updates(map[string]any{"owner_id": w.ownerID, "locked_until": lockedUntil, "fencing_token": gorm.Expr("fencing_token + 1")})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		return tx.Model(&model.AssistantChannelLease{}).Where("channel_id = ? AND owner_id = ?", channelID, w.ownerID).Pluck("fencing_token", &token).Error
	})
	return token > 0, token, err
}

func (w *Worker) markInFlight(store map[int64]struct{}, id int64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.started {
		return false
	}
	if _, exists := store[id]; exists {
		return false
	}
	store[id] = struct{}{}
	return true
}

func (w *Worker) clearInFlight(store map[int64]struct{}, id int64) {
	w.mu.Lock()
	delete(store, id)
	w.mu.Unlock()
}
