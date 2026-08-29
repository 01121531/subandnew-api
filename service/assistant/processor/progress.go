package processor

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/assistant/channel/wechatilink"
	"github.com/01121531/subandnew-api/service/assistant/runner"
)

const (
	progressInitialDelay = 5 * time.Second
	progressHeartbeat    = 30 * time.Second
	progressSendTimeout  = 3 * time.Second
	maxProgressMessages  = 8
)

type progressReporter struct {
	events   chan runner.ProgressEvent
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

type progressReporterConfig struct {
	initialDelay time.Duration
	heartbeat    time.Duration
	maxMessages int
	now          func() time.Time
	send         func(int, string)
}

func (p *Processor) startProgressReporter(ctx context.Context, event *model.AssistantInboundEvent, contextToken string, runID string) *progressReporter {
	if event == nil || strings.TrimSpace(event.PeerID) == "" || strings.TrimSpace(contextToken) == "" {
		return stoppedProgressReporter()
	}
	client, _, err := p.channels.ConnectedClient(ctx, event.ChannelID)
	if err != nil {
		return stoppedProgressReporter()
	}
	return newProgressReporter(progressReporterConfig{
		initialDelay: progressInitialDelay,
		heartbeat:    progressHeartbeat,
		maxMessages: maxProgressMessages,
		now:          p.now,
		send: func(sequence int, text string) {
			sendContext, cancel := context.WithTimeout(context.Background(), progressSendTimeout)
			defer cancel()
			_, _ = client.SendMessage(sendContext, wechatilink.Message{
				ToUserID: event.PeerID, ClientID: fmt.Sprintf("inbound:%d:progress:%d", event.ID, sequence),
				ContextToken: contextToken, MessageType: wechatilink.MessageTypeBot, MessageState: wechatilink.MessageStateFinish,
				Items: []wechatilink.MessageItem{{Type: wechatilink.MessageItemTypeText, IsCompleted: true, Text: &wechatilink.TextItem{Text: text}}},
			})
		},
	})
}

func stoppedProgressReporter() *progressReporter {
	reporter := &progressReporter{events: make(chan runner.ProgressEvent), stop: make(chan struct{}), done: make(chan struct{})}
	close(reporter.done)
	return reporter
}

func newProgressReporter(config progressReporterConfig) *progressReporter {
	reporter := &progressReporter{events: make(chan runner.ProgressEvent, 8), stop: make(chan struct{}), done: make(chan struct{})}
	if config.initialDelay <= 0 || config.heartbeat <= 0 || config.maxMessages <= 0 || config.now == nil || config.send == nil {
		close(reporter.done)
		return reporter
	}
	started := config.now()
	go func() {
		defer close(reporter.done)
		initialTimer := time.NewTimer(config.initialDelay)
		heartbeatTicker := time.NewTicker(config.heartbeat)
		defer initialTimer.Stop()
		defer heartbeatTicker.Stop()
		stage := "正在分析问题"
		lastStage := ""
		lastSent := time.Time{}
		sent := 0
		send := func(text string) {
			if sent >= config.maxMessages {
				return
			}
			sent++
			config.send(sent, text)
			lastSent = config.now()
		}
		visible := false
		for {
			select {
			case <-reporter.stop:
				return
			case <-initialTimer.C:
				visible = true
				lastStage = stage
				send("已收到，正在分析问题。")
			case event := <-reporter.events:
				nextStage := progressStage(event)
				if nextStage == "" {
					continue
				}
				stage = nextStage
				if visible && stage != lastStage {
					lastStage = stage
					send(stage + "。")
				}
			case <-heartbeatTicker.C:
				if visible && sent < config.maxMessages && (lastSent.IsZero() || config.now().Sub(lastSent) >= config.heartbeat) {
					send(fmt.Sprintf("%s，已处理 %d 秒。", stage, max(0, int(config.now().Sub(started).Seconds()))))
				}
			}
		}
	}()
	return reporter
}

func (r *progressReporter) Report(event runner.ProgressEvent) {
	if r == nil {
		return
	}
	select {
	case <-r.done:
		return
	default:
	}
	select {
	case r.events <- event:
	default:
	}
}

func (r *progressReporter) Stop() {
	if r == nil {
		return
	}
	r.stopOnce.Do(func() { close(r.stop) })
	<-r.done
}

func progressStage(event runner.ProgressEvent) string {
	switch event.Type {
	case runner.ProgressModelRequestStarted:
		if event.Step > 1 {
			return "正在整理查询结果"
		}
		return "正在分析问题"
	case runner.ProgressToolStarted:
		switch event.Tool {
		case "get_runtime_context", "list_instances":
			return "正在确认实例与时间范围"
		case "get_dashboard_summary", "get_realtime_metrics":
			return "正在读取实例指标"
		case "get_metric_history":
			return "正在查询历史指标"
		case "query_managed_accounts":
			return "正在读取账号快照"
		case "get_usage_record_filter_options", "query_usage_records", "get_usage_record_summary":
			return "正在查询使用记录"
		case "get_instance_health", "get_open_alerts":
			return "正在读取巡检与告警"
		default:
			return "正在读取所需数据"
		}
	default:
		return ""
	}
}
