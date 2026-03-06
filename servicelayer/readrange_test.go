package servicelayer

import (
	"testing"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/internal/encoding"
	"github.com/REQUEA/bacnet/objectmodel"
)

// buildUnsignedBytes encodes a uint64 as a BACnet application-tagged Unsigned.
func buildUnsignedBytes(v uint64) []byte {
	u := &encoding.Unsigned{}
	u.SetValue(v)
	b, _ := u.MarshalPrimitive()
	return b
}

// --- ReadRangeAck marshal/unmarshal roundtrip ---

func TestReadRangeAckMarshalUnmarshal(t *testing.T) {
	var ack ReadRangeAck
	ack.ObjectIdentifier.SetFromValues(uint16(bacnet.AnalogInput), 42)
	ack.PropertyIdentifier.SetValue(uint32(bacnet.LogBuffer))
	ack.FirstItem = true
	ack.LastItem = true
	ack.ItemCount = 2
	ack.ItemData = append(buildUnsignedBytes(100), buildUnsignedBytes(200)...)

	data, err := ack.Marshal()
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	got, err := UnmarshalReadRangeAck(data)
	if err != nil {
		t.Fatalf("UnmarshalReadRangeAck failed: %v", err)
	}
	if got.ObjectIdentifier.Instance() != 42 {
		t.Errorf("expected instance 42, got %d", got.ObjectIdentifier.Instance())
	}
	if got.ItemCount != 2 {
		t.Errorf("expected ItemCount=2, got %d", got.ItemCount)
	}
	if !got.FirstItem || !got.LastItem {
		t.Errorf("expected FirstItem=true LastItem=true, got %v %v", got.FirstItem, got.LastItem)
	}
	if got.MoreItems {
		t.Errorf("expected MoreItems=false, got true")
	}
	if len(got.ItemData) != len(ack.ItemData) {
		t.Errorf("ItemData length mismatch: got %d, want %d", len(got.ItemData), len(ack.ItemData))
	}
}

func TestReadRangeAckWithFirstSequenceNumber(t *testing.T) {
	var ack ReadRangeAck
	ack.ObjectIdentifier.SetFromValues(uint16(bacnet.AnalogInput), 5)
	ack.PropertyIdentifier.SetValue(uint32(bacnet.LogBuffer))
	ack.FirstItem = false
	ack.LastItem = false
	ack.MoreItems = true
	ack.ItemCount = 1
	ack.ItemData = buildUnsignedBytes(42)
	fsn := &encoding.Unsigned{}
	fsn.SetValue(100)
	ack.FirstSequenceNumber.Set(fsn)

	data, err := ack.Marshal()
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	got, err := UnmarshalReadRangeAck(data)
	if err != nil {
		t.Fatalf("UnmarshalReadRangeAck failed: %v", err)
	}
	if !got.MoreItems {
		t.Errorf("expected MoreItems=true")
	}
	if !got.FirstSequenceNumber.Present() {
		t.Errorf("expected FirstSequenceNumber to be present")
	} else if got.FirstSequenceNumber.Get().Value() != 100 {
		t.Errorf("expected FirstSequenceNumber=100, got %d", got.FirstSequenceNumber.Get().Value())
	}
}

// --- Server handler tests ---

func TestReadRange_NotRangeProperty(t *testing.T) {
	client, server, serverAddr := makeLoopbackPair(t)
	ctx, cancel := reqCtx(t)
	defer cancel()

	// Add an AnalogInputObject: PresentValue does not implement RangeProperty.
	ai := objectmodel.NewAnalogInputObject(42, "AI42", bacnet.NoUnits)
	server.devices[0].AddObject(ai)

	_, err := client.ReadRange(ctx, serverAddr, ReadRangeSpec{
		ObjType:  uint16(bacnet.AnalogInput),
		Instance: 42,
		PropId:   bacnet.PresentValue,
	})
	if err == nil {
		t.Fatal("expected error when property does not implement RangeProperty, got nil")
	}
}

func TestReadRange_ByPosition(t *testing.T) {
	client, server, serverAddr := makeLoopbackPair(t)
	ctx, cancel := reqCtx(t)
	defer cancel()

	// Create a RangeListObject with 3 items.
	obj := objectmodel.NewRangeListObject(uint16(bacnet.Trendlog), 55, "Log1")
	obj.AppendItem(buildUnsignedBytes(10))
	obj.AppendItem(buildUnsignedBytes(20))
	obj.AppendItem(buildUnsignedBytes(30))
	server.devices[0].AddObject(obj)

	ack, err := client.ReadRange(ctx, serverAddr, ReadRangeSpec{
		ObjType:    uint16(bacnet.Trendlog),
		Instance:   55,
		PropId:     bacnet.LogBuffer,
		ByPosition: &ReadRangeByPosition{ReferenceIndex: 1, Count: 10},
	})
	if err != nil {
		t.Fatalf("ReadRange failed: %v", err)
	}
	if ack.ItemCount != 3 {
		t.Errorf("expected ItemCount=3, got %d", ack.ItemCount)
	}
	if !ack.FirstItem {
		t.Error("expected FirstItem=true")
	}
	if !ack.LastItem {
		t.Error("expected LastItem=true")
	}
	if ack.MoreItems {
		t.Error("expected MoreItems=false")
	}
	if len(ack.ItemData) == 0 {
		t.Error("expected non-empty ItemData")
	}
}

func TestReadRange_ByPosition_Partial(t *testing.T) {
	client, server, serverAddr := makeLoopbackPair(t)
	ctx, cancel := reqCtx(t)
	defer cancel()

	obj := objectmodel.NewRangeListObject(uint16(bacnet.Trendlog), 56, "Log2")
	obj.AppendItem(buildUnsignedBytes(1))
	obj.AppendItem(buildUnsignedBytes(2))
	obj.AppendItem(buildUnsignedBytes(3))
	obj.AppendItem(buildUnsignedBytes(4))
	obj.AppendItem(buildUnsignedBytes(5))
	server.devices[0].AddObject(obj)

	// Read only 2 items starting at position 2.
	ack, err := client.ReadRange(ctx, serverAddr, ReadRangeSpec{
		ObjType:    uint16(bacnet.Trendlog),
		Instance:   56,
		PropId:     bacnet.LogBuffer,
		ByPosition: &ReadRangeByPosition{ReferenceIndex: 2, Count: 2},
	})
	if err != nil {
		t.Fatalf("ReadRange failed: %v", err)
	}
	if ack.ItemCount != 2 {
		t.Errorf("expected ItemCount=2, got %d", ack.ItemCount)
	}
	if ack.FirstItem {
		t.Error("expected FirstItem=false (not starting at first item)")
	}
	if ack.LastItem {
		t.Error("expected LastItem=false (more items remain)")
	}
}

func TestReadRange_UnknownObject(t *testing.T) {
	client, _, serverAddr := makeLoopbackPair(t)
	ctx, cancel := reqCtx(t)
	defer cancel()

	_, err := client.ReadRange(ctx, serverAddr, ReadRangeSpec{
		ObjType:  uint16(bacnet.Trendlog),
		Instance: 9999,
		PropId:   bacnet.LogBuffer,
	})
	if err == nil {
		t.Fatal("expected error for unknown object, got nil")
	}
}
