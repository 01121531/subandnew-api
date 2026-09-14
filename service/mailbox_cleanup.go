package service

import (
	"context"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/mailbox"
)

var mailboxCleanupState struct {
	sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// Called only while the process owns the control-plane leader lease.
func StartMailboxCleanup() {
	mailboxCleanupState.Lock()
	defer mailboxCleanupState.Unlock()
	if mailboxCleanupState.cancel != nil || !common.IsMasterNode {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	mailboxCleanupState.cancel, mailboxCleanupState.done = cancel, done
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			if err := mailbox.New(model.DB).Cleanup(ctx); err != nil && ctx.Err() == nil {
				common.SysError("mailbox attachment cleanup failed")
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
func CancelMailboxCleanup() {
	mailboxCleanupState.Lock()
	defer mailboxCleanupState.Unlock()
	if mailboxCleanupState.cancel != nil {
		mailboxCleanupState.cancel()
	}
}
func StopMailboxCleanup(ctx context.Context) error {
	mailboxCleanupState.Lock()
	cancel, done := mailboxCleanupState.cancel, mailboxCleanupState.done
	if cancel != nil {
		cancel()
	}
	mailboxCleanupState.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		mailboxCleanupState.Lock()
		if mailboxCleanupState.done == done {
			mailboxCleanupState.cancel = nil
			mailboxCleanupState.done = nil
		}
		mailboxCleanupState.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
