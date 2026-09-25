package config

import (
    "os"
    "path/filepath"
    "testing"
    "time"
)

func writeFile(t *testing.T, body string) string {
    t.Helper()
    p := filepath.Join(t.TempDir(), "cfg.json")
    if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
        t.Fatalf("write: %v", err)
    }
    return p
}

func TestLoadAppliesDefaults(t *testing.T) {
    p := writeFile(t, `{"signatures":[{"name":"meta","company_ids":["0x0D53"]}]}`)
    c, err := Load(p)
    if err != nil {
        t.Fatalf("Load: %v", err)
    }
    if c.Adapter != "hci0" {
        t.Errorf("Adapter = %q, want hci0", c.Adapter)
    }
    if c.GraceSeconds != 15 {
        t.Errorf("GraceSeconds = %d, want 15", c.GraceSeconds)
    }
    if c.Workers != 4 {
        t.Errorf("Workers = %d, want 4", c.Workers)
    }
    if c.CallTimeoutMs != 5000 {
        t.Errorf("CallTimeoutMs = %d, want 5000", c.CallTimeoutMs)
    }
    if c.AttemptGapMs != 0 {
        t.Errorf("AttemptGapMs = %d, want 0", c.AttemptGapMs)
    }
}

func TestLoadRejectsNoSignatures(t *testing.T) {
    p := writeFile(t, `{"signatures":[]}`)
    if _, err := Load(p); err == nil {
        t.Fatal("expected error for empty signatures")
    }
}

func TestLoadRejectsSignatureWithNoMatchers(t *testing.T) {
    p := writeFile(t, `{"signatures":[{"name":"meta"}]}`)
    if _, err := Load(p); err == nil {
        t.Fatal("expected error for signature with no match fields")
    }
}

func TestLoadRejectsBadJSON(t *testing.T) {
    p := writeFile(t, `{"signatures":`)
    if _, err := Load(p); err == nil {
        t.Fatal("expected error for malformed json")
    }
}

func TestDurations(t *testing.T) {
    c := &Config{GraceSeconds: 3, CallTimeoutMs: 250, AttemptGapMs: 20}
    if c.Grace() != 3*time.Second {
        t.Errorf("Grace = %v, want 3s", c.Grace())
    }
    if c.CallTimeout() != 250*time.Millisecond {
        t.Errorf("CallTimeout = %v, want 250ms", c.CallTimeout())
    }
    if c.AttemptGap() != 20*time.Millisecond {
        t.Errorf("AttemptGap = %v, want 20ms", c.AttemptGap())
    }
}

func TestParseCompanyIDs(t *testing.T) {
    ids, err := ParseCompanyIDs([]string{"0x0D53", "0d53", "0x01AB"})
    if err != nil {
        t.Fatalf("ParseCompanyIDs: %v", err)
    }
    want := []uint16{0x0D53, 0x0D53, 0x01AB}
    for i := range want {
        if ids[i] != want[i] {
            t.Errorf("ids[%d] = 0x%04X, want 0x%04X", i, ids[i], want[i])
        }
    }
}

func TestParseCompanyIDsRejectsGarbage(t *testing.T) {
    if _, err := ParseCompanyIDs([]string{"zz"}); err == nil {
        t.Fatal("expected error for non-hex company id")
    }
    if _, err := ParseCompanyIDs([]string{"0x"}); err == nil {
        t.Fatal("expected error for empty company id")
    }
    if _, err := ParseCompanyIDs([]string{"0x1FFFF"}); err == nil {
        t.Fatal("expected error for overflowing company id")
    }
}

func TestNormalizeUUID(t *testing.T) {
    cases := map[string]string{
        "0xFD5F": "0000fd5f-0000-1000-8000-00805f9b34fb",
        "FD5F":   "0000fd5f-0000-1000-8000-00805f9b34fb",
        "0000FD5F-0000-1000-8000-00805F9B34FB": "0000fd5f-0000-1000-8000-00805f9b34fb",
    }
    for in, want := range cases {
        if got := NormalizeUUID(in); got != want {
            t.Errorf("NormalizeUUID(%q) = %q, want %q", in, got, want)
        }
    }
}
