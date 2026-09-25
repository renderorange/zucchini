package loop

import (
    "context"
    "io"
    "log"
    "sync"
    "testing"
    "time"

    "github.com/godbus/dbus/v5"

    "zucchini/internal/bluez"
    "zucchini/internal/config"
    "zucchini/internal/matcher"
)

// fakeBackend stands in for bluez.Client. It records Connect calls and never
// touches D-Bus, so the loop's state machine can be exercised in isolation.
type fakeBackend struct {
    mu           sync.Mutex
    connectCalls int
    paths        []dbus.ObjectPath
    err          error
    block        chan struct{}
}

func (f *fakeBackend) StartDiscovery(dbus.ObjectPath) error { return nil }
func (f *fakeBackend) StopDiscovery(dbus.ObjectPath) error  { return nil }

func (f *fakeBackend) Devices() ([]bluez.DeviceSnapshot, error) { return nil, nil }

func (f *fakeBackend) Events() (<-chan bluez.Event, func(), error) {
    return make(chan bluez.Event), func() {}, nil
}

func (f *fakeBackend) Connect(ctx context.Context, path dbus.ObjectPath) error {
    f.mu.Lock()
    f.connectCalls++
    f.paths = append(f.paths, path)
    block, err := f.block, f.err
    f.mu.Unlock()

    if block != nil {
        select {
        case <-block:
        case <-ctx.Done():
            return ctx.Err()
        }
    }
    return err
}

func (f *fakeBackend) calls() int {
    f.mu.Lock()
    defer f.mu.Unlock()
    return f.connectCalls
}

func (f *fakeBackend) setBlock(ch chan struct{}) {
    f.mu.Lock()
    f.block = ch
    f.mu.Unlock()
}

func newTestRunner(t *testing.T, b Backend) *Runner {
    t.Helper()
    sigs := []config.Signature{{
        Name:         "meta-glasses",
        CompanyIDs:   []string{"0x0D53"},
        ServiceUUIDs: []string{"0xFD5F"},
        NamePatterns: []string{"Ray-Ban"},
    }}
    m, err := matcher.New(sigs)
    if err != nil {
        t.Fatalf("matcher.New: %v", err)
    }
    cfg := &config.Config{
        GraceSeconds:  15,
        Workers:       2,
        CallTimeoutMs: 500,
        AttemptGapMs:  1,
    }
    r := New(cfg, m, b, log.New(io.Discard, "", 0))
    r.tick = 5 * time.Millisecond
    return r
}

func matchingDevice(path string, connected bool) bluez.DeviceSnapshot {
    return bluez.DeviceSnapshot{
        Path:             dbus.ObjectPath(path),
        Address:          "AA:BB:CC:DD:EE:FF",
        Name:             "Ray-Ban Meta",
        Connected:        connected,
        ManufacturerData: map[uint16][]byte{0x0D53: {0x01}},
    }
}

func TestObserveStartsHammering(t *testing.T) {
    b := &fakeBackend{err: context.DeadlineExceeded}
    r := newTestRunner(t, b)
    defer r.shutdown()

    r.observe(context.Background(), matchingDevice("/org/bluez/hci0/dev_AA", false))

    deadline := time.Now().Add(time.Second)
    for b.calls() == 0 {
        if time.Now().After(deadline) {
            t.Fatal("expected connect attempts after observing a matching device")
        }
        time.Sleep(time.Millisecond)
    }
}

func TestObserveIgnoresNonMatchingDevice(t *testing.T) {
    b := &fakeBackend{}
    r := newTestRunner(t, b)
    defer r.shutdown()

    other := bluez.DeviceSnapshot{
        Path:    "/org/bluez/hci0/dev_11",
        Address: "11:22:33:44:55:66",
        Name:    "Sony WH-1000XM4",
    }
    r.observe(context.Background(), other)
    time.Sleep(20 * time.Millisecond)

    if got := b.calls(); got != 0 {
        t.Fatalf("connect calls = %d, want 0 for non-matching device", got)
    }
    r.mu.Lock()
    n := len(r.targets)
    r.mu.Unlock()
    if n != 0 {
        t.Fatalf("targets = %d, want 0", n)
    }
}

func TestHammeringIdlesWhileConnected(t *testing.T) {
    b := &fakeBackend{err: context.DeadlineExceeded}
    r := newTestRunner(t, b)
    defer r.shutdown()

    r.observe(context.Background(), matchingDevice("/org/bluez/hci0/dev_AA", true))
    time.Sleep(30 * time.Millisecond)

    if got := b.calls(); got != 0 {
        t.Fatalf("connect calls = %d, want 0 while device reports connected", got)
    }
}

func TestHammeringResumesAfterDisconnect(t *testing.T) {
    b := &fakeBackend{err: context.DeadlineExceeded}
    r := newTestRunner(t, b)
    defer r.shutdown()

    r.observe(context.Background(), matchingDevice("/org/bluez/hci0/dev_AA", true))
    time.Sleep(20 * time.Millisecond)
    if got := b.calls(); got != 0 {
        t.Fatalf("connect calls = %d while connected, want 0", got)
    }

    r.observe(context.Background(), matchingDevice("/org/bluez/hci0/dev_AA", false))

    deadline := time.Now().Add(time.Second)
    for b.calls() == 0 {
        if time.Now().After(deadline) {
            t.Fatal("expected hammering to resume after disconnect")
        }
        time.Sleep(time.Millisecond)
    }
}

func TestReapReleasesTargetAfterGraceWindow(t *testing.T) {
    b := &fakeBackend{err: context.DeadlineExceeded}
    r := newTestRunner(t, b)
    defer r.shutdown()

    r.observe(context.Background(), matchingDevice("/org/bluez/hci0/dev_AA", false))

    deadline := time.Now().Add(time.Second)
    for b.calls() == 0 {
        if time.Now().After(deadline) {
            t.Fatal("expected hammering before grace expiry")
        }
        time.Sleep(time.Millisecond)
    }

    r.mu.Lock()
    for _, tgt := range r.targets {
        tgt.lastSeen.Store(time.Now().Add(-time.Hour).UnixNano())
    }
    r.mu.Unlock()

    r.reap()

    r.mu.Lock()
    n := len(r.targets)
    r.mu.Unlock()
    if n != 0 {
        t.Fatalf("targets = %d after grace expiry, want 0", n)
    }

    before := b.calls()
    time.Sleep(30 * time.Millisecond)
    if after := b.calls(); after != before {
        t.Fatalf("connect calls grew after release: %d -> %d", before, after)
    }
}

func TestForgetStopsHammering(t *testing.T) {
    b := &fakeBackend{err: context.DeadlineExceeded}
    r := newTestRunner(t, b)
    defer r.shutdown()

    r.observe(context.Background(), matchingDevice("/org/bluez/hci0/dev_AA", false))

    deadline := time.Now().Add(time.Second)
    for b.calls() == 0 {
        if time.Now().After(deadline) {
            t.Fatal("expected hammering before forget")
        }
        time.Sleep(time.Millisecond)
    }

    r.forget("/org/bluez/hci0/dev_AA")

    before := b.calls()
    time.Sleep(30 * time.Millisecond)
    if after := b.calls(); after != before {
        t.Fatalf("connect calls grew after forget: %d -> %d", before, after)
    }
}

func TestConcurrentWorkersIssueParallelAttempts(t *testing.T) {
    b := &fakeBackend{}
    b.setBlock(make(chan struct{}))
    r := newTestRunner(t, b)
    defer r.shutdown()

    r.observe(context.Background(), matchingDevice("/org/bluez/hci0/dev_AA", false))

    deadline := time.Now().Add(time.Second)
    for b.calls() < 2 {
        if time.Now().After(deadline) {
            t.Fatalf("connect calls = %d, want >= 2 concurrent attempts", b.calls())
        }
        time.Sleep(time.Millisecond)
    }
}

func TestObserveIsIdempotentForSamePath(t *testing.T) {
    b := &fakeBackend{err: context.DeadlineExceeded}
    r := newTestRunner(t, b)
    defer r.shutdown()

    r.observe(context.Background(), matchingDevice("/org/bluez/hci0/dev_AA", false))
    r.observe(context.Background(), matchingDevice("/org/bluez/hci0/dev_AA", false))
    r.observe(context.Background(), matchingDevice("/org/bluez/hci0/dev_AA", false))

    r.mu.Lock()
    n := len(r.targets)
    r.mu.Unlock()
    if n != 1 {
        t.Fatalf("targets = %d, want 1 after repeated observes", n)
    }
}
