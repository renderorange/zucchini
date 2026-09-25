package matcher

import (
    "testing"

    "zucchini/internal/config"
)

func metaSigs(t *testing.T) []config.Signature {
    t.Helper()
    return []config.Signature{
        {
            Name:         "meta-glasses",
            CompanyIDs:   []string{"0x0D53", "0x01AB", "0x058E"},
            ServiceUUIDs: []string{"0xFD5F"},
            NamePatterns: []string{"Ray-Ban", "Oakley Meta"},
        },
    }
}

func newMatcher(t *testing.T) *Matcher {
    t.Helper()
    m, err := New(metaSigs(t))
    if err != nil {
        t.Fatalf("New: %v", err)
    }
    return m
}

func TestMatchByCompanyID(t *testing.T) {
    m := newMatcher(t)
    d := Device{
        Address:          "AA:BB:CC:DD:EE:FF",
        ManufacturerData: map[uint16][]byte{0x0D53: {0x01, 0x02}},
    }
    name, ok := m.Match(d)
    if !ok {
        t.Fatal("expected match by company id, got none")
    }
    if name != "meta-glasses" {
        t.Fatalf("name = %q, want meta-glasses", name)
    }
}

func TestMatchByServiceUUIDShortForm(t *testing.T) {
    m := newMatcher(t)
    // BlueZ reports the 128-bit expansion; config uses the 16-bit form.
    d := Device{
        Address: "AA:BB:CC:DD:EE:FF",
        UUIDs:   []string{"0000fd5f-0000-1000-8000-00805f9b34fb"},
    }
    if _, ok := m.Match(d); !ok {
        t.Fatal("expected match by service uuid")
    }
}

func TestMatchByNamePattern(t *testing.T) {
    m := newMatcher(t)
    d := Device{Address: "AA:BB:CC:DD:EE:FF", Name: "Ray-Ban Meta"}
    if _, ok := m.Match(d); !ok {
        t.Fatal("expected match by name pattern")
    }
}

func TestNoMatchOnUnrelatedDevice(t *testing.T) {
    m := newMatcher(t)
    d := Device{
        Address:          "11:22:33:44:55:66",
        Name:             "Sony WH-1000XM4",
        ManufacturerData: map[uint16][]byte{0x004C: {0x01}},
    }
    if _, ok := m.Match(d); ok {
        t.Fatal("expected no match on unrelated device")
    }
}

func TestNoMatchWhenEmpty(t *testing.T) {
    m := newMatcher(t)
    if _, ok := m.Match(Device{Address: "11:22:33:44:55:66"}); ok {
        t.Fatal("expected no match on empty device")
    }
}

func TestEmptySignatureListFailsToBuild(t *testing.T) {
    if _, err := New(nil); err == nil {
        // New(nil) builds zero rules; matching then always misses. Guard the
        // config layer instead and ensure empty rules never match.
        t.Fatal("expected error for empty signature list")
    }
}

func TestBadCompanyIDFailsToBuild(t *testing.T) {
    _, err := New([]config.Signature{{
        Name:       "bad",
        CompanyIDs: []string{"not-hex"},
    }})
    if err == nil {
        t.Fatal("expected error for bad company id")
    }
}

func TestBadNamePatternFailsToBuild(t *testing.T) {
    _, err := New([]config.Signature{{
        Name:         "bad",
        NamePatterns: []string{"[unclosed"},
    }})
    if err == nil {
        t.Fatal("expected error for bad regex")
    }
}
