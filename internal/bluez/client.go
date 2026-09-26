package bluez

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	Service            = "org.bluez"
	AdapterIface       = "org.bluez.Adapter1"
	DeviceIface        = "org.bluez.Device1"
	PropsIface         = "org.freedesktop.DBus.Properties"
	ObjectManagerIface = "org.freedesktop.DBus.ObjectManager"
	DBusIface          = "org.freedesktop.DBus"
)

const (
	// getDeviceTimeout bounds one full-property fetch inside the event
	// translator, so a wedged bluetoothd cannot stall the pipeline forever.
	getDeviceTimeout = 2 * time.Second

	// opTimeout bounds one-shot BlueZ calls made outside the event pipeline
	// (discovery control, GetManagedObjects). Exceeding it surfaces an error
	// that main turns into exit 1, so systemd restarts the daemon.
	opTimeout = 3 * time.Second
)

type DeviceSnapshot struct {
	Path             dbus.ObjectPath
	Address          string
	Name             string
	Alias            string
	Connected        bool
	UUIDs            []string
	ManufacturerData map[uint16][]byte
	ServiceData      map[string][]byte
}

type EventType int

const (
	EventAdded EventType = iota
	EventChanged
	EventRemoved
	EventBlueZGone
	EventDiscoveryStopped
)

type Event struct {
	Type   EventType
	Device DeviceSnapshot
}

type Client struct {
	conn *dbus.Conn
}

func Dial() (*Client, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("system bus: %w", err)
	}
	return &Client{conn: conn}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) AdapterPath(name string) (dbus.ObjectPath, error) {
	managed, err := c.managedObjects()
	if err != nil {
		return "", err
	}
	for path, ifaces := range managed {
		if _, ok := ifaces[AdapterIface]; ok {
			if name == "" || strings.HasSuffix(string(path), name) {
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("adapter %q not found", name)
}

func (c *Client) StartDiscovery(adapter dbus.ObjectPath) error {
	obj := c.conn.Object(Service, adapter)
	filter := map[string]dbus.Variant{
		"Transport":     dbus.MakeVariant("auto"),
		"DuplicateData": dbus.MakeVariant(true),
	}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	if err := obj.CallWithContext(ctx, AdapterIface+".SetDiscoveryFilter", 0, filter).Err; err != nil {
		return fmt.Errorf("set discovery filter: %w", err)
	}
	if err := obj.CallWithContext(ctx, AdapterIface+".StartDiscovery", 0).Err; err != nil {
		return fmt.Errorf("start discovery: %w", err)
	}
	return nil
}

func (c *Client) StopDiscovery(adapter dbus.ObjectPath) error {
	obj := c.conn.Object(Service, adapter)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	return obj.CallWithContext(ctx, AdapterIface+".StopDiscovery", 0).Err
}

// Connect issues a BlueZ Device1.Connect. The call blocks until BlueZ answers
// or ctx is cancelled. Concurrent calls against the same path are expected.
func (c *Client) Connect(ctx context.Context, path dbus.ObjectPath) error {
	obj := c.conn.Object(Service, path)
	return obj.CallWithContext(ctx, DeviceIface+".Connect", 0).Err
}

func (c *Client) Disconnect(path dbus.ObjectPath) error {
	obj := c.conn.Object(Service, path)
	return obj.Call(DeviceIface+".Disconnect", 0).Err
}

func (c *Client) Devices() ([]DeviceSnapshot, error) {
	managed, err := c.managedObjects()
	if err != nil {
		return nil, err
	}
	out := []DeviceSnapshot{}
	for path, ifaces := range managed {
		props, ok := ifaces[DeviceIface]
		if !ok {
			continue
		}
		out = append(out, snapshot(path, props))
	}
	return out, nil
}

// Events streams added/changed/removed device events. The stop func releases
// the subscription and closes the channel. ctx bounds the per-event device
// property fetches done inside the translator.
func (c *Client) Events(ctx context.Context) (<-chan Event, func(), error) {
	sig := make(chan *dbus.Signal, 256)
	c.conn.Signal(sig)

	fail := func(err error) (<-chan Event, func(), error) {
		c.conn.RemoveSignal(sig)
		return nil, nil, err
	}

	if err := c.conn.AddMatchSignal(
		dbus.WithMatchInterface(PropsIface),
		dbus.WithMatchMember("PropertiesChanged"),
		dbus.WithMatchArg(0, DeviceIface),
	); err != nil {
		return fail(err)
	}
	if err := c.conn.AddMatchSignal(
		dbus.WithMatchInterface(ObjectManagerIface),
		dbus.WithMatchMember("InterfacesAdded"),
	); err != nil {
		return fail(err)
	}
	if err := c.conn.AddMatchSignal(
		dbus.WithMatchInterface(ObjectManagerIface),
		dbus.WithMatchMember("InterfacesRemoved"),
	); err != nil {
		return fail(err)
	}
	if err := c.conn.AddMatchSignal(
		dbus.WithMatchInterface(DBusIface),
		dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg(0, Service),
	); err != nil {
		return fail(err)
	}

	out := make(chan Event, 256)
	done := make(chan struct{})

	go func() {
		defer close(out)
		for {
			select {
			case <-done:
				return
			case s, ok := <-sig:
				if !ok {
					return
				}
				ev, ok := c.translate(ctx, s)
				if !ok {
					continue
				}
				select {
				case out <- ev:
				case <-done:
					return
				}
			}
		}
	}()

	stop := func() {
		select {
		case <-done:
		default:
			close(done)
		}
		c.conn.RemoveSignal(sig)
	}
	return out, stop, nil
}

func (c *Client) managedObjects() (map[dbus.ObjectPath]map[string]map[string]dbus.Variant, error) {
	obj := c.conn.Object(Service, "/")
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	var managed map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	if err := obj.CallWithContext(ctx, ObjectManagerIface+".GetManagedObjects", 0).Store(&managed); err != nil {
		return nil, fmt.Errorf("get managed objects: %w", err)
	}
	return managed, nil
}

// GetDevice fetches the full current Device1 property set for one object path.
func (c *Client) GetDevice(ctx context.Context, path dbus.ObjectPath) (DeviceSnapshot, error) {
	obj := c.conn.Object(Service, path)
	var props map[string]dbus.Variant
	if err := obj.CallWithContext(ctx, PropsIface+".GetAll", 0, DeviceIface).Store(&props); err != nil {
		return DeviceSnapshot{}, fmt.Errorf("get device %s: %w", path, err)
	}
	return snapshot(path, props), nil
}
