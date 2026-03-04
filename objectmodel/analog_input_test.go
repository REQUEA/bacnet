package objectmodel

import (
	"testing"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/internal/encoding"
	"github.com/REQUEA/bacnet/logger"
)

func init() {
	logger.SetLogger(logger.NoOpLogger{})
}

func TestNewAnalogInputObject_Properties(t *testing.T) {
	ai := NewAnalogInputObject(1, "Room Temperature", bacnet.UnitsDegreesCelsius)

	// ObjectIdentifier
	p := ai.GetProperty(bacnet.ObjectIdentifier)
	if p == nil {
		t.Fatal("ObjectIdentifier property missing")
	}
	id := p.GetValue().(*encoding.BACnetObjectIdentifier)
	if id.ObjType() != uint16(bacnet.AnalogInput) {
		t.Errorf("expected object type %d, got %d", bacnet.AnalogInput, id.ObjType())
	}
	if id.Instance() != 1 {
		t.Errorf("expected instance 1, got %d", id.Instance())
	}

	// ObjectName
	p = ai.GetProperty(bacnet.ObjectName)
	if p == nil {
		t.Fatal("ObjectName property missing")
	}
	if p.GetValue().(*encoding.CharacterString).Value() != "Room Temperature" {
		t.Errorf("unexpected object name: %v", p.GetValue())
	}

	// PresentValue default
	if ai.GetPresentValue() != 0.0 {
		t.Errorf("expected initial PresentValue 0.0, got %f", ai.GetPresentValue())
	}

	// OutOfService default
	if ai.IsOutOfService() {
		t.Error("expected initial OutOfService false")
	}

	// Units
	p = ai.GetProperty(bacnet.Units)
	if p == nil {
		t.Fatal("Units property missing")
	}
	if p.GetValue().(*encoding.Enumerated).Value() != uint32(bacnet.UnitsDegreesCelsius) {
		t.Errorf("unexpected units value: %v", p.GetValue())
	}
}

func TestAnalogInputObject_SetPresentValue(t *testing.T) {
	ai := NewAnalogInputObject(1, "Temp", bacnet.UnitsNoUnits)
	ai.SetPresentValue(21.5)

	if ai.GetPresentValue() != 21.5 {
		t.Errorf("expected 21.5, got %f", ai.GetPresentValue())
	}
}

func TestAnalogInputObject_PresentValueMarshal(t *testing.T) {
	ai := NewAnalogInputObject(1, "Temp", bacnet.UnitsNoUnits)
	ai.SetPresentValue(21.5)

	p := ai.GetProperty(bacnet.PresentValue)
	data, err := p.MarshalValue()
	if err != nil {
		t.Fatalf("MarshalValue failed: %v", err)
	}

	// Real 21.5 = 0x41AC0000 in IEEE 754
	// app tag 4, length 4: 0x44
	expected := []byte{0x44, 0x41, 0xAC, 0x00, 0x00}
	if len(data) != len(expected) {
		t.Fatalf("expected %d bytes, got %d: %x", len(expected), len(data), data)
	}
	for i, b := range expected {
		if data[i] != b {
			t.Errorf("byte[%d]: expected 0x%02x, got 0x%02x", i, b, data[i])
		}
	}
}

func TestAnalogInputObject_PresentValueReadOnly(t *testing.T) {
	ai := NewAnalogInputObject(1, "Temp", bacnet.UnitsNoUnits)

	// PresentValue is writable
	p := ai.GetProperty(bacnet.PresentValue)
	if !p.IsWritable() {
		t.Error("PresentValue should be writable")
	}

	// ObjectIdentifier is read-only
	p = ai.GetProperty(bacnet.ObjectIdentifier)
	if p.IsWritable() {
		t.Error("ObjectIdentifier should be read-only")
	}
}

func TestAnalogInputObject_OutOfService(t *testing.T) {
	ai := NewAnalogInputObject(1, "Temp", bacnet.UnitsNoUnits)

	ai.SetOutOfService(true)
	if !ai.IsOutOfService() {
		t.Error("expected OutOfService true after SetOutOfService(true)")
	}

	ai.SetOutOfService(false)
	if ai.IsOutOfService() {
		t.Error("expected OutOfService false after SetOutOfService(false)")
	}
}

func TestAnalogInputObject_SetStatusFlag(t *testing.T) {
	ai := NewAnalogInputObject(1, "Temp", bacnet.UnitsNoUnits)
	ai.SetStatusFlag(bacnet.StatusFlagFault, true)

	bs := ai.properties[bacnet.StatusFlags].GetValue().(*encoding.BitString)
	if !bs.GetBit(uint(bacnet.StatusFlagFault)) {
		t.Error("expected StatusFlagFault to be set")
	}
	if bs.GetBit(uint(bacnet.StatusFlagInAlarm)) {
		t.Error("expected StatusFlagInAlarm to be clear")
	}
}

func TestAnalogInputObject_AllPropertyIdentifiers(t *testing.T) {
	ai := NewAnalogInputObject(1, "Temp", bacnet.UnitsNoUnits)
	ids := ai.AllPropertyIdentifiers()

	required := []bacnet.PropertyIdentifier{
		bacnet.ObjectIdentifier,
		bacnet.ObjectName,
		bacnet.ObjectTypeProp,
		bacnet.PresentValue,
		bacnet.StatusFlags,
		bacnet.EventState,
		bacnet.OutOfService,
		bacnet.Units,
		bacnet.PropertyList,
	}

	idSet := make(map[bacnet.PropertyIdentifier]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	for _, req := range required {
		if !idSet[req] {
			t.Errorf("missing property identifier %v", req)
		}
	}
}

func TestDevice_AddObject_GetObject(t *testing.T) {
	devObj := NewDeviceObject(
		bacnet.BACnetObjectIdentifier(bacnet.BacnetDevice)<<22|1,
		"Test Device", bacnet.DeviceStatusOperational,
		"TestVendor", 1, "TestModel", "1.0", "1.0",
		nil, nil, 1476, bacnet.SegmentationSupportNone,
		3000, 3, 1, 1,
	)
	dev := NewDevice(devObj)

	ai := NewAnalogInputObject(1, "Room Temp", bacnet.UnitsDegreesCelsius)
	dev.AddObject(ai)

	got := dev.GetObject(bacnet.AnalogInput, 1)
	if got == nil {
		t.Fatal("GetObject returned nil")
	}
	if got.GetOwner() != dev {
		t.Error("owner not set correctly")
	}

	// Non-existent object
	if dev.GetObject(bacnet.AnalogInput, 99) != nil {
		t.Error("expected nil for non-existent object")
	}

	// ObjectList should contain device + analog input (2 entries)
	objectListProp := dev.DeviceObject().GetProperty(bacnet.ObjectList)
	if objectListProp == nil {
		t.Fatal("ObjectList property missing")
	}
	arrayProp, ok := objectListProp.(*BACnetArrayProperty[*encoding.BACnetObjectIdentifier])
	if !ok {
		t.Fatal("ObjectList is not a BACnetArrayProperty")
	}
	if arrayProp.value.Len() != 2 {
		t.Errorf("expected ObjectList length 2, got %d", arrayProp.value.Len())
	}
	entry, _ := arrayProp.value.Get(2)
	if entry.ObjType() != uint16(bacnet.AnalogInput) || entry.Instance() != 1 {
		t.Errorf("unexpected ObjectList[2]: type=%d instance=%d", entry.ObjType(), entry.Instance())
	}
}

func TestBooleanMarshalRoundTrip(t *testing.T) {
	p := NewBooleanProperty(false, true)
	data, err := p.MarshalValue()
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	// Application tag 1 with LVT=1 (true): byte 0x11
	if len(data) != 1 || data[0] != 0x11 {
		t.Errorf("unexpected marshal: %x", data)
	}

	p2 := NewBooleanProperty(false, false)
	if err := p2.UnmarshalValue(data); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if !p2.GetValue().(*encoding.Boolean).Value() {
		t.Error("expected true after unmarshal")
	}
}
