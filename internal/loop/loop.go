package loop

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/godbus/dbus/v5"

	"zucchini/internal/bluez"
	"zucchini/internal/config"
	"zucchini/internal/matcher"
)

// Backend is the slice of BlueZ that the retry loop needs. *bluez.Client
// satisfies it; tests substitute a fake so the state machine can be driven
// without a radio.
type Backend interface {
	StartDiscovery(adapter dbus.ObjectPath) error
	StopDiscovery(adapter dbus.ObjectPath) error
	Connect(ctx context.Context, path dbus.ObjectPath) error
	Devices() ([]bluez.DeviceSnapshot, error)
	Events(ctx context.Context) (<-chan bluez.Event, func(), error)
}

type Runner struct {
	cfg     *config.Config
	matcher *matcher.Matcher
	client  Backend
	logger  *log.Logger
	tick    time.Duration

	discoveryDown   atomic.Bool
	lastDiscoverLog atomic.Int64

	mu      sync.Mutex
	targets map[dbus.ObjectPath]*target
}

type target struct {
	path        dbus.ObjectPath
	address     string
	sigName     string
	lastSeen    atomic.Int64 // unix nano
	connected   atomic.Bool
	lastFailLog atomic.Int64 // unix nano, rate-limits failure spam
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

func New(cfg *config.Config, m *matcher.Matcher, b Backend, logger *log.Logger) *Runner {
	return &Runner{
		cfg:     cfg,
		matcher: m,
		client:  b,
		logger:  logger,
		tick:    time.Second,
		targets: map[dbus.ObjectPath]*target{},
	}
}

// Run starts discovery and drives the retry loop until ctx is cancelled.
func (r *Runner) Run(ctx context.Context, adapter dbus.ObjectPath) error {
	if err := r.client.StartDiscovery(adapter); err != nil {
		return err
	}
	defer func() {
		if err := r.client.StopDiscovery(adapter); err != nil {
			r.logger.Printf("stop discovery: %v", err)
		}
	}()

	events, stop, err := r.client.Events(ctx)
	if err != nil {
		return err
	}
	defer stop()

	if existing, err := r.client.Devices(); err != nil {
		r.logger.Printf("seed devices: %v", err)
	} else {
		for _, d := range existing {
			r.observe(ctx, d)
		}
	}

	ticker := time.NewTicker(r.tick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.shutdown()
			return ctx.Err()
		case ev, ok := <-events:
			if !ok {
				r.shutdown()
				return nil
			}
			switch ev.Type {
			case bluez.EventBlueZGone:
				r.shutdown()
				return fmt.Errorf("%s disappeared from the system bus", bluez.Service)
			case bluez.EventDiscoveryStopped:
				if r.discoveryDown.CompareAndSwap(false, true) {
					r.logger.Printf("discovery stopped on %s, rediscovering", adapter)
				}
			default:
				r.handle(ctx, ev)
			}
		case <-ticker.C:
			r.reap()
			if r.discoveryDown.Load() {
				if err := r.client.StartDiscovery(adapter); err != nil {
					r.logDiscoveryFail(err)
				} else {
					r.discoveryDown.Store(false)
					r.logger.Printf("discovery restored on %s", adapter)
				}
			}
		}
	}
}

func (r *Runner) handle(ctx context.Context, ev bluez.Event) {
	switch ev.Type {
	case bluez.EventAdded, bluez.EventChanged:
		r.observe(ctx, ev.Device)
	case bluez.EventRemoved:
		r.forget(ev.Device.Path)
	}
}

func (r *Runner) observe(ctx context.Context, d bluez.DeviceSnapshot) {
	sigName, ok := r.matcher.Match(matcher.Device{
		Path:             string(d.Path),
		Address:          d.Address,
		Name:             d.Name,
		Alias:            d.Alias,
		UUIDs:            d.UUIDs,
		ManufacturerData: d.ManufacturerData,
		ServiceData:      d.ServiceData,
	})
	if !ok {
		return
	}

	r.mu.Lock()
	t, exists := r.targets[d.Path]
	if !exists {
		tctx, cancel := context.WithCancel(ctx)
		t = &target{
			path:    d.Path,
			address: d.Address,
			sigName: sigName,
			cancel:  cancel,
		}
		r.targets[d.Path] = t
		for i := 0; i < r.cfg.Workers; i++ {
			t.wg.Add(1)
			go r.hammer(tctx, t)
		}
		r.logger.Printf("target acquired: %s path=%s signature=%s workers=%d",
			d.Address, d.Path, sigName, r.cfg.Workers)
	}
	r.mu.Unlock()

	t.lastSeen.Store(time.Now().UnixNano())
	t.connected.Store(d.Connected)
}

func (r *Runner) forget(path dbus.ObjectPath) {
	r.mu.Lock()
	t, ok := r.targets[path]
	if ok {
		delete(r.targets, path)
	}
	r.mu.Unlock()
	if !ok {
		return
	}
	t.cancel()
	t.wg.Wait()
	r.logger.Printf("target released: %s path=%s", t.address, path)
}

// reap drops targets whose signature has been unseen longer than the grace window.
func (r *Runner) reap() {
	cutoff := time.Now().Add(-r.cfg.Grace()).UnixNano()
	var stale []dbus.ObjectPath

	r.mu.Lock()
	for p, t := range r.targets {
		if t.lastSeen.Load() < cutoff {
			stale = append(stale, p)
		}
	}
	r.mu.Unlock()

	for _, p := range stale {
		r.logger.Printf("signature gone past grace window (%s), releasing", r.cfg.Grace())
		r.forget(p)
	}
}

func (r *Runner) shutdown() {
	r.mu.Lock()
	all := make([]*target, 0, len(r.targets))
	for p, t := range r.targets {
		all = append(all, t)
		delete(r.targets, p)
	}
	r.mu.Unlock()

	for _, t := range all {
		t.cancel()
	}
	for _, t := range all {
		t.wg.Wait()
	}
}

// hammer issues Connect calls back-to-back with no backoff until ctx is
// cancelled. A successful Connect marks the device connected, which idles the
// worker; the observation loop clears the flag when the link drops.
func (r *Runner) hammer(ctx context.Context, t *target) {
	defer t.wg.Done()
	for {
		if ctx.Err() != nil {
			return
		}
		if !t.connected.Load() {
			callCtx, cancel := context.WithTimeout(ctx, r.cfg.CallTimeout())
			err := r.client.Connect(callCtx, t.path)
			cancel()
			if err == nil {
				t.connected.Store(true)
			} else if ctx.Err() == nil {
				r.logFailure(t, err)
			}

			select {
			case <-ctx.Done():
				return
			case <-time.After(r.cfg.AttemptGap()):
			}
			continue
		}

		// Idle on the live link: one ticker per idle episode instead of a
		// timer allocation per poll.
		idle := time.NewTicker(200 * time.Millisecond)
		for t.connected.Load() {
			select {
			case <-ctx.Done():
				idle.Stop()
				return
			case <-idle.C:
			}
		}
		idle.Stop()
	}
}

// logFailure rate-limits per-target failure lines so a spam loop cannot fill the journal.
func (r *Runner) logFailure(t *target, err error) {
	now := time.Now().UnixNano()
	last := t.lastFailLog.Load()
	if now-last < int64(time.Second) {
		return
	}
	if !t.lastFailLog.CompareAndSwap(last, now) {
		return
	}
	r.logger.Printf("connect failed: %s (%s): %v", t.address, t.sigName, err)
}

// logDiscoveryFail rate-limits re-arm failure lines to one per second, same
// pattern as logFailure, so a broken adapter cannot fill the journal.
func (r *Runner) logDiscoveryFail(err error) {
	now := time.Now().UnixNano()
	last := r.lastDiscoverLog.Load()
	if now-last < int64(time.Second) {
		return
	}
	if !r.lastDiscoverLog.CompareAndSwap(last, now) {
		return
	}
	r.logger.Printf("restart discovery: %v", err)
}
