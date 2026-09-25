package bluez

import (
    "github.com/godbus/dbus/v5"
)

func snapshot(path dbus.ObjectPath, props map[string]dbus.Variant) DeviceSnapshot {
    d := DeviceSnapshot{
        Path:             path,
        ManufacturerData: map[uint16][]byte{},
        ServiceData:      map[string][]byte{},
    }
    if v, ok := props["Address"]; ok {
        d.Address, _ = v.Value().(string)
    }
    if v, ok := props["Name"]; ok {
        d.Name, _ = v.Value().(string)
    }
    if v, ok := props["Alias"]; ok {
        d.Alias, _ = v.Value().(string)
    }
    if v, ok := props["Connected"]; ok {
        d.Connected, _ = v.Value().(bool)
    }
    if v, ok := props["UUIDs"]; ok {
        d.UUIDs, _ = v.Value().([]string)
    }
    if v, ok := props["ManufacturerData"]; ok {
        if m, ok := v.Value().(map[uint16]dbus.Variant); ok {
            for k, inner := range m {
                if b, ok := inner.Value().([]byte); ok {
                    d.ManufacturerData[k] = b
                }
            }
        }
    }
    if v, ok := props["ServiceData"]; ok {
        if m, ok := v.Value().(map[string]dbus.Variant); ok {
            for k, inner := range m {
                if b, ok := inner.Value().([]byte); ok {
                    d.ServiceData[k] = b
                }
            }
        }
    }
    return d
}

// parseInterfacesAdded decodes ObjectManager.InterfacesAdded. Only Device1
// interfaces are reported; anything else is ignored.
func parseInterfacesAdded(s *dbus.Signal) (Event, bool) {
    if s == nil || len(s.Body) < 2 {
        return Event{}, false
    }
    path, ok := s.Body[0].(dbus.ObjectPath)
    if !ok {
        return Event{}, false
    }
    ifaces, ok := s.Body[1].(map[string]map[string]dbus.Variant)
    if !ok {
        return Event{}, false
    }
    props, ok := ifaces[DeviceIface]
    if !ok {
        return Event{}, false
    }
    return Event{Type: EventAdded, Device: snapshot(path, props)}, true
}

// parseInterfacesRemoved decodes ObjectManager.InterfacesRemoved. It reports a
// removal only when Device1 is among the dropped interfaces.
func parseInterfacesRemoved(s *dbus.Signal) (Event, bool) {
    if s == nil || len(s.Body) < 2 {
        return Event{}, false
    }
    path, ok := s.Body[0].(dbus.ObjectPath)
    if !ok {
        return Event{}, false
    }
    ifaces, ok := s.Body[1].([]string)
    if !ok {
        return Event{}, false
    }
    for _, iface := range ifaces {
        if iface == DeviceIface {
            return Event{Type: EventRemoved, Device: DeviceSnapshot{Path: path}}, true
        }
    }
    return Event{}, false
}

// parsePropertiesChanged decodes Properties.PropertiesChanged and returns the
// object path, the interface name, and the changed property set. Callers that
// need the full property set must fetch it separately: the signal carries only
// a delta.
func parsePropertiesChanged(s *dbus.Signal) (dbus.ObjectPath, string, map[string]dbus.Variant, bool) {
    if s == nil || len(s.Body) < 2 {
        return "", "", nil, false
    }
    iface, ok := s.Body[0].(string)
    if !ok {
        return "", "", nil, false
    }
    changed, ok := s.Body[1].(map[string]dbus.Variant)
    if !ok {
        return "", "", nil, false
    }
    return s.Path, iface, changed, true
}

func (c *Client) translate(s *dbus.Signal) (Event, bool) {
    if s == nil {
        return Event{}, false
    }
    switch s.Name {
    case ObjectManagerIface + ".InterfacesAdded":
        return parseInterfacesAdded(s)
    case ObjectManagerIface + ".InterfacesRemoved":
        return parseInterfacesRemoved(s)
    case PropsIface + ".PropertiesChanged":
        path, iface, _, ok := parsePropertiesChanged(s)
        if !ok || iface != DeviceIface {
            return Event{}, false
        }
        // PropertiesChanged carries only the delta; fetch the full set so the
        // matcher always sees complete ManufacturerData/ServiceData.
        snap, err := c.GetDevice(path)
        if err != nil {
            return Event{}, false
        }
        return Event{Type: EventChanged, Device: snap}, true
    }
    return Event{}, false
}
