package servicelayer

import (
	"sync"
	"testing"
	"time"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/applicationlayer"
	"github.com/REQUEA/bacnet/internal/encoding"
	"github.com/REQUEA/bacnet/logger"
	"github.com/REQUEA/bacnet/networklayer"
	"github.com/REQUEA/bacnet/objectmodel"
)

// mockNetworkEntity captures sent PDUs for testing.
type mockNetworkEntity struct {
	mu   sync.Mutex
	sent [][]byte
}

func (m *mockNetworkEntity) NUnitDataIndication(_ *networklayer.Port, _ bacnet.MAC, _ bacnet.MAC, _ []byte) error {
	return nil
}
func (m *mockNetworkEntity) NUnitDataRequest(_ *bacnet.BACnetAddress, _ bool, _ networklayer.NPDUPriority, payload []byte) error {
	cp := make([]byte, len(payload))
	copy(cp, payload)
	m.mu.Lock()
	m.sent = append(m.sent, cp)
	m.mu.Unlock()
	return nil
}
func (m *mockNetworkEntity) NReleaseRequest(_ *bacnet.BACnetAddress) error { return nil }
func (m *mockNetworkEntity) GetMaxPDULength(_ bacnet.NetworkNumber) uint   { return 480 }

func (m *mockNetworkEntity) getSent() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([][]byte, len(m.sent))
	copy(result, m.sent)
	return result
}

// testMAC is a minimal MAC implementation for tests.
type testMAC struct {
	addr      []byte
	broadcast bool
}

func (m *testMAC) GetBytes() []byte        { return m.addr }
func (m *testMAC) FromBytes(b []byte)      { m.addr = b }
func (m *testMAC) String() string          { return "testMAC" }
func (m *testMAC) IsBroadcast() bool       { return m.broadcast }
func (m *testMAC) Equal(o bacnet.MAC) bool { return string(m.addr) == string(o.GetBytes()) }

func remoteAddr() *bacnet.BACnetAddress {
	return &bacnet.BACnetAddress{
		Network: 0,
		Mac:     &testMAC{addr: []byte{192, 168, 1, 100, 0xBA, 0xC0}},
	}
}

func localAddr() *bacnet.BACnetAddress {
	return &bacnet.BACnetAddress{
		Network: 0,
		Mac:     &testMAC{addr: []byte{10, 0, 0, 1, 0xBA, 0xC0}},
	}
}

const testDeviceName = "TestDevice"

// makeTestDevice creates a minimal local Device with instance 1000.
func makeTestDevice() *objectmodel.Device {
	id := bacnet.BACnetObjectIdentifier(uint32(bacnet.BacnetDevice)<<22 | 1000)
	devObj := objectmodel.NewDeviceObject(
		id, testDeviceName, bacnet.DeviceStatusOperational,
		"TestVendor", 99, "Model", "1.0", "1.0",
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

// makeTestSetup returns a wired-up ServiceHandler and the mock network entity.
func makeTestSetup() (*ServiceHandler, *mockNetworkEntity) {
	ae := applicationlayer.NewApplicationEntity()
	mock := &mockNetworkEntity{}
	ae.SetNetworkEntity(mock)
	db := objectmodel.NewObjectDatabase(ae.RemoteDeviceCache())
	if err := db.AddDevice(makeTestDevice()); err != nil {
		panic(err)
	}
	sh := NewServiceHandler(ae, db)
	return sh, mock
}

// marshalContextUnsigned encodes a uint64 with a given context tag number and context class.
func marshalContextUnsigned(contextTag byte, v uint64) []byte {
	var data []byte
	tmp := v
	for tmp > 0 {
		data = append([]byte{byte(tmp)}, data...)
		tmp >>= 8
	}
	l := len(data)
	return append([]byte{(contextTag << 4) | 0x08 | byte(l)}, data...)
}

// marshalContextObjectID encodes a BACnetObjectIdentifier with a context tag.
func marshalContextObjectID(contextTag byte, objType uint16, instance uint32) []byte {
	value := uint32(objType)<<22 | (instance & 0x3FFFFF)
	return []byte{
		(contextTag << 4) | 0x08 | 4,
		byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value),
	}
}

// buildReadPropertyBytes constructs a correctly-encoded ReadPropertyRequest payload.
func buildReadPropertyBytes(objType uint16, instance uint32, propID uint32) []byte {
	objBytes := marshalContextObjectID(0, objType, instance)
	propBytes := marshalContextUnsigned(1, uint64(propID))
	return append(objBytes, propBytes...)
}

// buildConfirmedServiceAPDU wraps a service payload in a ConfirmedServiceRequest APDU header.
func buildConfirmedServiceAPDU(invokeID uint, serviceChoice bacnet.BACnetConfirmedServiceChoice, payload []byte) []byte {
	// Byte 0: type=ConfirmedServiceRequest (0), SA=1 → 0x02
	// Byte 1: maxSegs=0 (unspecified), maxResp=4 (1024 bytes) → 0x04
	apdu := []byte{0x02, 0x04, byte(invokeID), byte(serviceChoice)}
	return append(apdu, payload...)
}

// injectAndWait injects an APDU indication and waits for async processing.
func injectAndWait(sh *ServiceHandler, apdu []byte) {
	ind := &networklayer.NPDUIndication{
		Source:        remoteAddr(),
		Dest:          localAddr(),
		ExpectedReply: true,
		Apdu:          apdu,
	}
	sh.applicationEntity.HandleNUnitDataIndication(ind)
	// Wait for the transaction goroutine to process the event.
	time.Sleep(20 * time.Millisecond)
}

// TestMain sets up the logger for all tests.
func TestMain(m *testing.M) {
	logger.SetLogger(logger.NoOpLogger{})
	m.Run()
}

// --- WritePropertyRequest encoding roundtrip ---

func TestWritePropertyRequest_Unmarshal(t *testing.T) {
	// Build WriteProperty bytes manually with correct context encoding:
	// objectIdentifier [0], propertyIdentifier [1], propertyValue [3]
	objBytes := marshalContextObjectID(0, uint16(bacnet.BacnetDevice), 1000)
	propBytes := marshalContextUnsigned(1, uint64(bacnet.ObjectName))
	// propertyValue [3]: context opening tag 3 (0x3E), app-tagged string, closing tag 3 (0x3F)
	cs := encoding.NewCharacterString("NewName")
	strBytes, _ := cs.MarshalPrimitive()
	abstract := encoding.NewAbstract(strBytes)
	valBytes, _ := abstract.MarshalTagged(3)
	data := append(append(objBytes, propBytes...), valBytes...)

	var req WritePropertyRequest
	_, err := req.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if req.objectIdentifier.ObjType() != uint16(bacnet.BacnetDevice) {
		t.Errorf("wrong object type: %d", req.objectIdentifier.ObjType())
	}
	if req.objectIdentifier.Instance() != 1000 {
		t.Errorf("wrong instance: %d", req.objectIdentifier.Instance())
	}
	if req.propertyIdentifier.Value() != uint32(bacnet.ObjectName) {
		t.Errorf("wrong property ID: %d", req.propertyIdentifier.Value())
	}
	var decoded encoding.CharacterString
	_, decErr := decoded.Unmarshal(req.propertyValue.Value())
	if decErr != nil {
		t.Fatalf("could not decode property value: %v", decErr)
	}
	if decoded.Value() != "NewName" {
		t.Errorf("decoded value = %q, want %q", decoded.Value(), "NewName")
	}
}

// --- Integration tests via full APDU injection ---

func TestHandleReadProperty_ObjectName(t *testing.T) {
	sh, mock := makeTestSetup()

	payload := buildReadPropertyBytes(uint16(bacnet.BacnetDevice), 1000, uint32(bacnet.ObjectName))
	apdu := buildConfirmedServiceAPDU(1, bacnet.ConfirmedServiceChoiceReadProperty, payload)
	injectAndWait(sh, apdu)

	if len(mock.getSent()) == 0 {
		t.Fatal("no PDU was sent")
	}
	pdu := mock.getSent()[0]
	// Expected: ComplexAck (type 0x30) or SimpleAck (0x20)
	pduType := pdu[0] & 0xF0
	if pduType != 0x30 {
		t.Errorf("expected ComplexAck PDU type 0x30, got 0x%02X (full pdu: %v)", pduType, pdu)
	}
}

func TestHandleReadProperty_UnknownObject(t *testing.T) {
	sh, mock := makeTestSetup()

	// Instance 9999 does not exist.
	payload := buildReadPropertyBytes(uint16(bacnet.BacnetDevice), 9999, uint32(bacnet.ObjectName))
	apdu := buildConfirmedServiceAPDU(1, bacnet.ConfirmedServiceChoiceReadProperty, payload)
	injectAndWait(sh, apdu)

	if len(mock.getSent()) == 0 {
		t.Fatal("no PDU was sent")
	}
	pdu := mock.getSent()[0]
	if pdu[0]&0xF0 != 0x50 {
		t.Errorf("expected Error PDU type 0x50, got 0x%02X", pdu[0])
	}
}

func TestHandleReadProperty_UnknownProperty(t *testing.T) {
	sh, mock := makeTestSetup()

	payload := buildReadPropertyBytes(uint16(bacnet.BacnetDevice), 1000, 0xFFFF)
	apdu := buildConfirmedServiceAPDU(1, bacnet.ConfirmedServiceChoiceReadProperty, payload)
	injectAndWait(sh, apdu)

	if len(mock.getSent()) == 0 {
		t.Fatal("no PDU was sent")
	}
	pdu := mock.getSent()[0]
	if pdu[0]&0xF0 != 0x50 {
		t.Errorf("expected Error PDU type 0x50, got 0x%02X", pdu[0])
	}
}

func TestHandleWriteProperty_ReadOnly(t *testing.T) {
	sh, mock := makeTestSetup()

	// ObjectType is read-only → expect WriteAccessDenied error
	objBytes := marshalContextObjectID(0, uint16(bacnet.BacnetDevice), 1000)
	propBytes := marshalContextUnsigned(1, uint64(bacnet.ObjectTypeProp))
	enumVal := encoding.NewEnumerated(uint32(bacnet.BacnetDevice))
	valRaw, _ := enumVal.MarshalPrimitive()
	abstract := encoding.NewAbstract(valRaw)
	abstractBytes, _ := abstract.MarshalTagged(3)
	payload := append(append(objBytes, propBytes...), abstractBytes...)

	apdu := buildConfirmedServiceAPDU(2, bacnet.ConfirmedServiceChoiceWriteProperty, payload)
	injectAndWait(sh, apdu)

	if len(mock.getSent()) == 0 {
		t.Fatal("no PDU was sent")
	}
	pdu := mock.getSent()[0]
	if pdu[0]&0xF0 != 0x50 {
		t.Errorf("expected Error PDU type 0x50, got 0x%02X", pdu[0])
	}
}

func TestHandleWriteProperty_Success(t *testing.T) {
	sh, mock := makeTestSetup()

	// ObjectName is writable
	newName := "NewName"
	objBytes := marshalContextObjectID(0, uint16(bacnet.BacnetDevice), 1000)
	propBytes := marshalContextUnsigned(1, uint64(bacnet.ObjectName))
	cs := encoding.NewCharacterString(newName)
	valRaw, _ := cs.MarshalPrimitive()
	abstract := encoding.NewAbstract(valRaw)
	abstractBytes, _ := abstract.MarshalTagged(3)
	payload := append(append(objBytes, propBytes...), abstractBytes...)

	apdu := buildConfirmedServiceAPDU(3, bacnet.ConfirmedServiceChoiceWriteProperty, payload)
	injectAndWait(sh, apdu)

	if len(mock.getSent()) == 0 {
		t.Fatal("no PDU was sent")
	}
	pdu := mock.getSent()[0]
	if pdu[0]&0xF0 != 0x20 {
		t.Errorf("expected SimpleAck PDU type 0x20, got 0x%02X", pdu[0])
	}

	// Verify the property was updated.
	device := sh.db.FindDeviceByObjectID(bacnet.BacnetDevice, 1000)
	if device == nil {
		t.Fatal("device not found after write")
	}
	nameProp := device.DeviceObject().GetProperty(bacnet.ObjectName)
	if nameProp == nil {
		t.Fatal("ObjectName property not found")
	}
	nameCS, ok := nameProp.GetValue().(*encoding.CharacterString)
	if !ok || nameCS.Value() != newName {
		t.Errorf("expected ObjectName=%q, got %v", newName, nameProp.GetValue())
	}
}

func TestHandleReadPropertyMultiple(t *testing.T) {
	sh, mock := makeTestSetup()

	// Build RPM request: objectIdentifier [0], opening tag [1], property refs, closing tag [1]
	objIDBytes := marshalContextObjectID(0, uint16(bacnet.BacnetDevice), 1000)
	prop1Bytes := marshalContextUnsigned(0, uint64(bacnet.ObjectName))
	prop2Bytes := marshalContextUnsigned(0, uint64(bacnet.SystemStatus))

	payload := objIDBytes
	payload = append(payload, openingTag(1))
	payload = append(payload, prop1Bytes...)
	payload = append(payload, prop2Bytes...)
	payload = append(payload, closingTag(1))

	apdu := buildConfirmedServiceAPDU(4, bacnet.ConfirmedServiceChoiceReadPropertyMultiple, payload)
	injectAndWait(sh, apdu)

	if len(mock.getSent()) == 0 {
		t.Fatal("no PDU was sent")
	}
	pdu := mock.getSent()[0]
	if pdu[0]&0xF0 != 0x30 {
		t.Errorf("expected ComplexAck PDU type 0x30, got 0x%02X", pdu[0])
	}
}
