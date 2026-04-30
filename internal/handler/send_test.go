package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"

	"msgflow/internal/config"
	"msgflow/internal/plugin"
)

type panicNotifier struct {
	name string
}

func (n *panicNotifier) Name() string {
	return n.name
}

func (n *panicNotifier) Send(context.Context, plugin.Message, map[string]string) error {
	panic("boom")
}

type blockingNotifier struct {
	name string
}

func (n *blockingNotifier) Name() string {
	return n.name
}

func (n *blockingNotifier) Send(ctx context.Context, _ plugin.Message, _ map[string]string) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestDispatchReleasesChannelControlOnNotifierPanic(t *testing.T) {
	channelName := "panic-channel"
	notifierName := "panic-notifier-test"
	plugin.Register(&panicNotifier{name: notifierName})
	plugin.RegisterAlias(channelName, notifierName)

	cfg := &config.Config{
		Server: config.ServerConfig{Retry: 0},
		Notifiers: map[string]map[string]string{
			channelName: {
				"type":            notifierName,
				"max_concurrency": "1",
			},
		},
	}
	h := New(cfg, zap.NewNop())

	err := h.dispatch([]string{channelName}, plugin.Message{Body: "body"})
	if err == nil {
		t.Fatal("expected dispatch error after notifier panic")
	}

	acquireCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	release, acquireErr := h.channelControl.Acquire(acquireCtx, channelName)
	if acquireErr != nil {
		t.Fatalf("expected channel control token to be released after panic, got %v", acquireErr)
	}
	release()
}

func TestSendAlertUsesDeliveryContextCancellation(t *testing.T) {
	channelName := "alert-channel"
	notifierName := "blocking-notifier-test"
	plugin.Register(&blockingNotifier{name: notifierName})
	plugin.RegisterAlias(channelName, notifierName)

	cfg := &config.Config{
		Notifiers: map[string]map[string]string{
			channelName: {
				"type": notifierName,
			},
		},
	}
	deliveryCtx, cancelDelivery := context.WithCancel(context.Background())
	cancelDelivery()
	h := NewWithContext(deliveryCtx, cfg, zap.NewNop())

	start := time.Now()
	h.sendAlert([]string{channelName}, plugin.Message{Body: "body"})
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("sendAlert did not exit promptly after delivery context cancellation: %v", elapsed)
	}
}

func TestWaitRetryDelayStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := waitRetryDelay(ctx, time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled error, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("waitRetryDelay returned too slowly after cancellation: %v", elapsed)
	}
}
