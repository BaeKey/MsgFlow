package handler

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestRequestDeduperBlocksConcurrentDuplicateUntilFinish(t *testing.T) {
	deduper := newRequestDeduper(1)
	key := buildRequestDedupKey("title", "body", []string{"bark"})
	now := time.Now()

	if !deduper.TryStart(key, now) {
		t.Fatal("first request should acquire dedup slot")
	}
	if deduper.TryStart(key, now) {
		t.Fatal("concurrent duplicate should be rejected while first request is in flight")
	}

	deduper.Finish(key, now, false)
	if !deduper.TryStart(key, now) {
		t.Fatal("failed request should release dedup slot for retry")
	}

	deduper.Finish(key, now, true)
	if deduper.TryStart(key, now) {
		t.Fatal("successful request should remain deduplicated inside window")
	}
}

func TestChannelControlCanceledWaitDoesNotReserveWindow(t *testing.T) {
	control := &channelControl{
		minInterval: 80 * time.Millisecond,
	}

	release, err := control.Acquire(context.Background())
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	release()

	cancelCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, err := control.Acquire(cancelCtx); err == nil {
		t.Fatal("expected acquire to be canceled")
	}

	reacquireStart := time.Now()
	release, err = control.Acquire(context.Background())
	if err != nil {
		t.Fatalf("reacquire failed: %v", err)
	}
	release()

	waitedAfterCancel := time.Since(reacquireStart)
	if waitedAfterCancel > 95*time.Millisecond {
		t.Fatalf("reacquire waited too long after canceled attempt: %v", waitedAfterCancel)
	}

	if time.Since(start) < 20*time.Millisecond {
		t.Fatal("canceled acquire returned too early; test setup did not exercise wait path")
	}
}

func TestRequestDeduperPruneExpiredRebuildsSeenMap(t *testing.T) {
	deduper := newRequestDeduper(1)
	if deduper == nil {
		t.Fatal("expected deduper")
	}

	originalSeen := deduper.seen
	originalPtr := reflect.ValueOf(originalSeen).Pointer()
	now := time.Now()
	deduper.seen["expired"] = now.Add(-time.Second)

	deduper.pruneExpired(now)

	if len(deduper.seen) != 0 {
		t.Fatalf("expected seen map to be empty after pruning, got %d entries", len(deduper.seen))
	}
	if deduper.seen == nil {
		t.Fatal("expected seen map to remain initialized")
	}
	if reflect.ValueOf(deduper.seen).Pointer() == originalPtr {
		t.Fatal("expected seen map backing store to be rebuilt after pruning all entries")
	}
}
