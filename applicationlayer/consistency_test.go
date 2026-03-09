package applicationlayer

import (
	"sync"
	"testing"
	"time"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/networklayer"
	"github.com/REQUEA/bacnet/objectmodel"
)

// --- Test infrastructure ---

type secTestNet struct {
	mu   sync.Mutex
	sent [][]byte
}

func (n *secTestNet) NUnitDataIndication(*networklayer.Port, bacnet.MAC, bacnet.MAC, []byte) error {
	return nil
}
func (n *secTestNet) NUnitDataRequest(_ *bacnet.BACnetAddress, _ bool, _ networklayer.NPDUPriority, payload []byte) error {
	cp := make([]byte, len(payload))
	copy(cp, payload)
	n.mu.Lock()
	n.sent = append(n.sent, cp)
	n.mu.Unlock()
	return nil
}
func (n *secTestNet) NReleaseRequest(*bacnet.BACnetAddress) error { return nil }
func (n *secTestNet) GetMaxPDULength(bacnet.NetworkNumber) uint   { return 480 }
func (n *secTestNet) getSent() [][]byte {
	n.mu.Lock()
	defer n.mu.Unlock()
	cp := make([][]byte, len(n.sent))
	copy(cp, n.sent)
	return cp
}

// findAbortPDU scans captured PDUs and returns the first Abort PDU (type nibble 7),
// or nil if none is found within timeout.
func findAbortPDU(n *secTestNet, sentByServer bool, timeout time.Duration) []byte {
	flag := byte(0)
	if sentByServer {
		flag = 1
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, pdu := range n.getSent() {
			if len(pdu) == 3 && pdu[0]>>4 == uint8(Abort) && pdu[0]&1 == flag {
				return pdu
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	return nil
}

// waitMinSent blocks until at least n PDUs have been sent or timeout elapses.
func waitMinSent(n *secTestNet, count int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(n.getSent()) >= count {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

type secTestSH struct {
	device *objectmodel.Device
}

func (h *secTestSH) GetDevice(_ bacnet.BACnetConfirmedServiceChoice, _ []byte) *objectmodel.Device {
	return h.device
}
func (h *secTestSH) HandleConfServIndication(*APDUIndication, bacnet.BACnetConfirmedServiceChoice) {}
func (h *secTestSH) HandleConfServConfirm(*APDUIndication, bacnet.BACnetConfirmedServiceChoice)    {}
func (h *secTestSH) HandleUnconfServIndication(*APDUIndication, bacnet.BACnetUnconfirmedServiceChoice) {
}
func (h *secTestSH) HandleSegmentAckIndication(*APDUIndication, bacnet.BACnetConfirmedServiceChoice) {
}
func (h *secTestSH) HandleRejectIndication(*APDUIndication, bacnet.BACnetConfirmedServiceChoice) {}
func (h *secTestSH) HandleAbortIndication(*APDUIndication, bacnet.BACnetConfirmedServiceChoice, uint8) {
}

type secTestMAC struct{ b []byte }

func (m *secTestMAC) GetBytes() []byte   { return m.b }
func (m *secTestMAC) FromBytes(b []byte) { m.b = b }
func (m *secTestMAC) String() string     { return "secTestMAC" }
func (m *secTestMAC) IsBroadcast() bool  { return false }
func (m *secTestMAC) Equal(o bacnet.MAC) bool {
	return string(m.b) == string(o.GetBytes())
}

func newSecTestAddr(b ...byte) *bacnet.BACnetAddress {
	return &bacnet.BACnetAddress{Network: 0, Mac: &secTestMAC{b: b}}
}

// newSecTestDevice creates a minimal Device with segmentation support.
func newSecTestDevice() *objectmodel.Device {
	id := bacnet.BACnetObjectIdentifier(uint32(bacnet.BacnetDevice)<<22 | 1000)
	devObj := objectmodel.NewDeviceObject(
		id, "SecTestDevice", bacnet.DeviceStatusOperational,
		"Vendor", 99, "Model", "1.0", "1.0",
		[]bacnet.BACnetServicesSupported{bacnet.ServicesSupportedWriteProperty},
		[]bacnet.BACnetObjectTypesSupported{bacnet.ObjectTypesSupportedDevice},
		480, bacnet.SegmentationSupportBoth,
		1000, 3, 0, 16,
	)
	return objectmodel.NewDevice(devObj)
}

// segConfReqPDU builds a raw segmented ConfirmedServiceRequest APDU.
// Byte layout: [typeFlag, segsResp, invokeID, seqNum, windowSize, serviceChoice, data...]
func segConfReqPDU(invokeID, seqNum, windowSize, maxSegs, maxResp, serviceChoice byte, moreSegments bool, data []byte) []byte {
	flags := byte(0x08) // SegmentedRequest
	if moreSegments {
		flags |= 0x04
	}
	segsResp := (maxSegs << 4) | maxResp
	pdu := []byte{flags, segsResp, invokeID, seqNum, windowSize, serviceChoice}
	return append(pdu, data...)
}

// complexAckSegPDU builds a raw segmented ComplexAck APDU.
// Byte layout: [typeFlag, invokeID, seqNum, windowSize, serviceChoice, data...]
func complexAckSegPDU(invokeID, seqNum, windowSize, serviceChoice byte, moreSegments bool, data []byte) []byte {
	flags := byte(0x08) // SegmentedRequest
	if moreSegments {
		flags |= 0x04
	}
	typeFlag := uint8(ComplexAck<<4) | flags
	pdu := []byte{typeFlag, invokeID, seqNum, windowSize, serviceChoice}
	return append(pdu, data...)
}

// --- Test 1: Server rejects follow-up segment with mismatched APDU attributes ---

func TestConsistencyServerApduAttributeMismatch(t *testing.T) {
	net := &secTestNet{}
	ae := NewApplicationEntity()
	ae.SetNetworkEntity(net)
	ae.SetServiceLayer(&secTestSH{device: newSecTestDevice()})

	srcAddr := newSecTestAddr(1, 2, 3, 4)
	dstAddr := newSecTestAddr(5, 6, 7, 8)

	// Segment 0: maxSegs=5, maxResp=4
	seg0 := segConfReqPDU(1, 0, 1, 5, 4, byte(bacnet.ConfirmedServiceChoiceWriteProperty), true, []byte{0xAA})
	ae.HandleNUnitDataIndication(&networklayer.NPDUIndication{
		Source: srcAddr,
		Dest:   dstAddr,
		Apdu:   seg0,
	})

	if !waitMinSent(net, 1, time.Second) {
		t.Fatal("timed out waiting for SegmentAck after segment 0")
	}

	// Segment 1: maxSegs=3 (changed from 5) — inconsistent APDU attributes
	seg1 := segConfReqPDU(1, 1, 1, 3, 4, byte(bacnet.ConfirmedServiceChoiceWriteProperty), false, []byte{0xBB})
	ae.HandleNUnitDataIndication(&networklayer.NPDUIndication{
		Source: srcAddr,
		Dest:   dstAddr,
		Apdu:   seg1,
	})

	abortPDU := findAbortPDU(net, true, time.Second)
	if abortPDU == nil {
		t.Fatal("expected server-sent Abort PDU, got none")
	}
	if abortPDU[2] != uint8(bacnet.AbortInvalidApduInThisState) {
		t.Errorf("expected AbortInvalidApduInThisState (0x%02X), got 0x%02X",
			uint8(bacnet.AbortInvalidApduInThisState), abortPDU[2])
	}
}

// --- Test 2: Client rejects follow-up ComplexAck segment with mismatched ServiceAckChoice ---

func TestConsistencyClientInconsistentServiceChoice(t *testing.T) {
	net := &secTestNet{}
	ae := NewApplicationEntity()
	ae.SetNetworkEntity(net)
	ae.SetServiceLayer(&secTestSH{device: newSecTestDevice()})

	dest := newSecTestAddr(9, 10, 11, 12)

	// Launch the request (invokeID=1 allocated)
	future := ae.SendConfServRequest(bacnet.ConfirmedServiceChoiceWriteProperty, dest, true, 0, []byte{0x01})

	// Wait for the request PDU to be sent
	if !waitMinSent(net, 1, time.Second) {
		t.Fatal("timed out waiting for ConfirmedServiceRequest to be sent")
	}

	// Inject ComplexAck segment 0: serviceChoice=12 (ReadProperty), more=1
	seg0 := complexAckSegPDU(1, 0, 1, 12, true, []byte{0xCC})
	ae.HandleNUnitDataIndication(&networklayer.NPDUIndication{
		Source: dest,
		Dest:   dest,
		Apdu:   seg0,
	})

	// Wait for the client's SegmentAck (2nd PDU sent)
	if !waitMinSent(net, 2, time.Second) {
		t.Fatal("timed out waiting for client SegmentAck after ComplexAck seg 0")
	}

	// Inject ComplexAck segment 1: serviceChoice=14 (RPM) — inconsistent!
	seg1 := complexAckSegPDU(1, 1, 1, 14, true, []byte{0xDD})
	ae.HandleNUnitDataIndication(&networklayer.NPDUIndication{
		Source: dest,
		Dest:   dest,
		Apdu:   seg1,
	})

	// Client should send Abort with AbortInvalidApduInThisState
	abortPDU := findAbortPDU(net, false, time.Second)
	if abortPDU == nil {
		t.Fatal("expected client-sent Abort PDU, got none")
	}
	if abortPDU[2] != uint8(bacnet.AbortInvalidApduInThisState) {
		t.Errorf("expected AbortInvalidApduInThisState (0x%02X), got 0x%02X",
			uint8(bacnet.AbortInvalidApduInThisState), abortPDU[2])
	}

	// Future must resolve with ResponseAbort
	resp := future.WaitResponseWithTimeout(time.Second)
	if resp == nil {
		t.Fatal("future did not resolve")
	}
	if resp.Type != ResponseAbort {
		t.Errorf("expected ResponseAbort, got %d", resp.Type)
	}
}
