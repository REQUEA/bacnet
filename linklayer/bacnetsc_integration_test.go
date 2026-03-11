package linklayer

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/REQUEA/bacnet"
	"github.com/gorilla/websocket"
)

// hubClient wraps a websocket connection with its own write mutex.
type hubClient struct {
	ws  *websocket.Conn
	wmu sync.Mutex
}

func (c *hubClient) send(data []byte) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = c.ws.WriteMessage(websocket.BinaryMessage, data)
}

// hubServer is a minimal in-process BACnet/SC hub for integration testing.
// It accepts WebSocket connections, completes the Connect-Request/Accept handshake,
// and forwards EncapsulatedNPDU messages to all other connected clients.
type hubServer struct {
	mu      sync.Mutex
	clients map[string]*hubClient // keyed by VMAC hex string
}

func newHubServer() *hubServer {
	return &hubServer{clients: make(map[string]*hubClient)}
}

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(_ *http.Request) bool { return true },
}

// hubVMAC is the fixed VMAC the test hub advertises in Connect-Accept.
var hubVMAC = BVMAC{0x00, 0x00, 0x00, 0x00, 0x00, 0x01}

func (h *hubServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ws, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	// Read Connect-Request.
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := ws.ReadMessage()
	if err != nil {
		_ = ws.Close()
		return
	}
	_ = ws.SetReadDeadline(time.Time{})

	req := &BVLCSCMessage{}
	if err := req.Unmarshal(data); err != nil || req.Function != BVLCSCFuncConnectRequest {
		_ = ws.Close()
		return
	}
	crPayload := &ConnectRequestPayload{}
	if err := crPayload.Unmarshal(req.Payload); err != nil {
		_ = ws.Close()
		return
	}

	// Send Connect-Accept.
	caPayload := &ConnectAcceptPayload{VMAC: hubVMAC, MaxBVLCLength: 65535, MaxNPDULength: 65535}
	caMsg := &BVLCSCMessage{
		Function:   BVLCSCFuncConnectAccept,
		Control:    ControlOriginVMACPresent,
		MessageID:  req.MessageID,
		OriginVMAC: &hubVMAC,
		Payload:    caPayload.Marshal(),
	}
	caData, _ := caMsg.Marshal()
	if err := ws.WriteMessage(websocket.BinaryMessage, caData); err != nil {
		_ = ws.Close()
		return
	}

	client := &hubClient{ws: ws}
	key := vmacKey(crPayload.VMAC)
	h.mu.Lock()
	h.clients[key] = client
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.clients, key)
		h.mu.Unlock()
		_ = ws.Close()
	}()

	// Forward messages to other clients.
	for {
		_, msgData, err := ws.ReadMessage()
		if err != nil {
			return
		}
		msg := &BVLCSCMessage{}
		if err := msg.Unmarshal(msgData); err != nil {
			continue
		}
		switch msg.Function {
		case BVLCSCFuncEncapsulatedNPDU:
			h.mu.Lock()
			targets := make([]*hubClient, 0)
			for k, c := range h.clients {
				if k != key {
					targets = append(targets, c)
				}
			}
			h.mu.Unlock()
			for _, c := range targets {
				c.send(msgData)
			}
		case BVLCSCFuncHeartbeatRequest:
			ack := &BVLCSCMessage{Function: BVLCSCFuncHeartbeatACK, MessageID: msg.MessageID}
			if d, err := ack.Marshal(); err == nil {
				client.send(d)
			}
		case BVLCSCFuncDisconnectRequest:
			ack := &BVLCSCMessage{Function: BVLCSCFuncDisconnectACK, MessageID: msg.MessageID}
			if d, err := ack.Marshal(); err == nil {
				client.send(d)
			}
			return
		case BVLCSCFuncAdvertisement:
			// No-op for test hub.
		}
	}
}

// wsURL converts an httptest.Server URL from http:// to ws://.
func wsURL(s *httptest.Server) string {
	return strings.Replace(s.URL, "http://", "ws://", 1)
}

// recordingNPDUHandler records the last received NPDU.
type recordingNPDUHandler struct {
	mu       sync.Mutex
	last     []byte
	notifyCh chan struct{}
}

func newRecordingNPDUHandler() *recordingNPDUHandler {
	return &recordingNPDUHandler{notifyCh: make(chan struct{}, 1)}
}

func (h *recordingNPDUHandler) HandleNPDU(_, _ bacnet.MAC, buf []byte) error {
	h.mu.Lock()
	h.last = make([]byte, len(buf))
	copy(h.last, buf)
	h.mu.Unlock()
	select {
	case h.notifyCh <- struct{}{}:
	default:
	}
	return nil
}

func (h *recordingNPDUHandler) waitFor(t *testing.T, timeout time.Duration) []byte {
	t.Helper()
	select {
	case <-h.notifyCh:
	case <-time.After(timeout):
		t.Fatal("timeout waiting for NPDU")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.last
}

// TestBACnetSCDirectConnect tests that two datlinks connected via a hub
// can exchange NPDUs.
func TestBACnetSCDirectConnect(t *testing.T) {
	hub := newHubServer()
	srv := httptest.NewServer(hub)
	defer srv.Close()

	// Build device A.
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

	// Build device B.
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

	// Give connections a moment to establish.
	time.Sleep(100 * time.Millisecond)

	// Device A broadcasts an NPDU via the hub.
	testNPDU := []byte{0x01, 0x20, 0xDE, 0xAD}
	if err := dlA.Send(testNPDU, BroadcastVMAC[:]); err != nil {
		t.Fatalf("send from A: %v", err)
	}

	// Device B should receive it.
	received := handlerB.waitFor(t, 3*time.Second)
	if string(received) != string(testNPDU) {
		t.Errorf("received NPDU mismatch: want %x, got %x", testNPDU, received)
	}
}

// TestBACnetSCHeartbeat verifies the heartbeat goroutine sends periodic
// Heartbeat-Requests that the hub acknowledges.
func TestBACnetSCHeartbeat(t *testing.T) {
	heartbeatReceived := make(chan struct{}, 1)

	// Custom hub that signals when it receives a Heartbeat-Request.
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ws, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = ws.Close() }()

		// Handshake.
		_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		_ = ws.SetReadDeadline(time.Time{})

		req := &BVLCSCMessage{}
		if err := req.Unmarshal(data); err != nil || req.Function != BVLCSCFuncConnectRequest {
			return
		}
		caPayload := &ConnectAcceptPayload{VMAC: hubVMAC, MaxBVLCLength: 65535, MaxNPDULength: 65535}
		caMsg := &BVLCSCMessage{
			Function:   BVLCSCFuncConnectAccept,
			Control:    ControlOriginVMACPresent,
			MessageID:  req.MessageID,
			OriginVMAC: &hubVMAC,
			Payload:    caPayload.Marshal(),
		}
		caData, _ := caMsg.Marshal()
		if err := ws.WriteMessage(websocket.BinaryMessage, caData); err != nil {
			return
		}

		for {
			_, msgData, err := ws.ReadMessage()
			if err != nil {
				return
			}
			msg := &BVLCSCMessage{}
			if err := msg.Unmarshal(msgData); err != nil {
				continue
			}
			if msg.Function == BVLCSCFuncHeartbeatRequest {
				ack := &BVLCSCMessage{Function: BVLCSCFuncHeartbeatACK, MessageID: msg.MessageID}
				if d, err := ack.Marshal(); err == nil {
					_ = ws.WriteMessage(websocket.BinaryMessage, d)
				}
				select {
				case heartbeatReceived <- struct{}{}:
				default:
				}
			}
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	dl, err := NewBACnetSCDatalink(BACnetSCConfig{
		PrimaryHubURL:     wsURL(srv),
		HeartbeatInterval: 100 * time.Millisecond, // fast for test
	})
	if err != nil {
		t.Fatalf("create datalink: %v", err)
	}
	if err := dl.Start(); err != nil {
		t.Fatalf("start datalink: %v", err)
	}
	defer func() { _ = dl.Stop() }()

	select {
	case <-heartbeatReceived:
		// success
	case <-time.After(3 * time.Second):
		t.Fatal("did not receive heartbeat within timeout")
	}
}

// TestBACnetSCHeartbeatSuppressedByActivity verifies that the device does NOT send
// a HeartbeatRequest when the hub is sending messages regularly (activity suppresses heartbeat).
func TestBACnetSCHeartbeatSuppressedByActivity(t *testing.T) {
	heartbeatSeen := make(chan struct{}, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ws, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = ws.Close() }()

		// Handshake.
		_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		_ = ws.SetReadDeadline(time.Time{})

		req := &BVLCSCMessage{}
		if err := req.Unmarshal(data); err != nil || req.Function != BVLCSCFuncConnectRequest {
			return
		}
		crPayload := &ConnectRequestPayload{}
		if err := crPayload.Unmarshal(req.Payload); err != nil {
			return
		}
		caPayload := &ConnectAcceptPayload{VMAC: hubVMAC, MaxBVLCLength: 65535, MaxNPDULength: 65535}
		caMsg := &BVLCSCMessage{
			Function:   BVLCSCFuncConnectAccept,
			Control:    ControlOriginVMACPresent,
			MessageID:  req.MessageID,
			OriginVMAC: &hubVMAC,
			Payload:    caPayload.Marshal(),
		}
		caData, _ := caMsg.Marshal()
		if err := ws.WriteMessage(websocket.BinaryMessage, caData); err != nil {
			return
		}

		// Send a message to the device every 50ms to keep it active,
		// while watching for any HeartbeatRequest coming back.
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				_ = ws.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
				_, msgData, err := ws.ReadMessage()
				if err != nil {
					return
				}
				msg := &BVLCSCMessage{}
				if err := msg.Unmarshal(msgData); err != nil {
					continue
				}
				if msg.Function == BVLCSCFuncHeartbeatRequest {
					select {
					case heartbeatSeen <- struct{}{}:
					default:
					}
				}
			}
		}()

		// Send EncapsulatedNPDU every 50ms so the device sees recent activity.
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		deadline := time.After(400 * time.Millisecond) // 4 heartbeat intervals of 100ms
		for {
			select {
			case <-done:
				return
			case <-deadline:
				return
			case <-ticker.C:
				npduMsg := &BVLCSCMessage{
					Function:  BVLCSCFuncEncapsulatedNPDU,
					MessageID: 0x0001,
					Payload:   []byte{0x01, 0x00},
				}
				d, _ := npduMsg.Marshal()
				_ = ws.WriteMessage(websocket.BinaryMessage, d)
			}
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	dl, err := NewBACnetSCDatalink(BACnetSCConfig{
		PrimaryHubURL:     wsURL(srv),
		HeartbeatInterval: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("create datalink: %v", err)
	}
	if err := dl.Start(); err != nil {
		t.Fatalf("start datalink: %v", err)
	}
	defer func() { _ = dl.Stop() }()

	// Wait long enough for heartbeat to fire if not suppressed (3 intervals).
	select {
	case <-heartbeatSeen:
		t.Fatal("heartbeat should have been suppressed by recent activity, but was sent")
	case <-time.After(350 * time.Millisecond):
		// success: no heartbeat was sent while hub was actively sending
	}
}

// TestBACnetSCHeartbeatTimeout verifies that the device closes the connection
// when a Heartbeat-Request goes unacknowledged for one heartbeat interval.
func TestBACnetSCHeartbeatTimeout(t *testing.T) {
	connClosed := make(chan struct{}, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ws, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() {
			_ = ws.Close()
			select {
			case connClosed <- struct{}{}:
			default:
			}
		}()

		// Handshake.
		_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		_ = ws.SetReadDeadline(time.Time{})

		req := &BVLCSCMessage{}
		if err := req.Unmarshal(data); err != nil || req.Function != BVLCSCFuncConnectRequest {
			return
		}
		caPayload := &ConnectAcceptPayload{VMAC: hubVMAC, MaxBVLCLength: 65535, MaxNPDULength: 65535}
		caMsg := &BVLCSCMessage{
			Function:   BVLCSCFuncConnectAccept,
			Control:    ControlOriginVMACPresent,
			MessageID:  req.MessageID,
			OriginVMAC: &hubVMAC,
			Payload:    caPayload.Marshal(),
		}
		caData, _ := caMsg.Marshal()
		if err := ws.WriteMessage(websocket.BinaryMessage, caData); err != nil {
			return
		}

		// Drain messages but never send HeartbeatACK — just let reads time out.
		for {
			_ = ws.SetReadDeadline(time.Now().Add(time.Second))
			_, _, err := ws.ReadMessage()
			if err != nil {
				// Device closed the connection — expected.
				return
			}
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	dl, err := NewBACnetSCDatalink(BACnetSCConfig{
		PrimaryHubURL:     wsURL(srv),
		HeartbeatInterval: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("create datalink: %v", err)
	}
	if err := dl.Start(); err != nil {
		t.Fatalf("start datalink: %v", err)
	}
	defer func() { _ = dl.Stop() }()

	// Device should close connection within ~2 heartbeat intervals (1 to send, 1 to detect missing ACK).
	select {
	case <-connClosed:
		// success
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected device to close connection after heartbeat timeout, but it did not")
	}
}

// recordingNPDUHandlerWithSrc records received NPDUs along with the source address.
type recordingNPDUHandlerWithSrc struct {
	mu       sync.Mutex
	lastBuf  []byte
	lastSrc  bacnet.MAC
	notifyCh chan struct{}
}

func newRecordingNPDUHandlerWithSrc() *recordingNPDUHandlerWithSrc {
	return &recordingNPDUHandlerWithSrc{notifyCh: make(chan struct{}, 1)}
}

func (h *recordingNPDUHandlerWithSrc) HandleNPDU(_, sadr bacnet.MAC, buf []byte) error {
	h.mu.Lock()
	h.lastSrc = sadr
	h.lastBuf = make([]byte, len(buf))
	copy(h.lastBuf, buf)
	h.mu.Unlock()
	select {
	case h.notifyCh <- struct{}{}:
	default:
	}
	return nil
}

func (h *recordingNPDUHandlerWithSrc) waitFor(t *testing.T, timeout time.Duration) (bacnet.MAC, []byte) {
	t.Helper()
	select {
	case <-h.notifyCh:
	case <-time.After(timeout):
		t.Fatal("timeout waiting for NPDU")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastSrc, h.lastBuf
}

// dialDirectRaw dials a direct WebSocket connection to url, completes the
// Connect-Request/Accept handshake with the given VMAC, and returns the WebSocket.
// The caller must close the returned *websocket.Conn.
func dialDirectRaw(t *testing.T, url string, vmac BVMAC) *websocket.Conn {
	t.Helper()
	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
		Subprotocols:     []string{scSubprotocolDirectConnect},
	}
	ws, httpResp, err := dialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial direct: %v", err)
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
		t.Fatalf("expected connect-accept, got func=%v err=%v", caMsg.Function, err)
	}
	return ws
}

// readUntilFunction reads messages from ws until one with the given function type
// is found, or the timeout expires.
func readUntilFunction(t *testing.T, ws *websocket.Conn, fn BVLCSCFunction, timeout time.Duration) *BVLCSCMessage {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		_ = ws.SetReadDeadline(time.Now().Add(remaining))
		_, data, err := ws.ReadMessage()
		_ = ws.SetReadDeadline(time.Time{})
		if err != nil {
			t.Fatalf("read message: %v", err)
		}
		msg := &BVLCSCMessage{}
		if err := msg.Unmarshal(data); err != nil {
			continue
		}
		if msg.Function == fn {
			return msg
		}
	}
	t.Fatalf("timeout waiting for function 0x%02X", fn)
	return nil
}

// TestNodeSwitchOutboundHub verifies that EncapsulatedNPDU sent via a hub
// includes both OriginVMAC and DestVMAC headers (spec §YY.4.2.1).
func TestNodeSwitchOutboundHub(t *testing.T) {
	captureCh := make(chan *BVLCSCMessage, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ws, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = ws.Close() }()

		// Handshake.
		_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		_ = ws.SetReadDeadline(time.Time{})
		req := &BVLCSCMessage{}
		if err := req.Unmarshal(data); err != nil || req.Function != BVLCSCFuncConnectRequest {
			return
		}
		caPayload := &ConnectAcceptPayload{VMAC: hubVMAC, MaxBVLCLength: 65535, MaxNPDULength: 65535}
		caMsg := &BVLCSCMessage{
			Function:   BVLCSCFuncConnectAccept,
			Control:    ControlOriginVMACPresent,
			MessageID:  req.MessageID,
			OriginVMAC: &hubVMAC,
			Payload:    caPayload.Marshal(),
		}
		caData, _ := caMsg.Marshal()
		if err := ws.WriteMessage(websocket.BinaryMessage, caData); err != nil {
			return
		}

		// Capture EncapsulatedNPDU messages.
		for {
			_, msgData, err := ws.ReadMessage()
			if err != nil {
				return
			}
			msg := &BVLCSCMessage{}
			if err := msg.Unmarshal(msgData); err != nil {
				continue
			}
			if msg.Function == BVLCSCFuncEncapsulatedNPDU {
				select {
				case captureCh <- msg:
				default:
				}
			}
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	dl, err := NewBACnetSCDatalink(BACnetSCConfig{PrimaryHubURL: wsURL(srv)})
	if err != nil {
		t.Fatalf("create datalink: %v", err)
	}
	if err := dl.Start(); err != nil {
		t.Fatalf("start datalink: %v", err)
	}
	defer func() { _ = dl.Stop() }()

	time.Sleep(100 * time.Millisecond)

	destVMAC := BVMAC{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	npdu := []byte{0x01, 0x20, 0xAA, 0xBB}
	if err := dl.Send(npdu, destVMAC[:]); err != nil {
		t.Fatalf("send: %v", err)
	}

	select {
	case msg := <-captureCh:
		// Hub: OriginVMAC must be absent, DestVMAC must be present (spec §YY.4.2.1).
		if msg.OriginVMAC != nil {
			t.Errorf("expected OriginVMAC absent on hub send, got %v", *msg.OriginVMAC)
		}
		if msg.DestVMAC == nil {
			t.Fatal("expected DestVMAC to be present on hub send")
		}
		if *msg.DestVMAC != destVMAC {
			t.Errorf("DestVMAC: want %v, got %v", destVMAC, *msg.DestVMAC)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for EncapsulatedNPDU at hub")
	}
}

// TestNodeSwitchOutboundDirect verifies that EncapsulatedNPDU sent via a direct
// connection omits both OriginVMAC and DestVMAC headers (spec §YY.4.2.1).
func TestNodeSwitchOutboundDirect(t *testing.T) {
	vmacB := BVMAC{0x0B, 0x0B, 0x0B, 0x0B, 0x0B, 0x0B}

	dl, err := NewBACnetSCDatalink(BACnetSCConfig{})
	if err != nil {
		t.Fatalf("create datalink: %v", err)
	}

	// Expose the datalink's direct-connect handler via an in-process test server.
	srv := httptest.NewServer(http.HandlerFunc(dl.serveDirectConnect))
	defer srv.Close()

	// Raw client B connects to A's direct listener.
	ws := dialDirectRaw(t, wsURL(srv), vmacB)
	defer func() { _ = ws.Close() }()

	// Drain the Advertisement that A sends after accepting.
	readUntilFunction(t, ws, BVLCSCFuncAdvertisement, time.Second)

	// A should now have B in its connections map. Send an NPDU from A to B.
	npdu := []byte{0x01, 0x20, 0xCC, 0xDD}
	if err := dl.Send(npdu, vmacB[:]); err != nil {
		t.Fatalf("send: %v", err)
	}

	// B reads the EncapsulatedNPDU and checks there are no VMAC headers.
	msg := readUntilFunction(t, ws, BVLCSCFuncEncapsulatedNPDU, 3*time.Second)
	if msg.OriginVMAC != nil {
		t.Error("expected no OriginVMAC on direct connection send")
	}
	if msg.DestVMAC != nil {
		t.Error("expected no DestVMAC on direct connection send")
	}
}

// TestNodeSwitchInboundDirectPeerVMAC verifies that when an EncapsulatedNPDU arrives
// on a direct connection without OriginVMAC, the peer's handshake VMAC is used as
// the source address delivered to the NPDU handler (spec §YY.4.2.2).
func TestNodeSwitchInboundDirectPeerVMAC(t *testing.T) {
	vmacB := BVMAC{0x0C, 0x0C, 0x0C, 0x0C, 0x0C, 0x0C}

	dl, err := NewBACnetSCDatalink(BACnetSCConfig{})
	if err != nil {
		t.Fatalf("create datalink: %v", err)
	}
	handler := newRecordingNPDUHandlerWithSrc()
	port := NewBACnetSCPort(dl)
	port.SetNPDUHandler(handler)

	srv := httptest.NewServer(http.HandlerFunc(dl.serveDirectConnect))
	defer srv.Close()

	ws := dialDirectRaw(t, wsURL(srv), vmacB)
	defer func() { _ = ws.Close() }()

	// Wait for connection to be fully registered (Advertisement signals this).
	readUntilFunction(t, ws, BVLCSCFuncAdvertisement, time.Second)

	// B sends EncapsulatedNPDU with Control == 0 (no VMAC headers).
	npdu := []byte{0x01, 0x20, 0xEE, 0xFF}
	encMsg := &BVLCSCMessage{
		Function:  BVLCSCFuncEncapsulatedNPDU,
		Control:   0,
		MessageID: 2,
		Payload:   npdu,
	}
	data, _ := encMsg.Marshal()
	if err := ws.WriteMessage(websocket.BinaryMessage, data); err != nil {
		t.Fatalf("send encapsulated NPDU: %v", err)
	}

	// A's handler should receive vmacB as the source address.
	src, _ := handler.waitFor(t, 3*time.Second)
	if src == nil {
		t.Fatal("expected non-nil source address")
	}
	srcVMAC, ok := src.(*BVMAC)
	if !ok {
		t.Fatalf("source address has unexpected type %T", src)
	}
	if *srcVMAC != vmacB {
		t.Errorf("source VMAC: want %v, got %v", vmacB, *srcVMAC)
	}
}

// TestNodeSwitchBroadcastFromDirectDiscarded verifies that an EncapsulatedNPDU
// with DestVMAC == BroadcastVMAC arriving on a direct connection is discarded
// and not forwarded to the NPDU handler (spec §YY.4.2.2).
func TestNodeSwitchBroadcastFromDirectDiscarded(t *testing.T) {
	vmacB := BVMAC{0x0D, 0x0D, 0x0D, 0x0D, 0x0D, 0x0D}

	dl, err := NewBACnetSCDatalink(BACnetSCConfig{})
	if err != nil {
		t.Fatalf("create datalink: %v", err)
	}
	handler := newRecordingNPDUHandler()
	port := NewBACnetSCPort(dl)
	port.SetNPDUHandler(handler)

	srv := httptest.NewServer(http.HandlerFunc(dl.serveDirectConnect))
	defer srv.Close()

	ws := dialDirectRaw(t, wsURL(srv), vmacB)
	defer func() { _ = ws.Close() }()

	readUntilFunction(t, ws, BVLCSCFuncAdvertisement, time.Second)

	// B sends EncapsulatedNPDU with DestVMAC = BroadcastVMAC.
	broadcast := BroadcastVMAC
	encMsg := &BVLCSCMessage{
		Function:  BVLCSCFuncEncapsulatedNPDU,
		Control:   ControlDestVMACPresent,
		MessageID: 3,
		DestVMAC:  &broadcast,
		Payload:   []byte{0x01, 0x20},
	}
	data, _ := encMsg.Marshal()
	if err := ws.WriteMessage(websocket.BinaryMessage, data); err != nil {
		t.Fatalf("send: %v", err)
	}

	// Handler must NOT be called.
	select {
	case <-handler.notifyCh:
		t.Fatal("handler was called but broadcast from direct connection should be discarded")
	case <-time.After(200 * time.Millisecond):
		// success: handler was not called
	}
}

// TestBACnetSCReconnect verifies that the datalink automatically reconnects to the hub
// after the connection is dropped.
func TestBACnetSCReconnect(t *testing.T) {
	connCh := make(chan struct{}, 10)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ws, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = ws.Close() }()

		// Handshake.
		_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		_ = ws.SetReadDeadline(time.Time{})

		req := &BVLCSCMessage{}
		if err := req.Unmarshal(data); err != nil || req.Function != BVLCSCFuncConnectRequest {
			return
		}
		caPayload := &ConnectAcceptPayload{VMAC: hubVMAC, MaxBVLCLength: 65535, MaxNPDULength: 65535}
		caMsg := &BVLCSCMessage{
			Function:   BVLCSCFuncConnectAccept,
			Control:    ControlOriginVMACPresent,
			MessageID:  req.MessageID,
			OriginVMAC: &hubVMAC,
			Payload:    caPayload.Marshal(),
		}
		caData, _ := caMsg.Marshal()
		if err := ws.WriteMessage(websocket.BinaryMessage, caData); err != nil {
			return
		}

		// Signal that a new connection was accepted, then close to simulate a drop.
		select {
		case connCh <- struct{}{}:
		default:
		}
		// Handler returns here, closing the WebSocket.
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	dl, err := NewBACnetSCDatalink(BACnetSCConfig{
		PrimaryHubURL:     wsURL(srv),
		ReconnectInterval: 50 * time.Millisecond,
		HeartbeatInterval: 10 * time.Second, // large enough not to interfere
	})
	if err != nil {
		t.Fatalf("create datalink: %v", err)
	}
	if err := dl.Start(); err != nil {
		t.Fatalf("start datalink: %v", err)
	}

	// Wait for first connection.
	select {
	case <-connCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for initial connection")
	}

	// Hub closed the WS; wait for reconnect.
	select {
	case <-connCh:
		// success
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for reconnect after connection drop")
	}

	// Stop must return without hanging.
	stopDone := make(chan error, 1)
	go func() { stopDone <- dl.Stop() }()
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatalf("stop: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() hung")
	}
}

// TestBACnetSCDisconnect verifies that Stop() sends a Disconnect-Request
// and the hub receives the Disconnect-ACK.
func TestBACnetSCDisconnect(t *testing.T) {
	disconnected := make(chan struct{}, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ws, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = ws.Close() }()

		// Handshake.
		_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		_ = ws.SetReadDeadline(time.Time{})

		req := &BVLCSCMessage{}
		if err := req.Unmarshal(data); err != nil || req.Function != BVLCSCFuncConnectRequest {
			return
		}
		caPayload := &ConnectAcceptPayload{VMAC: hubVMAC, MaxBVLCLength: 65535, MaxNPDULength: 65535}
		caMsg := &BVLCSCMessage{
			Function:   BVLCSCFuncConnectAccept,
			Control:    ControlOriginVMACPresent,
			MessageID:  req.MessageID,
			OriginVMAC: &hubVMAC,
			Payload:    caPayload.Marshal(),
		}
		caData, _ := caMsg.Marshal()
		if err := ws.WriteMessage(websocket.BinaryMessage, caData); err != nil {
			return
		}

		for {
			_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
			_, msgData, err := ws.ReadMessage()
			if err != nil {
				// Connection closed — that's fine.
				select {
				case disconnected <- struct{}{}:
				default:
				}
				return
			}
			msg := &BVLCSCMessage{}
			if err := msg.Unmarshal(msgData); err != nil {
				continue
			}
			if msg.Function == BVLCSCFuncDisconnectRequest {
				ack := &BVLCSCMessage{Function: BVLCSCFuncDisconnectACK, MessageID: msg.MessageID}
				if d, err := ack.Marshal(); err == nil {
					_ = ws.WriteMessage(websocket.BinaryMessage, d)
				}
				select {
				case disconnected <- struct{}{}:
				default:
				}
				return
			}
			if msg.Function == BVLCSCFuncHeartbeatRequest {
				ack := &BVLCSCMessage{Function: BVLCSCFuncHeartbeatACK, MessageID: msg.MessageID}
				if d, err := ack.Marshal(); err == nil {
					_ = ws.WriteMessage(websocket.BinaryMessage, d)
				}
			}
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	dl, err := NewBACnetSCDatalink(BACnetSCConfig{
		PrimaryHubURL: wsURL(srv),
	})
	if err != nil {
		t.Fatalf("create datalink: %v", err)
	}
	if err := dl.Start(); err != nil {
		t.Fatalf("start datalink: %v", err)
	}

	// Give connection time to establish.
	time.Sleep(100 * time.Millisecond)

	if err := dl.Stop(); err != nil {
		t.Fatalf("stop datalink: %v", err)
	}

	select {
	case <-disconnected:
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive disconnect signal within timeout")
	}
}

// TestHubConnectorFailoverOnPrimaryFail verifies that when the primary hub is
// unreachable the connector establishes the connection via the failover hub.
func TestHubConnectorFailoverOnPrimaryFail(t *testing.T) {
	failoverHub := newHubServer()
	failoverSrv := httptest.NewServer(failoverHub)
	defer failoverSrv.Close()

	dl, err := NewBACnetSCDatalink(BACnetSCConfig{
		PrimaryHubURL:     "ws://127.0.0.1:1", // connection-refused — always fails
		FailoverHubURL:    wsURL(failoverSrv),
		ReconnectInterval: 10 * time.Second, // keep primary loop quiet
		HeartbeatInterval: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("create datalink: %v", err)
	}
	if err := dl.Start(); err != nil {
		t.Fatalf("start datalink: %v", err)
	}
	defer func() { _ = dl.Stop() }()

	// Start() tries failover synchronously when primary fails, so by the time
	// it returns the failover connection is already established.
	dl.mu.RLock()
	failoverUp := dl.failoverHub != nil && dl.failoverHub.state == connConnected
	primaryUp := dl.primaryHub != nil && dl.primaryHub.state == connConnected
	dl.mu.RUnlock()

	if !failoverUp {
		t.Error("expected connection via failover hub after primary failure")
	}
	if primaryUp {
		t.Error("primary hub should not be connected")
	}
}

// TestHubConnectorSwitchToPrimaryOnRestore verifies that once primary becomes
// available the connector switches to it and disconnects from failover.
func TestHubConnectorSwitchToPrimaryOnRestore(t *testing.T) {
	// Primary hub handler: initially returns 503 to simulate unavailability,
	// then starts accepting WebSocket connections once enabled is closed.
	primaryHub := newHubServer()
	enabled := make(chan struct{})
	primarySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-enabled:
			primaryHub.ServeHTTP(w, r)
		default:
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
		}
	}))
	defer primarySrv.Close()

	failoverHub := newHubServer()
	failoverSrv := httptest.NewServer(failoverHub)
	defer failoverSrv.Close()

	dl, err := NewBACnetSCDatalink(BACnetSCConfig{
		PrimaryHubURL:     wsURL(primarySrv),
		FailoverHubURL:    wsURL(failoverSrv),
		ReconnectInterval: 100 * time.Millisecond,
		HeartbeatInterval: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("create datalink: %v", err)
	}
	if err := dl.Start(); err != nil {
		t.Fatalf("start datalink: %v", err)
	}
	defer func() { _ = dl.Stop() }()

	// After Start() primary returns 503, so we should be on failover.
	dl.mu.RLock()
	failoverUp := dl.failoverHub != nil && dl.failoverHub.state == connConnected
	dl.mu.RUnlock()
	if !failoverUp {
		t.Fatal("expected initial connection on failover hub")
	}

	// Enable primary hub and wait for the reconnect loop to switch over.
	close(enabled)

	deadline := time.After(3 * time.Second)
	for {
		dl.mu.RLock()
		primaryUp := dl.primaryHub != nil && dl.primaryHub.state == connConnected
		failoverNil := dl.failoverHub == nil
		dl.mu.RUnlock()
		if primaryUp && failoverNil {
			break // success: primary connected, failover disconnected
		}
		select {
		case <-deadline:
			dl.mu.RLock()
			p, fo := dl.primaryHub, dl.failoverHub
			dl.mu.RUnlock()
			t.Fatalf("timeout waiting for switch to primary: primary=%v failover=%v", p, fo)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
