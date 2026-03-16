package servicelayer

import (
	"context"
	"testing"
	"time"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/applicationlayer"
	"github.com/REQUEA/bacnet/internal/encoding"
	"github.com/REQUEA/bacnet/networklayer"
	"github.com/REQUEA/bacnet/objectmodel"
)

// loopbackNetwork routes NUnitDataRequest calls asynchronously to a peer ApplicationEntity.
type loopbackNetwork struct {
	selfAddr *bacnet.BACnetAddress
	peer     *applicationlayer.ApplicationEntity
}

func (l *loopbackNetwork) NUnitDataRequest(
	dadr *bacnet.BACnetAddress, der bool,
	_ networklayer.NPDUPriority, payload []byte,
) error {
	cp := make([]byte, len(payload))
	copy(cp, payload)
	ind := &networklayer.NPDUIndication{
		Source:        l.selfAddr,
		Dest:          dadr,
		ExpectedReply: der,
		Apdu:          cp,
	}
	go l.peer.HandleNUnitDataIndication(ind)
	return nil
}

func (l *loopbackNetwork) NReleaseRequest(*bacnet.BACnetAddress) error { return nil }
func (l *loopbackNetwork) GetMaxPDULength(bacnet.NetworkNumber) uint   { return 480 }
func (l *loopbackNetwork) NUnitDataIndication(*networklayer.Port, bacnet.MAC, bacnet.MAC, []byte) error {
	return nil
}

// makeLoopbackPair creates a client and server ServiceHandler connected via loopback.
// The server hosts device instance 1000.
func makeLoopbackPair(t *testing.T) (client *ServiceHandler, server *ServiceHandler, serverAddr *bacnet.BACnetAddress) {
	t.Helper()
	clientAddr := &bacnet.BACnetAddress{Network: 0, Mac: &testMAC{addr: []byte{10, 0, 0, 1, 0xBA, 0xC0}}}
	serverAddr = &bacnet.BACnetAddress{Network: 0, Mac: &testMAC{addr: []byte{10, 0, 0, 2, 0xBA, 0xC0}}}

	clientAE := applicationlayer.NewApplicationEntity()
	serverAE := applicationlayer.NewApplicationEntity()

	clientAE.SetNetworkEntity(&loopbackNetwork{selfAddr: clientAddr, peer: serverAE})
	serverAE.SetNetworkEntity(&loopbackNetwork{selfAddr: serverAddr, peer: clientAE})

	clientDB := objectmodel.NewObjectDatabase(clientAE.RemoteDeviceCache())
	if err := clientDB.AddDevice(makeTestDevice()); err != nil {
		t.Fatal(err)
	}
	serverDB := objectmodel.NewObjectDatabase(serverAE.RemoteDeviceCache())
	if err := serverDB.AddDevice(makeTestDevice()); err != nil {
		t.Fatal(err)
	}
	client = NewServiceHandler(clientAE, clientDB)
	server = NewServiceHandler(serverAE, serverDB)
	return
}

func reqCtx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 500*time.Millisecond)
}

// --- ReadProperty tests ---

func TestClientReadProperty_ObjectName(t *testing.T) {
	client, _, serverAddr := makeLoopbackPair(t)
	ctx, cancel := reqCtx(t)
	defer cancel()

	val, err := client.ReadProperty(ctx, serverAddr,
		uint16(bacnet.BacnetDevice), 1000, bacnet.ObjectName, nil)
	if err != nil {
		t.Fatalf("ReadProperty failed: %v", err)
	}
	name, ok := val.(string)
	if !ok {
		t.Fatalf("expected string, got %T: %v", val, val)
	}
	if name != testDeviceName {
		t.Errorf("expected ObjectName %q, got %q", testDeviceName, name)
	}
}

func TestClientReadProperty_VendorIdentifier(t *testing.T) {
	client, _, serverAddr := makeLoopbackPair(t)
	ctx, cancel := reqCtx(t)
	defer cancel()

	val, err := client.ReadProperty(ctx, serverAddr,
		uint16(bacnet.BacnetDevice), 1000, bacnet.VendorIdentifier, nil)
	if err != nil {
		t.Fatalf("ReadProperty failed: %v", err)
	}
	// VendorIdentifier is an unsigned integer; ReadProperty decodes it as uint64.
	vendorID, ok := val.(uint64)
	if !ok {
		t.Fatalf("expected uint64, got %T: %v", val, val)
	}
	if vendorID != 99 {
		t.Errorf("expected VendorIdentifier 99, got %d", vendorID)
	}
}

func TestClientReadProperty_UnknownObject(t *testing.T) {
	client, _, serverAddr := makeLoopbackPair(t)
	ctx, cancel := reqCtx(t)
	defer cancel()

	_, err := client.ReadProperty(ctx, serverAddr,
		uint16(bacnet.BacnetDevice), 9999, bacnet.ObjectName, nil)
	if err == nil {
		t.Fatal("expected error for unknown object, got nil")
	}
}

func TestClientReadProperty_UnknownProperty(t *testing.T) {
	client, _, serverAddr := makeLoopbackPair(t)
	ctx, cancel := reqCtx(t)
	defer cancel()

	_, err := client.ReadProperty(ctx, serverAddr,
		uint16(bacnet.BacnetDevice), 1000, bacnet.PropertyIdentifier(0xFFFF), nil)
	if err == nil {
		t.Fatal("expected error for unknown property, got nil")
	}
}

// --- WriteProperty tests ---

func TestClientWriteProperty_ObjectName(t *testing.T) {
	client, server, serverAddr := makeLoopbackPair(t)
	ctx, cancel := reqCtx(t)
	defer cancel()

	// Encode new ObjectName as charstring using the encoding package
	newName := "UpdatedDevice"
	cs := encoding.NewCharacterString(newName)
	valBytes, err := cs.MarshalPrimitive()
	if err != nil {
		t.Fatalf("MarshalPrimitive failed: %v", err)
	}

	if writeErr := client.WriteProperty(ctx, serverAddr,
		uint16(bacnet.BacnetDevice), 1000, bacnet.ObjectName, nil, valBytes, nil); writeErr != nil {
		t.Fatalf("WriteProperty failed: %v", writeErr)
	}
	// Verify name was updated on the server
	devObj := server.Device().DeviceObject()
	nameProp := devObj.GetProperty(bacnet.ObjectName)
	nameCS, ok := nameProp.GetValue().(*encoding.CharacterString)
	if !ok || nameCS.Value() != newName {
		t.Errorf("expected updated name %q, got %v", newName, nameProp.GetValue())
	}
}

func TestClientWriteProperty_ReadOnlyProperty(t *testing.T) {
	client, _, serverAddr := makeLoopbackPair(t)
	ctx, cancel := reqCtx(t)
	defer cancel()

	// VendorIdentifier is read-only
	valBytes := []byte{0x21, 0x63} // unsigned 99

	err := client.WriteProperty(ctx, serverAddr,
		uint16(bacnet.BacnetDevice), 1000, bacnet.VendorIdentifier, nil, valBytes, nil)
	if err == nil {
		t.Fatal("expected error writing read-only property, got nil")
	}
}

// --- ReadPropertyMultiple tests ---

func TestClientReadPropertyMultiple(t *testing.T) {
	client, _, serverAddr := makeLoopbackPair(t)
	ctx, cancel := reqCtx(t)
	defer cancel()

	var spec ReadAccessSpec
	spec.ObjectIdentifier.SetFromValues(uint16(bacnet.BacnetDevice), 1000)

	var ref1 PropertyReference
	ref1.PropertyIdentifier.SetValue(uint32(bacnet.ObjectName))

	var ref2 PropertyReference
	ref2.PropertyIdentifier.SetValue(uint32(bacnet.VendorIdentifier))

	spec.PropertyRefs = []PropertyReference{ref1, ref2}

	results, err := client.ReadPropertyMultiple(ctx, serverAddr, []ReadAccessSpec{spec})
	if err != nil {
		t.Fatalf("ReadPropertyMultiple failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if len(results[0].PropertyResults) != 2 {
		t.Fatalf("expected 2 property results, got %d", len(results[0].PropertyResults))
	}

	// ObjectName: should succeed with a charstring value
	pr0 := results[0].PropertyResults[0]
	if pr0.Value == nil {
		t.Errorf("ObjectName result should have a value, got error %v/%v", pr0.ErrorClass, pr0.ErrorCode)
	} else {
		rawVal := pr0.Value.Value()
		if len(rawVal) < 2 || (rawVal[0]>>4) != 7 {
			t.Errorf("ObjectName: expected charstring tag, got bytes %v", rawVal)
		} else {
			var cs encoding.CharacterString
			_, decErr := cs.Unmarshal(rawVal)
			if decErr != nil {
				t.Errorf("ObjectName decode failed: %v", decErr)
			} else if cs.Value() != testDeviceName {
				t.Errorf("ObjectName: expected %q, got %q", testDeviceName, cs.Value())
			}
		}
	}

	// VendorIdentifier: should succeed with an unsigned int value
	pr1 := results[0].PropertyResults[1]
	if pr1.Value == nil {
		t.Errorf("VendorIdentifier result should have a value, got error %v/%v", pr1.ErrorClass, pr1.ErrorCode)
	} else {
		rawVal := pr1.Value.Value()
		if len(rawVal) < 2 || (rawVal[0]>>4) != 2 {
			t.Errorf("VendorIdentifier: expected unsigned tag, got bytes %v", rawVal)
		}
	}
}

func TestClientReadPropertyMultiple_UnknownProperty(t *testing.T) {
	client, _, serverAddr := makeLoopbackPair(t)
	ctx, cancel := reqCtx(t)
	defer cancel()

	var spec ReadAccessSpec
	spec.ObjectIdentifier.SetFromValues(uint16(bacnet.BacnetDevice), 1000)

	var ref1 PropertyReference
	ref1.PropertyIdentifier.SetValue(uint32(bacnet.ObjectName))

	var ref2 PropertyReference
	ref2.PropertyIdentifier.SetValue(uint32(0xFFFF)) // unknown property

	spec.PropertyRefs = []PropertyReference{ref1, ref2}

	results, err := client.ReadPropertyMultiple(ctx, serverAddr, []ReadAccessSpec{spec})
	if err != nil {
		t.Fatalf("ReadPropertyMultiple failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// First property (ObjectName) should succeed
	pr0 := results[0].PropertyResults[0]
	if pr0.Value == nil {
		t.Errorf("ObjectName should have succeeded, got error")
	}

	// Second property (unknown) should have an error
	pr1 := results[0].PropertyResults[1]
	if pr1.ErrorClass == nil || pr1.ErrorCode == nil {
		t.Errorf("unknown property should return an error result, got value")
	}
}

// TestRPMRequestMarshalUnmarshal round-trips a ReadPropertyMultiple request.
func TestRPMRequestMarshalUnmarshal(t *testing.T) {
	var spec ReadAccessSpec
	spec.ObjectIdentifier.SetFromValues(uint16(bacnet.BacnetDevice), 1000)

	var ref1 PropertyReference
	ref1.PropertyIdentifier.SetValue(uint32(bacnet.ObjectName))

	var ref2 PropertyReference
	ref2.PropertyIdentifier.SetValue(uint32(bacnet.VendorIdentifier))

	spec.PropertyRefs = []PropertyReference{ref1, ref2}

	req := ReadPropertyMultipleRequest{AccessSpecs: []ReadAccessSpec{spec}}
	data, err := req.Marshal()
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var req2 ReadPropertyMultipleRequest
	remaining, err := req2.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("unexpected remaining bytes: %v", remaining)
	}
	if len(req2.AccessSpecs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(req2.AccessSpecs))
	}
	if len(req2.AccessSpecs[0].PropertyRefs) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(req2.AccessSpecs[0].PropertyRefs))
	}
	if bacnet.PropertyIdentifier(req2.AccessSpecs[0].PropertyRefs[0].PropertyIdentifier.Value()) != bacnet.ObjectName {
		t.Errorf("expected ObjectName, got %v", req2.AccessSpecs[0].PropertyRefs[0].PropertyIdentifier.Value())
	}
	if bacnet.PropertyIdentifier(req2.AccessSpecs[0].PropertyRefs[1].PropertyIdentifier.Value()) != bacnet.VendorIdentifier {
		t.Errorf("expected VendorIdentifier, got %v", req2.AccessSpecs[0].PropertyRefs[1].PropertyIdentifier.Value())
	}
}

// TestReadPropertyAckRoundTrip round-trips a ReadPropertyAck.
func TestReadPropertyAckRoundTrip(t *testing.T) {
	var ack ReadPropertyAck
	ack.objectIdentifier.SetFromValues(uint16(bacnet.BacnetDevice), 1000)
	ack.propertyIdentifier.SetValue(uint32(bacnet.ObjectName))
	nameCS := encoding.NewCharacterString(testDeviceName)
	nameBytes, _ := nameCS.MarshalPrimitive()
	ack.propertyValue = encoding.NewAbstract(nameBytes)

	data, err := ack.Marshal()
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var ack2 ReadPropertyAck
	remaining, err := ack2.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("unexpected remaining bytes: %v", remaining)
	}
	if ack2.objectIdentifier.ObjType() != uint16(bacnet.BacnetDevice) {
		t.Errorf("expected BacnetDevice type")
	}
	if ack2.objectIdentifier.Instance() != 1000 {
		t.Errorf("expected instance 1000")
	}
	if bacnet.PropertyIdentifier(ack2.propertyIdentifier.Value()) != bacnet.ObjectName {
		t.Errorf("expected ObjectName")
	}
	var decoded encoding.CharacterString
	_, decErr := decoded.Unmarshal(ack2.propertyValue.Value())
	if decErr != nil {
		t.Fatalf("CharacterString.Unmarshal failed: %v", decErr)
	}
	if decoded.Value() != testDeviceName {
		t.Errorf("expected %q, got %q", testDeviceName, decoded.Value())
	}
}
