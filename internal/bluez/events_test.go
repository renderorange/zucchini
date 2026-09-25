package bluez

import (
    "testing"

    "github.com/godbus/dbus/v5"
)

// These tests pin our parsing of the documented BlueZ D-Bus interfaces
// (ObjectManager.InterfacesAdded/Removed, Properties.PropertiesChanged) using
// synthetic signals. No live bus is involved: we verify the wire shape we
// expect BlueZ to emit and how we decode it.

func TestParseInterfacesAddedDevice(t *testing.T) {
    sig := &dbus.Signal{
        Name: "org.freedesktop.DBus.ObjectManager.InterfacesAdded",
        Path: "/org/bluez/hci0",
        Body: []interface{}{
            dbus.ObjectPath("/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF"),
            map[string]map[string]dbus.Variant{
                "org.bluez.Device1": {
                    "Address":   dbus.MakeVariant("AA:BB:CC:DD:EE:FF"),
                    "Name":      dbus.MakeVariant("Ray-Ban Meta"),
                    "Alias":     dbus.MakeVariant("Ray-Ban Meta"),
                    "Connected": dbus.MakeVariant(false),
                    "UUIDs":     dbus.MakeVariant([]string{"0000fd5f-0000-1000-8000-00805f9b34fb"}),
                    "ManufacturerData": dbus.MakeVariant(map[uint16]dbus.Variant{
                        0x0D53: dbus.MakeVariant([]byte{0x01, 0x02}),
                    }),
                    "ServiceData": dbus.MakeVariant(map[string]dbus.Variant{
                        "0000fd5f-0000-1000-8000-00805f9b34fb": dbus.MakeVariant([]byte{0xAA}),
                    }),
                },
            },
        },
    }

    ev, ok := parseInterfacesAdded(sig)
    if !ok {
        t.Fatal("parseInterfacesAdded returned ok=false, want true")
    }
    if ev.Type != EventAdded {
        t.Errorf("Type = %v, want EventAdded", ev.Type)
    }
    d := ev.Device
    if d.Path != "/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF" {
        t.Errorf("Path = %q", d.Path)
    }
    if d.Address != "AA:BB:CC:DD:EE:FF" {
        t.Errorf("Address = %q", d.Address)
    }
    if d.Name != "Ray-Ban Meta" {
        t.Errorf("Name = %q", d.Name)
    }
    if d.Connected {
        t.Error("Connected = true, want false")
    }
    if len(d.UUIDs) != 1 || d.UUIDs[0] != "0000fd5f-0000-1000-8000-00805f9b34fb" {
        t.Errorf("UUIDs = %v", d.UUIDs)
    }
    if got := d.ManufacturerData[0x0D53]; len(got) != 2 {
        t.Errorf("ManufacturerData[0x0D53] = %v, want 2 bytes", got)
    }
    if got := d.ServiceData["0000fd5f-0000-1000-8000-00805f9b34fb"]; len(got) != 1 {
        t.Errorf("ServiceData = %v, want 1 byte", got)
    }
}

func TestParseInterfacesAddedIgnoresNonDeviceInterface(t *testing.T) {
    sig := &dbus.Signal{
        Name: "org.freedesktop.DBus.ObjectManager.InterfacesAdded",
        Body: []interface{}{
            dbus.ObjectPath("/org/bluez/hci0/dev_AA"),
            map[string]map[string]dbus.Variant{
                "org.bluez.Battery1": {
                    "Percentage": dbus.MakeVariant(uint8(80)),
                },
            },
        },
    }
    if _, ok := parseInterfacesAdded(sig); ok {
        t.Fatal("expected ok=false for non-Device1 interface")
    }
}

func TestParseInterfacesAddedRejectsMalformedBody(t *testing.T) {
    cases := []struct {
        name string
        body []interface{}
    }{
        {"empty", nil},
        {"one element", []interface{}{dbus.ObjectPath("/x")}},
        {"path not objectpath", []interface{}{"not-a-path", map[string]map[string]dbus.Variant{}}},
        {"ifaces not a map", []interface{}{dbus.ObjectPath("/x"), "nope"}},
        {"missing Device1", []interface{}{
            dbus.ObjectPath("/x"),
            map[string]map[string]dbus.Variant{},
        }},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            sig := &dbus.Signal{
                Name: "org.freedesktop.DBus.ObjectManager.InterfacesAdded",
                Body: tc.body,
            }
            if _, ok := parseInterfacesAdded(sig); ok {
                t.Fatal("expected ok=false for malformed body")
            }
        })
    }
}

func TestParseInterfacesRemovedDevice(t *testing.T) {
    sig := &dbus.Signal{
        Name: "org.freedesktop.DBus.ObjectManager.InterfacesRemoved",
        Body: []interface{}{
            dbus.ObjectPath("/org/bluez/hci0/dev_AA"),
            []string{"org.bluez.Device1", "org.bluez.Battery1"},
        },
    }
    ev, ok := parseInterfacesRemoved(sig)
    if !ok {
        t.Fatal("parseInterfacesRemoved returned ok=false, want true")
    }
    if ev.Type != EventRemoved {
        t.Errorf("Type = %v, want EventRemoved", ev.Type)
    }
    if ev.Device.Path != "/org/bluez/hci0/dev_AA" {
        t.Errorf("Path = %q", ev.Device.Path)
    }
}

func TestParseInterfacesRemovedIgnoresOtherInterfaces(t *testing.T) {
    sig := &dbus.Signal{
        Name: "org.freedesktop.DBus.ObjectManager.InterfacesRemoved",
        Body: []interface{}{
            dbus.ObjectPath("/org/bluez/hci0/dev_AA"),
            []string{"org.bluez.Battery1"},
        },
    }
    if _, ok := parseInterfacesRemoved(sig); ok {
        t.Fatal("expected ok=false when Device1 is not removed")
    }
}

func TestParseInterfacesRemovedRejectsMalformedBody(t *testing.T) {
    cases := []struct {
        name string
        body []interface{}
    }{
        {"empty", nil},
        {"wrong types", []interface{}{42, 42}},
        {"ifaces not string slice", []interface{}{dbus.ObjectPath("/x"), []int{1}}},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            sig := &dbus.Signal{
                Name: "org.freedesktop.DBus.ObjectManager.InterfacesRemoved",
                Body: tc.body,
            }
            if _, ok := parseInterfacesRemoved(sig); ok {
                t.Fatal("expected ok=false for malformed body")
            }
        })
    }
}

func TestParsePropertiesChangedDevice(t *testing.T) {
    sig := &dbus.Signal{
        Name: "org.freedesktop.DBus.Properties.PropertiesChanged",
        Path: "/org/bluez/hci0/dev_AA",
        Body: []interface{}{
            "org.bluez.Device1",
            map[string]dbus.Variant{
                "Connected": dbus.MakeVariant(true),
                "RSSI":      dbus.MakeVariant(int16(-42)),
            },
            []string{},
        },
    }
    path, iface, changed, ok := parsePropertiesChanged(sig)
    if !ok {
        t.Fatal("parsePropertiesChanged returned ok=false, want true")
    }
    if path != "/org/bluez/hci0/dev_AA" {
        t.Errorf("path = %q", path)
    }
    if iface != "org.bluez.Device1" {
        t.Errorf("iface = %q", iface)
    }
    if v, ok := changed["Connected"]; !ok {
        t.Error("missing Connected in changed props")
    } else if connected, _ := v.Value().(bool); !connected {
        t.Error("Connected = false, want true")
    }
}

func TestParsePropertiesChangedReturnsAnyInterface(t *testing.T) {
    // The parser decodes shape only; filtering to Device1 is translate's policy.
    sig := &dbus.Signal{
        Name: "org.freedesktop.DBus.Properties.PropertiesChanged",
        Body: []interface{}{
            "org.bluez.Battery1",
            map[string]dbus.Variant{"Percentage": dbus.MakeVariant(uint8(10))},
            []string{},
        },
    }
    path, iface, changed, ok := parsePropertiesChanged(sig)
    if !ok {
        t.Fatal("expected ok=true, parser decodes any interface")
    }
    if iface != "org.bluez.Battery1" {
        t.Errorf("iface = %q, want org.bluez.Battery1", iface)
    }
    if path != "" {
        t.Errorf("path = %q", path)
    }
    if _, ok := changed["Percentage"]; !ok {
        t.Error("missing Percentage in changed props")
    }
}

func TestTranslateIgnoresPropertiesChangedForOtherInterfaces(t *testing.T) {
    // Safe without a live bus: translate returns before it fetches anything
    // once it sees a non-Device1 interface.
    c := &Client{}
    sig := &dbus.Signal{
        Name: PropsIface + ".PropertiesChanged",
        Path: "/org/bluez/hci0/dev_AA",
        Body: []interface{}{
            "org.bluez.Battery1",
            map[string]dbus.Variant{"Percentage": dbus.MakeVariant(uint8(10))},
            []string{},
        },
    }
    if _, ok := c.translate(sig); ok {
        t.Fatal("translate should ignore non-Device1 PropertiesChanged")
    }
}

func TestTranslateIgnoresUnknownSignalNames(t *testing.T) {
    c := &Client{}
    if _, ok := c.translate(&dbus.Signal{Name: "org.bluez.Adapter1.SomethingChanged"}); ok {
        t.Fatal("translate should ignore unknown signals")
    }
    if _, ok := c.translate(nil); ok {
        t.Fatal("translate should reject nil signal")
    }
}

func TestParsePropertiesChangedRejectsMalformedBody(t *testing.T) {
    cases := []struct {
        name string
        body []interface{}
    }{
        {"empty", nil},
        {"missing props", []interface{}{"org.bluez.Device1"}},
        {"wrong iface type", []interface{}{42, map[string]dbus.Variant{}, []string{}}},
        {"wrong props type", []interface{}{"org.bluez.Device1", "nope", []string{}}},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            sig := &dbus.Signal{
                Name: "org.freedesktop.DBus.Properties.PropertiesChanged",
                Body: tc.body,
            }
            if _, _, _, ok := parsePropertiesChanged(sig); ok {
                t.Fatal("expected ok=false for malformed body")
            }
        })
    }
}

func TestSnapshotDecodesManufacturerAndServiceData(t *testing.T) {
    props := map[string]dbus.Variant{
        "Address":   dbus.MakeVariant("AA:BB:CC:DD:EE:FF"),
        "Connected": dbus.MakeVariant(true),
        "ManufacturerData": dbus.MakeVariant(map[uint16]dbus.Variant{
            0x0D53: dbus.MakeVariant([]byte{0x01}),
            0x01AB: dbus.MakeVariant([]byte{0x02, 0x03}),
        }),
        "ServiceData": dbus.MakeVariant(map[string]dbus.Variant{
            "0000fd5f-0000-1000-8000-00805f9b34fb": dbus.MakeVariant([]byte{0xFF}),
        }),
    }
    d := snapshot("/org/bluez/hci0/dev_AA", props)

    if !d.Connected {
        t.Error("Connected = false, want true")
    }
    if len(d.ManufacturerData) != 2 {
        t.Errorf("ManufacturerData len = %d, want 2", len(d.ManufacturerData))
    }
    if len(d.ManufacturerData[0x0D53]) != 1 || len(d.ManufacturerData[0x01AB]) != 2 {
        t.Errorf("ManufacturerData = %v", d.ManufacturerData)
    }
    if len(d.ServiceData["0000fd5f-0000-1000-8000-00805f9b34fb"]) != 1 {
        t.Errorf("ServiceData = %v", d.ServiceData)
    }
}

func TestSnapshotToleratesEmptyAndWrongTypes(t *testing.T) {
    d := snapshot("/x", map[string]dbus.Variant{
        "Address":          dbus.MakeVariant(42),
        "ManufacturerData": dbus.MakeVariant("not-a-map"),
    })
    if d.Address != "" {
        t.Errorf("Address = %q, want empty on type mismatch", d.Address)
    }
    if d.ManufacturerData == nil {
        t.Error("ManufacturerData should be non-nil empty map")
    }
    if len(d.ManufacturerData) != 0 {
        t.Errorf("ManufacturerData = %v, want empty", d.ManufacturerData)
    }

    empty := snapshot("/x", nil)
    if empty.ManufacturerData == nil || empty.ServiceData == nil {
        t.Error("maps should be initialized for nil props")
    }
}
