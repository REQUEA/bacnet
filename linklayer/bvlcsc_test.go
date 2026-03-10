package linklayer

import (
	"testing"
)

func TestBVLCSCMessageRoundTrip_EncapsulatedNPDU(t *testing.T) {
	npdu := []byte{0x01, 0x00, 0x10, 0x00, 0xC4, 0x02}
	origin := BVMAC{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	dest := BVMAC{0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F}

	msg := &BVLCSCMessage{
		Function:   BVLCSCFuncEncapsulatedNPDU,
		Control:    ControlOriginVMACPresent | ControlDestVMACPresent,
		MessageID:  42,
		OriginVMAC: &origin,
		DestVMAC:   &dest,
		Payload:    npdu,
	}

	data, err := msg.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := &BVLCSCMessage{}
	if err := got.Unmarshal(data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Function != BVLCSCFuncEncapsulatedNPDU {
		t.Errorf("function: want 0x01, got 0x%02X", got.Function)
	}
	if got.MessageID != 42 {
		t.Errorf("messageID: want 42, got %d", got.MessageID)
	}
	if got.OriginVMAC == nil || *got.OriginVMAC != origin {
		t.Errorf("originVMAC mismatch")
	}
	if got.DestVMAC == nil || *got.DestVMAC != dest {
		t.Errorf("destVMAC mismatch")
	}
	if string(got.Payload) != string(npdu) {
		t.Errorf("payload mismatch")
	}
}

func TestBVLCSCMessageRoundTrip_NoVMACs(t *testing.T) {
	msg := &BVLCSCMessage{
		Function:  BVLCSCFuncHeartbeatRequest,
		Control:   0,
		MessageID: 7,
	}
	data, err := msg.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(data) != 4 {
		t.Errorf("want 4 bytes for header-only message, got %d", len(data))
	}
	got := &BVLCSCMessage{}
	if err := got.Unmarshal(data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Function != BVLCSCFuncHeartbeatRequest {
		t.Errorf("function mismatch")
	}
	if got.OriginVMAC != nil || got.DestVMAC != nil {
		t.Errorf("expected nil VMACs")
	}
}

func TestBVLCSCConnectRequestPayload(t *testing.T) {
	vmac := BVMAC{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}
	uuid := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	p := &ConnectRequestPayload{
		VMAC:          vmac,
		MaxBVLCLength: 1476,
		MaxNPDULength: 1476,
		DeviceUUID:    uuid,
	}
	data := p.Marshal()
	if len(data) != 26 {
		t.Errorf("want 26 bytes, got %d", len(data))
	}

	got := &ConnectRequestPayload{}
	if err := got.Unmarshal(data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.VMAC != vmac {
		t.Errorf("VMAC mismatch")
	}
	if got.MaxBVLCLength != 1476 {
		t.Errorf("MaxBVLCLength: want 1476, got %d", got.MaxBVLCLength)
	}
	if got.MaxNPDULength != 1476 {
		t.Errorf("MaxNPDULength: want 1476, got %d", got.MaxNPDULength)
	}
	if got.DeviceUUID != uuid {
		t.Errorf("DeviceUUID mismatch")
	}
}

func TestBVLCSCConnectAcceptPayload(t *testing.T) {
	vmac := BVMAC{0x11, 0x22, 0x33, 0x44, 0x55, 0x66}
	uuid := [16]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	p := &ConnectAcceptPayload{VMAC: vmac, MaxBVLCLength: 65535, MaxNPDULength: 1476, DeviceUUID: uuid}
	data := p.Marshal()
	if len(data) != 26 {
		t.Errorf("want 26 bytes, got %d", len(data))
	}

	got := &ConnectAcceptPayload{}
	if err := got.Unmarshal(data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.VMAC != vmac {
		t.Errorf("VMAC mismatch")
	}
	if got.MaxBVLCLength != 65535 {
		t.Errorf("MaxBVLCLength mismatch")
	}
	if got.MaxNPDULength != 1476 {
		t.Errorf("MaxNPDULength mismatch")
	}
	if got.DeviceUUID != uuid {
		t.Errorf("DeviceUUID mismatch")
	}
}

func TestBVLCSCAdvertisementPayload(t *testing.T) {
	p := &AdvertisementPayload{
		HubConnectionState:      HubConnectionConnected,
		AcceptDirectConnections: true,
		MaxBVLCLength:           9999,
		MaxNPDULength:           1476,
	}
	data := p.Marshal()
	if len(data) != 6 {
		t.Errorf("want 6 bytes, got %d", len(data))
	}

	got := &AdvertisementPayload{}
	if err := got.Unmarshal(data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.HubConnectionState != HubConnectionConnected {
		t.Errorf("HubConnectionState: want %d, got %d", HubConnectionConnected, got.HubConnectionState)
	}
	if !got.AcceptDirectConnections {
		t.Error("AcceptDirectConnections: want true, got false")
	}
	if got.MaxBVLCLength != 9999 {
		t.Errorf("MaxBVLCLength: want 9999, got %d", got.MaxBVLCLength)
	}
	if got.MaxNPDULength != 1476 {
		t.Errorf("MaxNPDULength: want 1476, got %d", got.MaxNPDULength)
	}
}

func TestBVLCSCAddressResolutionACKPayload(t *testing.T) {
	p := &AddressResolutionACKPayload{
		URIs: []string{"wss://hub.example.com:9999/sc"},
	}
	data := p.Marshal()

	got := &AddressResolutionACKPayload{}
	if err := got.Unmarshal(data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.URIs) != 1 || got.URIs[0] != p.URIs[0] {
		t.Errorf("URIs mismatch: %v", got.URIs)
	}
}

func TestBVLCSCBVLCResultPayload(t *testing.T) {
	// ResultCode=1 (NAK) includes all error fields.
	p := &BVLCResultPayload{
		Function:          BVLCSCFuncConnectRequest,
		ResultCode:        1,
		ErrorHeaderMarker: 0x91,
		ErrorClass:        0x0002,
		ErrorCode:         0x0004,
		ErrorMsg:          "not authorized",
	}
	data := p.Marshal()

	got := &BVLCResultPayload{}
	if err := got.Unmarshal(data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Function != p.Function {
		t.Errorf("Function mismatch")
	}
	if got.ResultCode != p.ResultCode {
		t.Errorf("ResultCode mismatch")
	}
	if got.ErrorHeaderMarker != p.ErrorHeaderMarker {
		t.Errorf("ErrorHeaderMarker mismatch")
	}
	if got.ErrorClass != p.ErrorClass {
		t.Errorf("ErrorClass mismatch")
	}
	if got.ErrorCode != p.ErrorCode {
		t.Errorf("ErrorCode mismatch")
	}
	if got.ErrorMsg != p.ErrorMsg {
		t.Errorf("ErrorMsg mismatch")
	}
}

func TestBVLCSCDisconnectMessages(t *testing.T) {
	for _, fn := range []BVLCSCFunction{
		BVLCSCFuncDisconnectRequest,
		BVLCSCFuncDisconnectACK,
		BVLCSCFuncHeartbeatACK,
	} {
		msg := &BVLCSCMessage{
			Function:  fn,
			Control:   0,
			MessageID: 99,
		}
		data, err := msg.Marshal()
		if err != nil {
			t.Fatalf("marshal fn=0x%02X: %v", fn, err)
		}
		got := &BVLCSCMessage{}
		if err := got.Unmarshal(data); err != nil {
			t.Fatalf("unmarshal fn=0x%02X: %v", fn, err)
		}
		if got.Function != fn {
			t.Errorf("function mismatch: want 0x%02X got 0x%02X", fn, got.Function)
		}
	}
}

func TestBVMACProperties(t *testing.T) {
	v, err := NewRandomVMAC()
	if err != nil {
		t.Fatalf("NewRandomVMAC: %v", err)
	}
	if v.IsBroadcast() {
		t.Error("random VMAC should not be broadcast")
	}

	b := v.GetBytes()
	if len(b) != 6 {
		t.Errorf("GetBytes: want 6 bytes, got %d", len(b))
	}

	var v2 BVMAC
	v2.FromBytes(b)
	if v != v2 {
		t.Error("FromBytes round-trip failed")
	}

	bc := BroadcastVMAC
	if !bc.IsBroadcast() {
		t.Error("BroadcastVMAC should be broadcast")
	}

	if !v.Equal(&v2) {
		t.Error("Equal should be true for same VMAC")
	}
	if v.Equal(&bc) {
		t.Error("Equal should be false for different VMACs")
	}
}

func TestBVLCSCMessageTooShort(t *testing.T) {
	msg := &BVLCSCMessage{}
	if err := msg.Unmarshal([]byte{0x01, 0x00}); err == nil {
		t.Error("expected error for too-short message")
	}
}

func TestBVLCSCControlFlagBits(t *testing.T) {
	origin := BVMAC{1, 2, 3, 4, 5, 6}
	dest := BVMAC{7, 8, 9, 10, 11, 12}
	msg := &BVLCSCMessage{
		Function:   BVLCSCFuncEncapsulatedNPDU,
		OriginVMAC: &origin,
		DestVMAC:   &dest,
		MessageID:  1,
	}
	data, _ := msg.Marshal()
	// Octet 1 is the control flags byte.
	flags := BVLCSCControlFlags(data[1])
	if flags&ControlOriginVMACPresent == 0 {
		t.Error("OriginVMAC present flag not set")
	}
	if flags&ControlDestVMACPresent == 0 {
		t.Error("DestVMAC present flag not set")
	}
}

func newSecurePathOption() *SecurePathOption {
	return &SecurePathOption{BVLCSCOptionBase: BVLCSCOptionBase{
		HeaderMarker: BVLCSCHeaderMarker{OptionType: uint8(OptionTypeSecurePath)},
	}}
}

func newHelloOption(caps uint8) *HelloOption {
	return &HelloOption{
		BVLCSCOptionBase: BVLCSCOptionBase{
			HeaderMarker: BVLCSCHeaderMarker{OptionType: uint8(OptionTypeHello), DataFlag: true},
			HeaderLength: 1,
		},
		Capabilities: caps,
	}
}

func newHintOption(scope []byte) *HintOption {
	return &HintOption{
		BVLCSCOptionBase: BVLCSCOptionBase{
			HeaderMarker: BVLCSCHeaderMarker{OptionType: uint8(OptionTypeHint), DataFlag: true},
			HeaderLength: uint16(len(scope)),
		},
		Scope: scope,
	}
}

func newProprietaryOption(vendorID uint16, propType uint8, data []byte) *ProprietaryOption {
	return &ProprietaryOption{
		BVLCSCOptionBase: BVLCSCOptionBase{
			HeaderMarker: BVLCSCHeaderMarker{OptionType: uint8(OptionTypeProprietary), DataFlag: true},
			HeaderLength: uint16(3 + len(data)),
		},
		VendorID:        vendorID,
		ProprietaryType: propType,
		Data:            data,
	}
}

func TestBVLCSCOptionsRoundTrip(t *testing.T) {
	// 1. Secure-Path option: no data, DataFlag=0, control flag auto-set.
	t.Run("SecurePath", func(t *testing.T) {
		msg := &BVLCSCMessage{
			Function:    BVLCSCFuncEncapsulatedNPDU,
			MessageID:   10,
			DataOptions: []BVLCSCOption{newSecurePathOption()},
		}
		data, err := msg.Marshal()
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if BVLCSCControlFlags(data[1])&ControlDataOptionsPresent == 0 {
			t.Error("DataOptions flag not set in control byte")
		}
		got := &BVLCSCMessage{}
		if err := got.Unmarshal(data); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(got.DataOptions) != 1 {
			t.Fatalf("want 1 data option, got %d", len(got.DataOptions))
		}
		if _, ok := got.DataOptions[0].(*SecurePathOption); !ok {
			t.Errorf("option[0]: want *SecurePathOption, got %T", got.DataOptions[0])
		}
	})

	// 2. Proprietary option: all fields survive round-trip.
	t.Run("Proprietary", func(t *testing.T) {
		msg := &BVLCSCMessage{
			Function:           BVLCSCFuncEncapsulatedNPDU,
			MessageID:          20,
			DestinationOptions: []BVLCSCOption{newProprietaryOption(0x1234, 0x42, []byte{0xDE, 0xAD})},
		}
		data, err := msg.Marshal()
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if BVLCSCControlFlags(data[1])&ControlDestOptionsPresent == 0 {
			t.Error("DestOptions flag not set")
		}
		got := &BVLCSCMessage{}
		if err := got.Unmarshal(data); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(got.DestinationOptions) != 1 {
			t.Fatalf("want 1 dest option, got %d", len(got.DestinationOptions))
		}
		p, ok := got.DestinationOptions[0].(*ProprietaryOption)
		if !ok {
			t.Fatalf("want *ProprietaryOption, got %T", got.DestinationOptions[0])
		}
		if p.VendorID != 0x1234 || p.ProprietaryType != 0x42 || string(p.Data) != "\xDE\xAD" {
			t.Errorf("proprietary fields mismatch: %+v", p)
		}
	})

	// 3. Multiple options: MoreOptions bit correct for all-but-last.
	t.Run("MultipleOptions", func(t *testing.T) {
		msg := &BVLCSCMessage{
			Function:  BVLCSCFuncEncapsulatedNPDU,
			MessageID: 30,
			DataOptions: []BVLCSCOption{
				newHelloOption(0x01),
				newHintOption([]byte("abc")),
				newSecurePathOption(),
			},
		}
		data, err := msg.Marshal()
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		got := &BVLCSCMessage{}
		if err := got.Unmarshal(data); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(got.DataOptions) != 3 {
			t.Fatalf("want 3 data options, got %d", len(got.DataOptions))
		}
		h, ok := got.DataOptions[0].(*HelloOption)
		if !ok {
			t.Fatalf("option[0]: want *HelloOption, got %T", got.DataOptions[0])
		}
		if h.Capabilities != 0x01 {
			t.Errorf("Hello capabilities: want 1, got %d", h.Capabilities)
		}
		if _, ok := got.DataOptions[1].(*HintOption); !ok {
			t.Errorf("option[1]: want *HintOption, got %T", got.DataOptions[1])
		}
		if _, ok := got.DataOptions[2].(*SecurePathOption); !ok {
			t.Errorf("option[2]: want *SecurePathOption, got %T", got.DataOptions[2])
		}
		// Header = 4 bytes; no VMACs. DataOptions starts at offset 4.
		// Hello entry: [marker+DataFlag+type][len_hi][len_lo][caps] = 4 bytes, so byte[4] is Hello marker.
		if data[4]&HeaderMarkerMoreOptions == 0 {
			t.Error("first option marker should have MoreOptions bit set")
		}
		// Hint marker is at offset 4+4=8. It should also have MoreOptions set.
		if data[8]&HeaderMarkerMoreOptions == 0 {
			t.Error("second option marker should have MoreOptions bit set")
		}
	})

	// 4. Options alongside VMACs and payload in a full message.
	t.Run("WithVMACsAndPayload", func(t *testing.T) {
		origin := BVMAC{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
		dest := BVMAC{0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F}
		payload := []byte{0xC4, 0x02, 0x00, 0x00, 0x01}
		msg := &BVLCSCMessage{
			Function:           BVLCSCFuncEncapsulatedNPDU,
			MessageID:          99,
			OriginVMAC:         &origin,
			DestVMAC:           &dest,
			DestinationOptions: []BVLCSCOption{newSecurePathOption()},
			DataOptions:        []BVLCSCOption{newHintOption([]byte{0x01})},
			Payload:            payload,
		}
		data, err := msg.Marshal()
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		got := &BVLCSCMessage{}
		if err := got.Unmarshal(data); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.OriginVMAC == nil || *got.OriginVMAC != origin {
			t.Error("OriginVMAC mismatch")
		}
		if got.DestVMAC == nil || *got.DestVMAC != dest {
			t.Error("DestVMAC mismatch")
		}
		if len(got.DestinationOptions) != 1 {
			t.Error("DestinationOptions count mismatch")
		}
		if _, ok := got.DestinationOptions[0].(*SecurePathOption); !ok {
			t.Errorf("dest option: want *SecurePathOption, got %T", got.DestinationOptions[0])
		}
		if len(got.DataOptions) != 1 {
			t.Error("DataOptions count mismatch")
		}
		if h, ok := got.DataOptions[0].(*HintOption); !ok || string(h.Scope) != "\x01" {
			t.Errorf("data option: want *HintOption with scope [0x01], got %T", got.DataOptions[0])
		}
		if string(got.Payload) != string(payload) {
			t.Error("Payload mismatch")
		}
	})
}
