package testutil

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/logger"
	"github.com/REQUEA/bacnet/objectmodel"
	"github.com/REQUEA/bacnet/servicelayer"
)

func TestMain(m *testing.M) {
	logger.SetLogger(logger.NoOpLogger{})
	m.Run()
}

// reqCtx returns a context with a 2-second timeout for a single BACnet request.
func reqCtx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 2*time.Second)
}

// marshalCharstring encodes s as a BACnet application-tagged charstring (tag 7).
func marshalCharstring(s string) []byte {
	encoded := append([]byte{0x00}, []byte(s)...) // 0x00 = UTF-8 encoding indicator
	l := len(encoded)
	if l < 5 {
		return append([]byte{(7 << 4) | byte(l)}, encoded...)
	}
	return append([]byte{(7 << 4) | 5, byte(l)}, encoded...)
}

// decodeCharstring decodes an application-tagged charstring from raw bytes.
func decodeCharstring(b []byte) (string, bool) {
	if len(b) < 2 {
		return "", false
	}
	tag := b[0] >> 4
	if tag != 7 {
		return "", false
	}
	lvt := b[0] & 0x0F
	var str []byte
	if lvt == 5 {
		if len(b) < 3 {
			return "", false
		}
		length := int(b[1])
		if len(b) < 2+length {
			return "", false
		}
		str = b[3 : 2+length] // skip encoding indicator byte at b[2]
	} else {
		if len(b) < 1+int(lvt) {
			return "", false
		}
		str = b[2 : 1+int(lvt)] // skip encoding indicator byte at b[1]
	}
	return string(str), true
}

// --- P8-2: WhoIs / IAm ---

// TestIntegrationWhoIsIAm verifies that a WhoIs broadcast triggers an IAm
// response and populates the requester's remote device cache.
func TestIntegrationWhoIsIAm(t *testing.T) {
	bus := &Bus{}
	a := NewTestStack(t, bus, 100, "DeviceA")
	b := NewTestStack(t, bus, 200, "DeviceB")

	if err := a.ServiceHandler().WhoIs(nil, nil); err != nil {
		t.Fatalf("WhoIs failed: %v", err)
	}

	// B processes WhoIs asynchronously and sends an IAm broadcast.
	// Poll until A's cache contains B.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if a.RemoteDeviceCache().Get(b.Addr()) != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if a.RemoteDeviceCache().Get(b.Addr()) == nil {
		t.Error("stack A did not discover stack B after WhoIs")
	}
}

// --- P8-3: ReadProperty ---

// TestIntegrationReadProperty verifies round-trip ReadProperty between two stacks.
func TestIntegrationReadProperty(t *testing.T) {
	bus := &Bus{}
	a := NewTestStack(t, bus, 100, "DeviceA")
	b := NewTestStack(t, bus, 200, "DeviceB")

	ctx, cancel := reqCtx(t)
	defer cancel()

	val, err := a.ServiceHandler().ReadProperty(ctx, b.Addr(),
		uint16(bacnet.BacnetDevice), 200, bacnet.ObjectName, nil)
	if err != nil {
		t.Fatalf("ReadProperty failed: %v", err)
	}

	// ObjectName is decoded as a string by ReadProperty.
	name, ok := val.(string)
	if !ok {
		t.Fatalf("expected string, got %T: %v", val, val)
	}
	if name != "DeviceB" {
		t.Errorf("expected ObjectName %q, got %q", "DeviceB", name)
	}
}

// --- P8-4: WriteProperty ---

// TestIntegrationWriteProperty verifies that WriteProperty modifies the remote
// device's property value and that the change is visible on a subsequent read.
func TestIntegrationWriteProperty(t *testing.T) {
	bus := &Bus{}
	a := NewTestStack(t, bus, 100, "DeviceA")
	b := NewTestStack(t, bus, 200, "DeviceB")

	newName := "UpdatedB"
	valBytes := marshalCharstring(newName)

	ctx, cancel := reqCtx(t)
	defer cancel()

	if err := a.ServiceHandler().WriteProperty(ctx, b.Addr(),
		uint16(bacnet.BacnetDevice), 200, bacnet.ObjectName, nil, valBytes, nil); err != nil {
		t.Fatalf("WriteProperty failed: %v", err)
	}

	// Read back and verify.
	ctx2, cancel2 := reqCtx(t)
	defer cancel2()

	readVal, err := a.ServiceHandler().ReadProperty(ctx2, b.Addr(),
		uint16(bacnet.BacnetDevice), 200, bacnet.ObjectName, nil)
	if err != nil {
		t.Fatalf("ReadProperty after write failed: %v", err)
	}
	name, ok := readVal.(string)
	if !ok {
		t.Fatalf("expected string after write, got %T: %v", readVal, readVal)
	}
	if name != newName {
		t.Errorf("expected %q, got %q", newName, name)
	}
}

// --- P8-5: ReadPropertyMultiple ---

// TestIntegrationReadPropertyMultiple verifies that a single RPM request
// returns the correct values for multiple properties.
func TestIntegrationReadPropertyMultiple(t *testing.T) {
	bus := &Bus{}
	a := NewTestStack(t, bus, 100, "DeviceA")
	b := NewTestStack(t, bus, 200, "DeviceB")

	ctx, cancel := reqCtx(t)
	defer cancel()

	var spec servicelayer.ReadAccessSpec
	spec.ObjectIdentifier.SetFromValues(uint16(bacnet.BacnetDevice), 200)

	var ref1 servicelayer.PropertyReference
	ref1.PropertyIdentifier.SetValue(uint32(bacnet.ObjectName))

	var ref2 servicelayer.PropertyReference
	ref2.PropertyIdentifier.SetValue(uint32(bacnet.VendorIdentifier))

	spec.PropertyRefs = []servicelayer.PropertyReference{ref1, ref2}

	results, err := a.ServiceHandler().ReadPropertyMultiple(ctx, b.Addr(),
		[]servicelayer.ReadAccessSpec{spec})
	if err != nil {
		t.Fatalf("ReadPropertyMultiple failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if len(results[0].PropertyResults) != 2 {
		t.Fatalf("expected 2 property results, got %d", len(results[0].PropertyResults))
	}

	// ObjectName must succeed and contain the device name.
	pr0 := results[0].PropertyResults[0]
	if pr0.Value == nil {
		t.Errorf("ObjectName should have a value, got error")
	} else {
		raw := pr0.Value.Value()
		name, ok := decodeCharstring(raw)
		if !ok {
			t.Errorf("ObjectName: expected charstring, got bytes %v", raw)
		} else if name != "DeviceB" {
			t.Errorf("ObjectName: expected %q, got %q", "DeviceB", name)
		}
	}

	// VendorIdentifier must succeed with an unsigned value.
	pr1 := results[0].PropertyResults[1]
	if pr1.Value == nil {
		t.Errorf("VendorIdentifier should have a value, got error")
	} else {
		raw := pr1.Value.Value()
		if len(raw) < 1 || (raw[0]>>4) != 2 {
			t.Errorf("VendorIdentifier: expected unsigned tag, got bytes %v", raw)
		}
	}
}

// --- P8-6: Segmented response (server → client) ---

// TestIntegrationSegmentedResponse verifies that a large ReadProperty response
// is correctly segmented by the server and reassembled by the client.
//
// Stack B carries a 500-character Description property. With maxAPDU=480, the
// response body (~514 bytes) exceeds 480 bytes so B sends a 2-segment ComplexAck.
// Stack A must reassemble both segments and return the full string.
func TestIntegrationSegmentedResponse(t *testing.T) {
	largeDesc := "x" + strings.Repeat("A", 499) // 500-char string

	bus := &Bus{}
	segOpts := TestStackOptions{
		MaxAPDU:             480,
		SegmentationSupport: bacnet.SegmentationSupportBoth,
	}
	a := NewTestStack(t, bus, 100, "DeviceA", segOpts)
	b := NewTestStack(t, bus, 200, "DeviceB", segOpts)

	// Add large Description property to B's device object.
	b.ServiceHandler().Device().DeviceObject().SetProperty(
		bacnet.Description,
		objectmodel.NewCharacterStringProperty(false, largeDesc),
	)

	ctx, cancel := reqCtx(t)
	defer cancel()

	val, err := a.ServiceHandler().ReadProperty(ctx, b.Addr(),
		uint16(bacnet.BacnetDevice), 200, bacnet.Description, nil)
	if err != nil {
		t.Fatalf("ReadProperty(Description) failed: %v", err)
	}

	got, ok := val.(string)
	if !ok {
		t.Fatalf("expected string, got %T: %v", val, val)
	}
	if got != largeDesc {
		t.Errorf("Description mismatch: got len=%d, want len=%d", len(got), len(largeDesc))
		if len(got) > 20 {
			t.Errorf("  first 20 bytes got: %q", got[:20])
		}
	}
}

// --- P8-7: Segmented request (client → server) ---

// TestIntegrationSegmentedRequest verifies that a large WriteProperty request
// is correctly segmented by the client and reassembled by the server.
//
// Stack A has maxAPDU=128. A writes a 200-character string to B's ObjectName.
// The WriteProperty request body (~212 bytes) exceeds 128 bytes so A sends a
// 2-segment request. B reassembles both segments, applies the write, and
// responds with SimpleAck. A's future resolves without error.
func TestIntegrationSegmentedRequest(t *testing.T) {
	newName := "N" + strings.Repeat("B", 199) // 200-char string

	bus := &Bus{}
	aOpts := TestStackOptions{
		MaxAPDU:             128,
		SegmentationSupport: bacnet.SegmentationSupportBoth,
	}
	bOpts := TestStackOptions{
		MaxAPDU:             128,
		SegmentationSupport: bacnet.SegmentationSupportBoth,
	}
	a := NewTestStack(t, bus, 100, "DeviceA", aOpts)
	b := NewTestStack(t, bus, 200, "DeviceB", bOpts)

	// Discover B so that A knows B's maxAPDU (128) and uses the correct segment size.
	if err := a.ServiceHandler().WhoIs(nil, nil); err != nil {
		t.Fatalf("WhoIs failed: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if a.RemoteDeviceCache().Get(b.Addr()) != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if a.RemoteDeviceCache().Get(b.Addr()) == nil {
		t.Fatal("stack A did not discover stack B")
	}

	// Encode the new name as an application-tagged CharacterString.
	valBytes := marshalCharstring(newName)

	ctx, cancel := reqCtx(t)
	defer cancel()

	if err := a.ServiceHandler().WriteProperty(ctx, b.Addr(),
		uint16(bacnet.BacnetDevice), 200, bacnet.ObjectName, nil, valBytes, nil); err != nil {
		t.Fatalf("WriteProperty (segmented) failed: %v", err)
	}

	// Read back and verify the write was applied correctly.
	ctx2, cancel2 := reqCtx(t)
	defer cancel2()

	readVal, err := a.ServiceHandler().ReadProperty(ctx2, b.Addr(),
		uint16(bacnet.BacnetDevice), 200, bacnet.ObjectName, nil)
	if err != nil {
		t.Fatalf("ReadProperty after segmented write failed: %v", err)
	}
	got, ok := readVal.(string)
	if !ok {
		t.Fatalf("expected string after write, got %T", readVal)
	}
	if got != newName {
		t.Errorf("ObjectName mismatch: got len=%d, want len=%d", len(got), len(newName))
	}
}
