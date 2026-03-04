package servicelayer

import (
	"testing"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/internal/encoding"
)

// --- WritePropertyMultipleRequest marshal/unmarshal roundtrip ---

func TestWPMRequestMarshalUnmarshal(t *testing.T) {
	// Build a request with two objects and two properties each.
	nameCS := encoding.NewCharacterString("NewName")
	nameBytes, _ := nameCS.MarshalPrimitive()

	var pv1 WritePropertyValue
	pv1.PropertyIdentifier.SetValue(uint32(bacnet.ObjectName))
	pv1.Value = encoding.NewAbstract(nameBytes)

	var pv2 WritePropertyValue
	pv2.PropertyIdentifier.SetValue(uint32(bacnet.Description))
	pv2.Value = encoding.NewAbstract(nameBytes)

	var spec WriteAccessSpec
	spec.ObjectIdentifier.SetFromValues(uint16(bacnet.BacnetDevice), 1000)
	spec.ListOfProperties = []WritePropertyValue{pv1, pv2}

	req := WritePropertyMultipleRequest{WriteAccessSpecs: []WriteAccessSpec{spec}}
	data, err := req.Marshal()
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var req2 WritePropertyMultipleRequest
	remaining, err := req2.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("unexpected remaining bytes: %v", remaining)
	}
	if len(req2.WriteAccessSpecs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(req2.WriteAccessSpecs))
	}
	if len(req2.WriteAccessSpecs[0].ListOfProperties) != 2 {
		t.Fatalf("expected 2 properties, got %d", len(req2.WriteAccessSpecs[0].ListOfProperties))
	}
	if bacnet.PropertyIdentifier(req2.WriteAccessSpecs[0].ListOfProperties[0].PropertyIdentifier.Value()) != bacnet.ObjectName {
		t.Errorf("expected ObjectName, got %v", req2.WriteAccessSpecs[0].ListOfProperties[0].PropertyIdentifier.Value())
	}
}

// --- Server handler tests ---

func TestWPMServer_Success(t *testing.T) {
	client, server, serverAddr := makeLoopbackPair(t)
	ctx, cancel := reqCtx(t)
	defer cancel()

	nameCS := encoding.NewCharacterString("WPMUpdate")
	nameBytes, _ := nameCS.MarshalPrimitive()
	vendorCS := encoding.NewCharacterString("WPMVendor")
	vendorBytes, _ := vendorCS.MarshalPrimitive()

	var spec WriteAccessSpec
	spec.ObjectIdentifier.SetFromValues(uint16(bacnet.BacnetDevice), 1000)

	var pv1 WritePropertyValue
	pv1.PropertyIdentifier.SetValue(uint32(bacnet.ObjectName))
	pv1.Value = encoding.NewAbstract(nameBytes)

	var pv2 WritePropertyValue
	pv2.PropertyIdentifier.SetValue(uint32(bacnet.VendorName))
	pv2.Value = encoding.NewAbstract(vendorBytes)

	spec.ListOfProperties = []WritePropertyValue{pv1, pv2}

	err := client.WritePropertyMultiple(ctx, serverAddr, []WriteAccessSpec{spec})
	if err != nil {
		t.Fatalf("WritePropertyMultiple failed: %v", err)
	}

	// Verify both properties were written on the server.
	devObj := server.devices[0].DeviceObject()

	nameProp := devObj.GetProperty(bacnet.ObjectName)
	nameVal, ok := nameProp.GetValue().(*encoding.CharacterString)
	if !ok || nameVal.Value() != "WPMUpdate" {
		t.Errorf("expected ObjectName 'WPMUpdate', got %v", nameProp.GetValue())
	}

	vnameProp := devObj.GetProperty(bacnet.VendorName)
	vnameVal, ok2 := vnameProp.GetValue().(*encoding.CharacterString)
	if !ok2 || vnameVal.Value() != "WPMVendor" {
		t.Errorf("expected VendorName 'WPMVendor', got %v", vnameProp.GetValue())
	}
}

func TestWPMServer_UnknownObject(t *testing.T) {
	client, _, serverAddr := makeLoopbackPair(t)
	ctx, cancel := reqCtx(t)
	defer cancel()

	nameCS := encoding.NewCharacterString("Test")
	nameBytes, _ := nameCS.MarshalPrimitive()

	var spec WriteAccessSpec
	spec.ObjectIdentifier.SetFromValues(uint16(bacnet.BacnetDevice), 9999) // unknown instance

	var pv WritePropertyValue
	pv.PropertyIdentifier.SetValue(uint32(bacnet.ObjectName))
	pv.Value = encoding.NewAbstract(nameBytes)
	spec.ListOfProperties = []WritePropertyValue{pv}

	err := client.WritePropertyMultiple(ctx, serverAddr, []WriteAccessSpec{spec})
	if err == nil {
		t.Fatal("expected error for unknown object, got nil")
	}
}

func TestWPMServer_ReadOnlyProperty(t *testing.T) {
	client, server, serverAddr := makeLoopbackPair(t)
	ctx, cancel := reqCtx(t)
	defer cancel()

	// Save original name to verify it wasn't changed.
	devObj := server.devices[0].DeviceObject()
	origName := devObj.GetProperty(bacnet.ObjectName).GetValue().(*encoding.CharacterString).Value()

	nameCS := encoding.NewCharacterString("ShouldNotUpdate")
	nameBytes, _ := nameCS.MarshalPrimitive()
	vendorIdBytes := []byte{0x21, 0x63} // VendorIdentifier (read-only)

	var spec WriteAccessSpec
	spec.ObjectIdentifier.SetFromValues(uint16(bacnet.BacnetDevice), 1000)

	var pv1 WritePropertyValue
	pv1.PropertyIdentifier.SetValue(uint32(bacnet.ObjectName))
	pv1.Value = encoding.NewAbstract(nameBytes)

	var pv2 WritePropertyValue
	pv2.PropertyIdentifier.SetValue(uint32(bacnet.VendorIdentifier))
	pv2.Value = encoding.NewAbstract(vendorIdBytes)

	spec.ListOfProperties = []WritePropertyValue{pv1, pv2}

	err := client.WritePropertyMultiple(ctx, serverAddr, []WriteAccessSpec{spec})
	if err == nil {
		t.Fatal("expected error writing read-only property, got nil")
	}

	// Verify ObjectName was NOT changed (all-or-nothing).
	currentName := devObj.GetProperty(bacnet.ObjectName).GetValue().(*encoding.CharacterString).Value()
	if currentName != origName {
		t.Errorf("all-or-nothing violated: ObjectName was changed to %q despite error", currentName)
	}
}
