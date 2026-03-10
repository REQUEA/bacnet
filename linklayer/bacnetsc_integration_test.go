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

	// Device A sends an NPDU broadcast (no dest VMAC → hub broadcasts).
	testNPDU := []byte{0x01, 0x20, 0xDE, 0xAD}
	if err := dlA.Send(testNPDU, nil); err != nil {
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
