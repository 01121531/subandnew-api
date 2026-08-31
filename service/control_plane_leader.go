package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/logger"
	"github.com/01121531/subandnew-api/model"
)

const (
	controlPlaneLeaderLeaseName = "control-plane-background-services"
	controlPlaneLeaderLeaseTTL  = 30 * time.Second
	controlPlaneLeaderRetry     = 5 * time.Second
	controlPlaneLeaderHeartbeat = 10 * time.Second
)

type ControlPlaneLeader struct {
	holder   string
	onLeader func()
	onLost   func()
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

func StartControlPlaneLeader(onLeader func(), onLost func()) *ControlPlaneLeader {
	if !common.IsMasterNode || model.DB == nil || onLeader == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	leader := &ControlPlaneLeader{
		holder:   fmt.Sprintf("%s-%s", common.NodeName, common.GetRandomString(12)),
		onLeader: onLeader,
		onLost:   onLost,
		cancel:   cancel,
	}
	leader.wg.Add(1)
	go leader.run(ctx)
	return leader
}

func (leader *ControlPlaneLeader) Stop(ctx context.Context) error {
	if leader == nil {
		return nil
	}
	leader.cancel()
	done := make(chan struct{})
	go func() {
		leader.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (leader *ControlPlaneLeader) run(ctx context.Context) {
	defer leader.wg.Done()
	isLeader := false
	lastRenewedAt := time.Time{}
	ticker := time.NewTicker(controlPlaneLeaderRetry)
	defer ticker.Stop()
	defer func() {
		if isLeader {
			_ = model.ReleaseControlPlaneLease(controlPlaneLeaderLeaseName, leader.holder)
		}
	}()

	for {
		now := time.Now()
		if !isLeader {
			acquired, err := model.TryAcquireControlPlaneLease(controlPlaneLeaderLeaseName, leader.holder, now.Unix(), now.Add(controlPlaneLeaderLeaseTTL).Unix())
			if err != nil {
				logger.LogWarn(context.Background(), fmt.Sprintf("control plane leader acquisition failed: %v", err))
			} else if acquired {
				isLeader = true
				lastRenewedAt = now
				logger.LogInfo(context.Background(), fmt.Sprintf("control plane leadership acquired: holder=%s", leader.holder))
				leader.onLeader()
			}
		} else if now.Sub(lastRenewedAt) >= controlPlaneLeaderHeartbeat {
			renewed, err := model.RenewControlPlaneLease(controlPlaneLeaderLeaseName, leader.holder, now.Unix(), now.Add(controlPlaneLeaderLeaseTTL).Unix())
			if err == nil && renewed {
				lastRenewedAt = now
			} else if now.Sub(lastRenewedAt) >= controlPlaneLeaderLeaseTTL {
				logger.LogWarn(context.Background(), fmt.Sprintf("control plane leadership lost: holder=%s err=%v", leader.holder, err))
				if leader.onLost != nil {
					leader.onLost()
				}
				return
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
