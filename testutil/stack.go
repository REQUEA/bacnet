// Package testutil provides in-process BACnet stack helpers for integration tests.
// It uses a virtual in-process Bus instead of real UDP sockets so that tests
// run fast, deterministically, and without port-binding requirements.
package testutil

import (
	"fmt"
	"sync"
	"testing"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/applicationlayer"
	"github.com/REQUEA/bacnet/networklayer"
	"github.com/REQUEA/bacnet/objectmodel"
	"github.com/REQUEA/bacnet/servicelayer"
)

// busMAC is a minimal MAC implementation for in-process BACnet tests.
type busMAC struct {
	addr      []byte
	broadcast bool
}

func (m *busMAC) GetBytes() []byte        { return m.addr }
func (m *busMAC) FromBytes(b []byte)      { m.addr = b }
func (m *busMAC) String() string          { return fmt.Sprintf("%x", m.addr) }
func (m *busMAC) IsBroadcast() bool       { return m.broadcast }
func (m *busMAC) Equal(o bacnet.MAC) bool {
	ob, ok := o.(*busMAC)
	if !ok {
		return false
	}
	return string(m.addr) == string(ob.addr)
}

// Bus is an in-process broadcast-capable message bus that connects multiple
// TestStack instances without requiring real UDP sockets.
type Bus struct {
	mu    sync.RWMutex
	nodes []*busNode
}

type busNode struct {
	addr *bacnet.BACnetAddress
	ae   *applicationlayer.ApplicationEntity
}

func (b *Bus) register(addr *bacnet.BACnetAddress, ae *applicationlayer.ApplicationEntity) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nodes = append(b.nodes, &busNode{addr: addr, ae: ae})
}

// deliver routes a PDU to the appropriate peer(s). When dadr.Mac is nil or
// dadr.Network == BroadcastDNET the PDU is broadcast to all nodes; otherwise
// it is delivered only to the node whose MAC matches dadr.Mac.
func (b *Bus) deliver(src *bacnet.BACnetAddress, dadr *bacnet.BACnetAddress, der bool, payload []byte) {
	b.mu.RLock()
	nodes := make([]*busNode, len(b.nodes))
	copy(nodes, b.nodes)
	b.mu.RUnlock()

	isBroadcast := dadr.Mac == nil || dadr.Network == bacnet.BroadcastDNET

	for _, node := range nodes {
		if node.addr == src {
			continue // never deliver to self
		}

		var shouldDeliver bool
		if isBroadcast {
			shouldDeliver = true
		} else if dadr.Mac != nil {
			shouldDeliver = dadr.Mac.Equal(node.addr.Mac)
		}
		if !shouldDeliver {
			continue
		}

		// Ensure Dest.Mac is non-nil so that handlers can inspect it without
		// panicking (e.g. the confirmed-service-request broadcast-drop check).
		dest := dadr
		if isBroadcast && dadr.Mac == nil {
			dest = &bacnet.BACnetAddress{
				Network: dadr.Network,
				Mac:     &busMAC{broadcast: true},
			}
		}

		cp := make([]byte, len(payload))
		copy(cp, payload)
		ind := &networklayer.NPDUIndication{
			Source:        src,
			Dest:          dest,
			ExpectedReply: der,
			Apdu:          cp,
		}
		ae := node.ae
		go ae.HandleNUnitDataIndication(ind)
	}
}

// busNetwork implements networklayer.NetworkEntity backed by a Bus.
type busNetwork struct {
	selfAddr *bacnet.BACnetAddress
	bus      *Bus
}

func (n *busNetwork) NUnitDataRequest(
	dadr *bacnet.BACnetAddress, der bool,
	_ networklayer.NPDUPriority, payload []byte,
) error {
	n.bus.deliver(n.selfAddr, dadr, der, payload)
	return nil
}

func (n *busNetwork) NReleaseRequest(*bacnet.BACnetAddress) error { return nil }
func (n *busNetwork) GetMaxPDULength(bacnet.NetworkNumber) uint   { return 480 }
func (n *busNetwork) NUnitDataIndication(
	*networklayer.Port, bacnet.MAC, bacnet.MAC, []byte,
) error {
	return nil
}

// TestStack is a fully wired BACnet stack (ApplicationEntity + ServiceHandler)
// connected to a Bus. Use NewTestStack to create one.
type TestStack struct {
	addr *bacnet.BACnetAddress
	ae   *applicationlayer.ApplicationEntity
	sh   *servicelayer.ServiceHandler
}

// NewTestStack creates a TestStack with the given device instance and name,
// wires it to the provided Bus, and registers it for message delivery.
//
// instanceId determines both the device object instance number and the node's
// in-process MAC address. Use distinct instance IDs for each stack on a Bus.
func NewTestStack(t *testing.T, bus *Bus, instanceId uint32, deviceName string) *TestStack {
	t.Helper()
	addr := &bacnet.BACnetAddress{
		Network: 0,
		Mac:     &busMAC{addr: []byte{10, 0, byte(instanceId >> 8), byte(instanceId), 0xBA, 0xC0}},
	}
	ae := applicationlayer.NewApplicationEntity()
	net := &busNetwork{selfAddr: addr, bus: bus}
	ae.SetNetworkEntity(net)
	device := newTestDevice(instanceId, deviceName)
	sh := servicelayer.NewServiceHandler(ae, device)
	s := &TestStack{addr: addr, ae: ae, sh: sh}
	bus.register(addr, ae)
	return s
}

// Addr returns the stack's BACnet address.
func (s *TestStack) Addr() *bacnet.BACnetAddress { return s.addr }

// ServiceHandler returns the stack's service handler.
func (s *TestStack) ServiceHandler() *servicelayer.ServiceHandler { return s.sh }

// RemoteDeviceCache returns the application entity's remote device cache.
func (s *TestStack) RemoteDeviceCache() *objectmodel.RemoteDeviceCache {
	return s.ae.RemoteDeviceCache()
}

// Close is a no-op; included for API symmetry with real UDP-based stacks.
func (s *TestStack) Close() {}

// newTestDevice builds a minimal Device suitable for integration testing.
func newTestDevice(instanceId uint32, objectName string) *objectmodel.Device {
	id := bacnet.BACnetObjectIdentifier(uint32(bacnet.BacnetDevice)<<22 | instanceId)
	devObj := objectmodel.NewDeviceObject(
		id, objectName, bacnet.DeviceStatusOperational,
		"TestVendor", 99, "TestModel", "1.0", "1.0",
		[]bacnet.BACnetServicesSupported{
			bacnet.ServicesSupportedReadProperty,
			bacnet.ServicesSupportedWriteProperty,
			bacnet.ServicesSupportedReadPropertyMultiple,
		},
		[]bacnet.BACnetObjectTypesSupported{bacnet.ObjectTypesSupportedDevice},
		480, bacnet.SegmentationSupportNone,
		1000, 3, 0, 1,
	)
	return objectmodel.NewDevice(devObj)
}
