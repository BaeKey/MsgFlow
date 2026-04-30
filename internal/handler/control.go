package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"msgflow/internal/config"
	"msgflow/internal/plugin"
)

type requestDeduper struct {
	window   time.Duration
	mu       sync.Mutex
	seen     map[string]time.Time
	inFlight map[string]struct{}
}

func newRequestDeduper(windowSeconds int) *requestDeduper {
	if windowSeconds <= 0 {
		return nil
	}
	return &requestDeduper{
		window:   time.Duration(windowSeconds) * time.Second,
		seen:     make(map[string]time.Time),
		inFlight: make(map[string]struct{}),
	}
}

func (d *requestDeduper) TryStart(key string, now time.Time) bool {
	if d == nil {
		return true
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	d.pruneExpired(now)
	if expiresAt, ok := d.seen[key]; ok && expiresAt.After(now) {
		return false
	}
	if _, ok := d.inFlight[key]; ok {
		return false
	}

	d.inFlight[key] = struct{}{}
	return true
}

func (d *requestDeduper) Finish(key string, now time.Time, success bool) {
	if d == nil {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.inFlight, key)
	d.pruneExpired(now)
	if success {
		d.seen[key] = now.Add(d.window)
	}
}

func (d *requestDeduper) pruneExpired(now time.Time) {
	for existingKey, expiresAt := range d.seen {
		if !expiresAt.After(now) {
			delete(d.seen, existingKey)
		}
	}

	// 高频突发写入后，即使元素被 delete，map bucket 也不会自动缩回去。
	// 在完全清空时重建 map，避免 deduper 长期持有历史容量。
	if len(d.seen) == 0 {
		d.seen = make(map[string]time.Time)
	}
}

func buildRequestDedupKey(msgTitle, msgBody string, channels []string) string {
	sortedChannels := append([]string(nil), channels...)
	sort.Strings(sortedChannels)

	sum := sha256.Sum256([]byte(strings.Join([]string{
		msgTitle,
		msgBody,
		strings.Join(sortedChannels, "\x00"),
	}, "\x01")))
	return hex.EncodeToString(sum[:])
}

func startRequestDedup(deduper *requestDeduper, msg plugin.Message, channels []string) (string, bool) {
	if deduper == nil {
		return "", true
	}
	key := buildRequestDedupKey(msg.Title, msg.Body, channels)
	return key, deduper.TryStart(key, time.Now())
}

func finishRequestDedup(deduper *requestDeduper, key string, success bool) {
	if deduper == nil {
		return
	}
	deduper.Finish(key, time.Now(), success)
}

type channelControl struct {
	sem         chan struct{}
	minInterval time.Duration
	mu          sync.Mutex
	nextAllowed time.Time
}

func (c *channelControl) Acquire(ctx context.Context) (func(), error) {
	releaseSem := func() {}
	if c.sem != nil {
		select {
		case c.sem <- struct{}{}:
			releaseSem = func() { <-c.sem }
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if c.minInterval <= 0 {
		return releaseSem, nil
	}

	for {
		c.mu.Lock()
		now := time.Now()
		if !c.nextAllowed.After(now) {
			c.nextAllowed = now.Add(c.minInterval)
			c.mu.Unlock()
			return releaseSem, nil
		}
		startAt := c.nextAllowed
		c.mu.Unlock()

		timer := time.NewTimer(time.Until(startAt))
		select {
		case <-timer.C:
		case <-ctx.Done():
			stopTimer(timer)
			releaseSem()
			return nil, ctx.Err()
		}
		stopTimer(timer)
	}
}

func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

type channelControlManager struct {
	controls map[string]*channelControl
}

func newChannelControlManager(cfg *config.Config) *channelControlManager {
	manager := &channelControlManager{
		controls: make(map[string]*channelControl, len(cfg.Notifiers)),
	}

	for channelName, raw := range cfg.Notifiers {
		maxConcurrency := parseConfigInt(raw["max_concurrency"])
		minIntervalMS := parseConfigInt(raw["min_interval_ms"])
		if maxConcurrency <= 0 && minIntervalMS <= 0 {
			continue
		}

		control := &channelControl{}
		if maxConcurrency > 0 {
			control.sem = make(chan struct{}, maxConcurrency)
		}
		if minIntervalMS > 0 {
			control.minInterval = time.Duration(minIntervalMS) * time.Millisecond
		}
		manager.controls[channelName] = control
	}

	return manager
}

func (m *channelControlManager) Acquire(ctx context.Context, channelName string) (func(), error) {
	if m == nil {
		return func() {}, nil
	}
	control, ok := m.controls[channelName]
	if !ok {
		return func() {}, nil
	}
	return control.Acquire(ctx)
}

func parseConfigInt(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return parsed
}
