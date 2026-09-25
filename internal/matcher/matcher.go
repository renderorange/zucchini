package matcher

import (
    "fmt"
    "regexp"

    "zucchini/internal/config"
)

// Device is the flat view of a BlueZ device that matching needs.
type Device struct {
    Path             string
    Address          string
    Name             string
    Alias            string
    UUIDs            []string
    ManufacturerData map[uint16][]byte
    ServiceData      map[string][]byte
}

type Matcher struct {
    rules []rule
}

type rule struct {
    name         string
    companyIDs   map[uint16]bool
    serviceUUIDs map[string]bool
    namePatterns []*regexp.Regexp
}

func New(sigs []config.Signature) (*Matcher, error) {
    if len(sigs) == 0 {
        return nil, fmt.Errorf("at least one signature is required")
    }
    m := &Matcher{rules: make([]rule, 0, len(sigs))}
    for _, s := range sigs {
        r := rule{
            name:         s.Name,
            companyIDs:   map[uint16]bool{},
            serviceUUIDs: map[string]bool{},
        }
        ids, err := config.ParseCompanyIDs(s.CompanyIDs)
        if err != nil {
            return nil, fmt.Errorf("signature %q: %w", s.Name, err)
        }
        for _, id := range ids {
            r.companyIDs[id] = true
        }
        for _, u := range s.ServiceUUIDs {
            r.serviceUUIDs[config.NormalizeUUID(u)] = true
        }
        for _, p := range s.NamePatterns {
            re, err := regexp.Compile("(?i)" + p)
            if err != nil {
                return nil, fmt.Errorf("signature %q: bad pattern %q: %w", s.Name, p, err)
            }
            r.namePatterns = append(r.namePatterns, re)
        }
        m.rules = append(m.rules, r)
    }
    return m, nil
}

// Match returns the name of the first signature rule that matches the device.
func (m *Matcher) Match(d Device) (string, bool) {
    for i := range m.rules {
        if m.rules[i].matches(d) {
            return m.rules[i].name, true
        }
    }
    return "", false
}

func (r *rule) matches(d Device) bool {
    for id := range d.ManufacturerData {
        if r.companyIDs[id] {
            return true
        }
    }
    for u := range d.ServiceData {
        if r.serviceUUIDs[config.NormalizeUUID(u)] {
            return true
        }
    }
    for _, u := range d.UUIDs {
        if r.serviceUUIDs[config.NormalizeUUID(u)] {
            return true
        }
    }
    for _, n := range []string{d.Name, d.Alias} {
        if n == "" {
            continue
        }
        for _, re := range r.namePatterns {
            if re.MatchString(n) {
                return true
            }
        }
    }
    return false
}
