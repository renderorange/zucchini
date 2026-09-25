package config

import (
    "encoding/json"
    "fmt"
    "os"
    "strconv"
    "strings"
    "time"
)

// Signature describes one family of devices to target.
// A device matches if any listed field hits; fields are optional but at least one is required.
type Signature struct {
    Name         string   `json:"name"`
    CompanyIDs   []string `json:"company_ids"`
    ServiceUUIDs []string `json:"service_uuids"`
    NamePatterns []string `json:"name_patterns"`
}

type Config struct {
    Adapter       string      `json:"adapter"`
    GraceSeconds  int         `json:"grace_seconds"`
    Workers       int         `json:"workers"`
    CallTimeoutMs int         `json:"call_timeout_ms"`
    AttemptGapMs  int         `json:"attempt_gap_ms"`
    Signatures    []Signature `json:"signatures"`
}

func Load(path string) (*Config, error) {
    b, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    var c Config
    if err := json.Unmarshal(b, &c); err != nil {
        return nil, fmt.Errorf("parse %s: %w", path, err)
    }
    c.applyDefaults()
    if err := c.validate(); err != nil {
        return nil, err
    }
    return &c, nil
}

func (c *Config) applyDefaults() {
    if c.Adapter == "" {
        c.Adapter = "hci0"
    }
    if c.GraceSeconds <= 0 {
        c.GraceSeconds = 15
    }
    if c.Workers <= 0 {
        c.Workers = 4
    }
    if c.CallTimeoutMs <= 0 {
        c.CallTimeoutMs = 5000
    }
    if c.AttemptGapMs < 0 {
        c.AttemptGapMs = 0
    }
}

func (c *Config) validate() error {
    if len(c.Signatures) == 0 {
        return fmt.Errorf("at least one signature is required")
    }
    for i, s := range c.Signatures {
        if s.Name == "" {
            return fmt.Errorf("signatures[%d]: name is required", i)
        }
        if len(s.CompanyIDs) == 0 && len(s.ServiceUUIDs) == 0 && len(s.NamePatterns) == 0 {
            return fmt.Errorf("signature %q: need at least one match field", s.Name)
        }
    }
    return nil
}

func (c *Config) Grace() time.Duration {
    return time.Duration(c.GraceSeconds) * time.Second
}

func (c *Config) CallTimeout() time.Duration {
    return time.Duration(c.CallTimeoutMs) * time.Millisecond
}

func (c *Config) AttemptGap() time.Duration {
    return time.Duration(c.AttemptGapMs) * time.Millisecond
}

// ParseCompanyIDs converts hex strings such as "0x0D53" or "0d53" into uint16 values.
func ParseCompanyIDs(raw []string) ([]uint16, error) {
    out := make([]uint16, 0, len(raw))
    for _, s := range raw {
        v, err := parseHexUint16(s)
        if err != nil {
            return nil, err
        }
        out = append(out, v)
    }
    return out, nil
}

func parseHexUint16(s string) (uint16, error) {
    t := strings.TrimSpace(strings.ToLower(s))
    t = strings.TrimPrefix(t, "0x")
    if t == "" {
        return 0, fmt.Errorf("empty company id")
    }
    v, err := strconv.ParseUint(t, 16, 16)
    if err != nil {
        return 0, fmt.Errorf("bad company id %q: %w", s, err)
    }
    return uint16(v), nil
}

// NormalizeUUID expands a 16-bit UUID to its 128-bit Bluetooth base form and lowercases it.
func NormalizeUUID(s string) string {
    t := strings.TrimSpace(strings.ToLower(s))
    t = strings.TrimPrefix(t, "0x")
    if len(t) == 4 {
        return "0000" + t + "-0000-1000-8000-00805f9b34fb"
    }
    return t
}
