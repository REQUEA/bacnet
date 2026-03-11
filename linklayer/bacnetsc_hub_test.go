package linklayer

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// newTestHub creates a BACnetSCHub and a test HTTP server wired to it.
// The server is closed via t.Cleanup.
func newTestHub(t *testing.T) (*BACnetSCHub, *httptest.Server) {
	t.Helper()
	hub, err := NewBACnetSCHub(BACnetSCHubConfig{})
	if err != nil {
		t.Fatalf("create hub: %v", err)
	}
	srv := httptest.NewServer(hub)
	t.Cleanup(srv.Close)
	return hub, srv
}

// dialHubRaw dials a hub WebSocket, completes the Connect-Request/Accept
// handshake with the given VMAC, and returns the WebSocket connection plus
// the hub's VMAC extracted from the OriginVMAC header of Connect-Accept.
func dialHubRaw(t *testing.T, url string, vmac BVMAC) (*websocket.Conn, BVMAC) {
	t.Helper()
	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
		Subprotocols:     []string{scSubprotocolHub},
	}
	ws, httpResp, err := dialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial hub: %v", err)
	}
	if httpResp != nil && httpResp.Body != nil {
		_ = httpResp.Body.Close()
	}

	crPayload := &ConnectRequestPayload{VMAC: vmac, MaxBVLCLength: 65535, MaxNPDULength: 65535}
	crMsg := &BVLCSCMessage{Function: BVLCSCFuncConnectRequest, MessageID: 1, Payload: crPayload.Marshal()}
	crData, _ := crMsg.Marshal()
	if err := ws.WriteMessage(websocket.BinaryMessage, crData); err != nil {
		t.Fatalf("send connect-request: %v", err)
	}

	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, caData, err := ws.ReadMessage()
	_ = ws.SetReadDeadline(time.Time{})
	if err != nil {
		t.Fatalf("read connect-accept: %v", err)
	}
	caMsg := &BVLCSCMessage{}
	if err := caMsg.Unmarshal(caData); err != nil || caMsg.Function != BVLCSCFuncConnectAccept {
		t.Fatalf("expected connect-accept: func=%v err=%v", caMsg.Function, err)
	}
	var hubVMACOut BVMAC
	if caMsg.OriginVMAC != nil {
		hubVMACOut = *caMsg.OriginVMAC
	}
	return ws, hubVMACOut
}

// TestHubFunctionBroadcast verifies that a broadcast EncapsulatedNPDU sent by
// client A is forwarded to client B with OriginVMAC=A and DestVMAC=BroadcastVMAC.
func TestHubFunctionBroadcast(t *testing.T) {
	_, srv := newTestHub(t)

	vmacA := BVMAC{0xAA, 0x00, 0x00, 0x00, 0x00, 0x01}
	vmacB := BVMAC{0xBB, 0x00, 0x00, 0x00, 0x00, 0x02}

	wsA, _ := dialHubRaw(t, wsURL(srv), vmacA)
	defer func() { _ = wsA.Close() }()
	wsB, _ := dialHubRaw(t, wsURL(srv), vmacB)
	defer func() { _ = wsB.Close() }()

	broadcast := BroadcastVMAC
	msg := &BVLCSCMessage{
		Function:  BVLCSCFuncEncapsulatedNPDU,
		Control:   ControlDestVMACPresent,
		MessageID: 2,
		DestVMAC:  &broadcast,
		Payload:   []byte{0x01, 0x02, 0x03},
	}
	data, _ := msg.Marshal()
	if err := wsA.WriteMessage(websocket.BinaryMessage, data); err != nil {
		t.Fatalf("send broadcast: %v", err)
	}

	received := readUntilFunction(t, wsB, BVLCSCFuncEncapsulatedNPDU, 3*time.Second)
	if received.OriginVMAC == nil {
		t.Fatal("expected OriginVMAC in broadcast forward")
	}
	if *received.OriginVMAC != vmacA {
		t.Errorf("OriginVMAC: want %v, got %v", vmacA, *received.OriginVMAC)
	}
	if received.DestVMAC == nil {
		t.Fatal("expected DestVMAC=BroadcastVMAC in broadcast forward")
	}
	if *received.DestVMAC != BroadcastVMAC {
		t.Errorf("DestVMAC: want BroadcastVMAC, got %v", *received.DestVMAC)
	}
}

// TestHubFunctionUnicast verifies that a unicast EncapsulatedNPDU is forwarded
// to the matching client with OriginVMAC set and no DestVMAC, and that the
// sender does not receive it back.
func TestHubFunctionUnicast(t *testing.T) {
	_, srv := newTestHub(t)

	vmacA := BVMAC{0xAA, 0x00, 0x00, 0x00, 0x00, 0x03}
	vmacB := BVMAC{0xBB, 0x00, 0x00, 0x00, 0x00, 0x04}

	wsA, _ := dialHubRaw(t, wsURL(srv), vmacA)
	defer func() { _ = wsA.Close() }()
	wsB, _ := dialHubRaw(t, wsURL(srv), vmacB)
	defer func() { _ = wsB.Close() }()

	msg := &BVLCSCMessage{
		Function:  BVLCSCFuncEncapsulatedNPDU,
		Control:   ControlDestVMACPresent,
		MessageID: 3,
		DestVMAC:  &vmacB,
		Payload:   []byte{0x04, 0x05, 0x06},
	}
	data, _ := msg.Marshal()
	if err := wsA.WriteMessage(websocket.BinaryMessage, data); err != nil {
		t.Fatalf("send unicast: %v", err)
	}

	// B should receive with OriginVMAC=A and no DestVMAC.
	received := readUntilFunction(t, wsB, BVLCSCFuncEncapsulatedNPDU, 3*time.Second)
	if received.OriginVMAC == nil {
		t.Fatal("expected OriginVMAC in unicast forward")
	}
	if *received.OriginVMAC != vmacA {
		t.Errorf("OriginVMAC: want %v, got %v", vmacA, *received.OriginVMAC)
	}
	if received.DestVMAC != nil {
		t.Errorf("expected no DestVMAC in unicast forward, got %v", *received.DestVMAC)
	}

	// A must NOT receive the message back.
	_ = wsA.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	_, _, readErr := wsA.ReadMessage()
	_ = wsA.SetReadDeadline(time.Time{})
	if readErr == nil {
		t.Error("sender A unexpectedly received a message")
	}
}

// TestHubFunctionUnicastUnknown verifies that a unicast to an unknown VMAC is
// silently discarded and the sender receives no response.
func TestHubFunctionUnicastUnknown(t *testing.T) {
	_, srv := newTestHub(t)

	vmacA := BVMAC{0xAA, 0x00, 0x00, 0x00, 0x00, 0x05}
	unknown := BVMAC{0xCC, 0x00, 0x00, 0x00, 0x00, 0x06}

	wsA, _ := dialHubRaw(t, wsURL(srv), vmacA)
	defer func() { _ = wsA.Close() }()

	msg := &BVLCSCMessage{
		Function:  BVLCSCFuncEncapsulatedNPDU,
		Control:   ControlDestVMACPresent,
		MessageID: 4,
		DestVMAC:  &unknown,
		Payload:   []byte{0x07, 0x08},
	}
	data, _ := msg.Marshal()
	if err := wsA.WriteMessage(websocket.BinaryMessage, data); err != nil {
		t.Fatalf("send unicast to unknown: %v", err)
	}

	// No response expected.
	_ = wsA.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	_, _, err := wsA.ReadMessage()
	_ = wsA.SetReadDeadline(time.Time{})
	if err == nil {
		t.Error("expected no response for unicast to unknown VMAC")
	}
}

// TestHubFunctionDuplicateReject verifies that a second connection attempt with
// an already-connected VMAC receives a BVLC-Result NAK and is closed.
func TestHubFunctionDuplicateReject(t *testing.T) {
	_, srv := newTestHub(t)

	vmacA := BVMAC{0xAA, 0x00, 0x00, 0x00, 0x00, 0x07}

	wsA, _ := dialHubRaw(t, wsURL(srv), vmacA)
	defer func() { _ = wsA.Close() }()

	// Second connect with the same VMAC — must not complete the handshake.
	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
		Subprotocols:     []string{scSubprotocolHub},
	}
	ws2, httpResp, err := dialer.Dial(wsURL(srv), nil)
	if err != nil {
		t.Fatalf("dial hub (2nd): %v", err)
	}
	if httpResp != nil && httpResp.Body != nil {
		_ = httpResp.Body.Close()
	}
	defer func() { _ = ws2.Close() }()

	crPayload := &ConnectRequestPayload{VMAC: vmacA, MaxBVLCLength: 65535, MaxNPDULength: 65535}
	crMsg := &BVLCSCMessage{Function: BVLCSCFuncConnectRequest, MessageID: 1, Payload: crPayload.Marshal()}
	crData, _ := crMsg.Marshal()
	if err := ws2.WriteMessage(websocket.BinaryMessage, crData); err != nil {
		t.Fatalf("send connect-request (2nd): %v", err)
	}

	_ = ws2.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msgData, err := ws2.ReadMessage()
	_ = ws2.SetReadDeadline(time.Time{})
	if err != nil {
		// Connection closed by hub without sending a message is also valid.
		return
	}

	msg := &BVLCSCMessage{}
	if err := msg.Unmarshal(msgData); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if msg.Function != BVLCSCFuncResult {
		t.Errorf("expected BVLC-Result (0x00), got 0x%02X", msg.Function)
	}
	p := &BVLCResultPayload{}
	if err := p.Unmarshal(msg.Payload); err != nil {
		t.Fatalf("unmarshal result payload: %v", err)
	}
	if p.ResultCode == 0 {
		t.Error("expected non-zero result code (NAK) for duplicate VMAC")
	}
}

// TestHubFunctionHeartbeat verifies that the hub replies to a HeartbeatRequest
// with a HeartbeatACK carrying the same MessageID.
func TestHubFunctionHeartbeat(t *testing.T) {
	_, srv := newTestHub(t)

	vmacA := BVMAC{0xAA, 0x00, 0x00, 0x00, 0x00, 0x08}
	wsA, _ := dialHubRaw(t, wsURL(srv), vmacA)
	defer func() { _ = wsA.Close() }()

	const hbID uint16 = 42
	hbReq := &BVLCSCMessage{Function: BVLCSCFuncHeartbeatRequest, MessageID: hbID}
	data, _ := hbReq.Marshal()
	if err := wsA.WriteMessage(websocket.BinaryMessage, data); err != nil {
		t.Fatalf("send heartbeat-request: %v", err)
	}

	ack := readUntilFunction(t, wsA, BVLCSCFuncHeartbeatACK, 3*time.Second)
	if ack.MessageID != hbID {
		t.Errorf("heartbeat ACK MessageID: want %d, got %d", hbID, ack.MessageID)
	}
}

// TestHubFunctionDisconnect verifies that the hub replies to a DisconnectRequest
// with a DisconnectACK and removes the client from its registry.
func TestHubFunctionDisconnect(t *testing.T) {
	hub, srv := newTestHub(t)

	vmacA := BVMAC{0xAA, 0x00, 0x00, 0x00, 0x00, 0x09}
	wsA, _ := dialHubRaw(t, wsURL(srv), vmacA)
	defer func() { _ = wsA.Close() }()

	hub.mu.RLock()
	_, registered := hub.clients[vmacKey(vmacA)]
	hub.mu.RUnlock()
	if !registered {
		t.Fatal("client not registered in hub after connect")
	}

	const discID uint16 = 99
	discReq := &BVLCSCMessage{Function: BVLCSCFuncDisconnectRequest, MessageID: discID}
	data, _ := discReq.Marshal()
	if err := wsA.WriteMessage(websocket.BinaryMessage, data); err != nil {
		t.Fatalf("send disconnect-request: %v", err)
	}

	ack := readUntilFunction(t, wsA, BVLCSCFuncDisconnectACK, 3*time.Second)
	if ack.MessageID != discID {
		t.Errorf("disconnect ACK MessageID: want %d, got %d", discID, ack.MessageID)
	}

	// Hub must remove the client from its map.
	deadline := time.After(time.Second)
	for {
		hub.mu.RLock()
		_, still := hub.clients[vmacKey(vmacA)]
		hub.mu.RUnlock()
		if !still {
			break
		}
		select {
		case <-deadline:
			t.Fatal("client still registered after disconnect")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// TestHubFunctionFullStack creates two full BACnetSCDatalink instances that
// connect to a BACnetSCHub and verifies end-to-end NPDU delivery.
func TestHubFunctionFullStack(t *testing.T) {
	hub, err := NewBACnetSCHub(BACnetSCHubConfig{})
	if err != nil {
		t.Fatalf("create hub: %v", err)
	}
	srv := httptest.NewServer(hub)
	defer srv.Close()

	dlA, err := NewBACnetSCDatalink(BACnetSCConfig{
		PrimaryHubURL: wsURL(srv),
		MaxAPDU:       1476,
	})
	if err != nil {
		t.Fatalf("create datalink A: %v", err)
	}
	handlerA := newRecordingNPDUHandler()
	portA := NewBACnetSCPort(dlA)
	portA.SetNPDUHandler(handlerA)
	if err := dlA.Start(); err != nil {
		t.Fatalf("start datalink A: %v", err)
	}
	defer func() { _ = dlA.Stop() }()

	dlB, err := NewBACnetSCDatalink(BACnetSCConfig{
		PrimaryHubURL: wsURL(srv),
		MaxAPDU:       1476,
	})
	if err != nil {
		t.Fatalf("create datalink B: %v", err)
	}
	handlerB := newRecordingNPDUHandler()
	portB := NewBACnetSCPort(dlB)
	portB.SetNPDUHandler(handlerB)
	if err := dlB.Start(); err != nil {
		t.Fatalf("start datalink B: %v", err)
	}
	defer func() { _ = dlB.Stop() }()

	time.Sleep(100 * time.Millisecond)

	testNPDU := []byte{0x01, 0x20, 0xAB, 0xCD}
	if err := dlA.Send(testNPDU, BroadcastVMAC[:]); err != nil {
		t.Fatalf("send from A: %v", err)
	}

	received := handlerB.waitFor(t, 3*time.Second)
	if string(received) != string(testNPDU) {
		t.Errorf("NPDU mismatch: want %x, got %x", testNPDU, received)
	}
}
