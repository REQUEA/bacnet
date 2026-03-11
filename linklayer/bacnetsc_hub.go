package linklayer

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/REQUEA/bacnet/logger"
	"github.com/gorilla/websocket"
)

// BACnetSCHubConfig holds the configuration for a BACnet/SC Hub Function.
type BACnetSCHubConfig struct {
	// VMAC is the hub's Virtual MAC Address. Auto-generated if zero.
	VMAC BVMAC
	// DeviceUUID is the hub's 16-byte device UUID. Auto-generated if zero.
	DeviceUUID [16]byte
	// TLSConfig is the TLS configuration. When nil the server uses plain HTTP/WS.
	TLSConfig *tls.Config
	// MaxBVLC is the maximum BVLC message length advertised (default 65535).
	MaxBVLC uint16
	// MaxNPDU is the maximum NPDU length advertised (default 65535).
	MaxNPDU uint16
	// ListenAddr is the TCP address the hub listens on (e.g. ":9898").
	// When empty, Start() is a no-op and the hub is used via ServeHTTP directly.
	ListenAddr string
}

func (cfg *BACnetSCHubConfig) maxBVLC() uint16 {
	if cfg.MaxBVLC > 0 {
		return cfg.MaxBVLC
	}
	return 65535
}

func (cfg *BACnetSCHubConfig) maxNPDU() uint16 {
	if cfg.MaxNPDU > 0 {
		return cfg.MaxNPDU
	}
	return 65535
}

// hubConn is the per-client state managed by BACnetSCHub.
type hubConn struct {
	ws   *websocket.Conn
	wmu  sync.Mutex
	vmac BVMAC
}

func (c *hubConn) sendRaw(data []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return c.ws.WriteMessage(websocket.BinaryMessage, data)
}

func (c *hubConn) send(data []byte) { _ = c.sendRaw(data) }

// BACnetSCHub implements the BACnet/SC Hub Function (Annex AB.5.3).
// It accepts WebSocket connections from nodes, completes the handshake, and
// forwards EncapsulatedNPDU messages between connected clients.
type BACnetSCHub struct {
	cfg        BACnetSCHubConfig
	mu         sync.RWMutex
	clients    map[string]*hubConn // keyed by vmacKey(vmac)
	httpServer *http.Server
}

// NewBACnetSCHub creates a new Hub Function instance.
// VMAC and DeviceUUID are auto-generated when zero.
func NewBACnetSCHub(cfg BACnetSCHubConfig) (*BACnetSCHub, error) {
	if cfg.VMAC == (BVMAC{}) {
		v, err := NewRandomVMAC()
		if err != nil {
			return nil, fmt.Errorf("sc hub: generate VMAC: %w", err)
		}
		cfg.VMAC = v
	}
	var zeroUUID [16]byte
	if cfg.DeviceUUID == zeroUUID {
		if _, err := rand.Read(cfg.DeviceUUID[:]); err != nil {
			return nil, fmt.Errorf("sc hub: generate device UUID: %w", err)
		}
	}
	return &BACnetSCHub{
		cfg:     cfg,
		clients: make(map[string]*hubConn),
	}, nil
}

// Start begins listening on cfg.ListenAddr (if set).
// The hub can also be embedded in an existing HTTP mux via ServeHTTP.
func (h *BACnetSCHub) Start() error {
	if h.cfg.ListenAddr == "" {
		return nil
	}
	mux := http.NewServeMux()
	mux.Handle("/", h)
	h.httpServer = &http.Server{
		Addr:              h.cfg.ListenAddr,
		Handler:           mux,
		TLSConfig:         h.cfg.TLSConfig,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		var err error
		if h.cfg.TLSConfig != nil {
			err = h.httpServer.ListenAndServeTLS("", "")
		} else {
			err = h.httpServer.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			logger.Error("sc hub: listen: ", err)
		}
	}()
	return nil
}

// Stop shuts down the hub's HTTP server gracefully.
func (h *BACnetSCHub) Stop() error {
	if h.httpServer == nil {
		return nil
	}
	return h.httpServer.Shutdown(context.Background())
}

// ServeHTTP upgrades incoming HTTP requests to WebSocket and handles the
// BACnet/SC hub protocol for each connection.
// It satisfies http.Handler so the hub can be mounted on any ServeMux.
func (h *BACnetSCHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin:  func(_ *http.Request) bool { return true },
		Subprotocols: []string{scSubprotocolHub},
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("sc hub: websocket upgrade: ", err)
		return
	}
	go h.serveConn(ws)
}

// serveConn manages a single client connection through its full lifetime:
// handshake → message loop → cleanup.
func (h *BACnetSCHub) serveConn(ws *websocket.Conn) {
	// --- Handshake: read Connect-Request (AB.5.3.1) ---
	_ = ws.SetReadDeadline(time.Now().Add(10 * time.Second))
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
	clientVMAC := crPayload.VMAC
	clientKey := vmacKey(clientVMAC)

	// Create the client struct early so every write (including Connect-Accept)
	// goes through client.sendRaw/send, serialising all writes with client.wmu.
	client := &hubConn{ws: ws, vmac: clientVMAC}

	// Reject duplicate VMAC (AB.5.3.1: at most one hub connection per node).
	// Register under the same lock that protects the map to prevent TOCTOU races.
	h.mu.Lock()
	if _, exists := h.clients[clientKey]; exists {
		h.mu.Unlock()
		nak := &BVLCResultPayload{
			Function:   BVLCSCFuncConnectRequest,
			ResultCode: 1,
			ErrorMsg:   "duplicate VMAC",
		}
		nakMsg := &BVLCSCMessage{
			Function:  BVLCSCFuncResult,
			MessageID: req.MessageID,
			Payload:   nak.Marshal(),
		}
		// Direct write is safe here — client was never registered so no other
		// goroutine can concurrently call client.send().
		if d, err := nakMsg.Marshal(); err == nil {
			_ = ws.WriteMessage(websocket.BinaryMessage, d)
		}
		_ = ws.Close()
		return
	}
	h.clients[clientKey] = client
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.clients, clientKey)
		h.mu.Unlock()
		_ = ws.Close()
	}()

	// Send Connect-Accept via client.sendRaw so it is serialised with any
	// forwardNPDU calls that arrive immediately after registration.
	caPayload := &ConnectAcceptPayload{
		VMAC:          h.cfg.VMAC,
		DeviceUUID:    h.cfg.DeviceUUID,
		MaxBVLCLength: h.cfg.maxBVLC(),
		MaxNPDULength: h.cfg.maxNPDU(),
	}
	caMsg := &BVLCSCMessage{
		Function:   BVLCSCFuncConnectAccept,
		Control:    ControlOriginVMACPresent,
		MessageID:  req.MessageID,
		OriginVMAC: &h.cfg.VMAC,
		Payload:    caPayload.Marshal(),
	}
	caData, err := caMsg.Marshal()
	if err != nil {
		return
	}
	if err := client.sendRaw(caData); err != nil {
		return
	}

	// --- Message loop ---
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
			h.forwardMessage(BVLCSCFuncEncapsulatedNPDU, msg, client, clientKey)

		case BVLCSCFuncAddressResolution, BVLCSCFuncAddressResolutionACK:
			h.forwardMessage(msg.Function, msg, client, clientKey)

		case BVLCSCFuncAdvertisement, BVLCSCFuncAdvertisementSolicitation:
			h.forwardBroadcast(msg.Function, msg, client, clientKey)

		case BVLCSCFuncProprietaryMessage:
			h.forwardMessage(BVLCSCFuncProprietaryMessage, msg, client, clientKey)

		case BVLCSCFuncHeartbeatRequest:
			ack := &BVLCSCMessage{
				Function:  BVLCSCFuncHeartbeatACK,
				MessageID: msg.MessageID,
			}
			if d, err := ack.Marshal(); err == nil {
				client.send(d)
			}

		case BVLCSCFuncDisconnectRequest:
			ack := &BVLCSCMessage{
				Function:  BVLCSCFuncDisconnectACK,
				MessageID: msg.MessageID,
			}
			if d, err := ack.Marshal(); err == nil {
				client.send(d)
			}
			return // defer handles cleanup

		case BVLCSCFuncResult,
			BVLCSCFuncConnectAccept,
			BVLCSCFuncDisconnectACK,
			BVLCSCFuncHeartbeatACK:
			// silently ignore: valid but not expected in the message loop

		default:
			logger.Trace("sc hub: unhandled function ", msg.Function)
		}
	}
}

// forwardMessage routes fn according to AB.5.3.2/AB.5.3.3/AB.5.5/AB.5.6.
//
// Broadcast (DestVMAC nil or == BroadcastVMAC):
//   - Adds OriginVMAC = sender VMAC, keeps DestVMAC = BroadcastVMAC.
//   - Delivered to every connected client except the sender.
//
// Unicast (DestVMAC set and != BroadcastVMAC):
//   - Adds OriginVMAC = sender VMAC, removes DestVMAC.
//   - Delivered only to the matching client; silently discarded if absent.
func (h *BACnetSCHub) forwardMessage(fn BVLCSCFunction, msg *BVLCSCMessage, from *hubConn, fromKey string) {
	isBroadcast := msg.DestVMAC == nil || *msg.DestVMAC == BroadcastVMAC

	if isBroadcast {
		broadcast := BroadcastVMAC
		fwd := &BVLCSCMessage{
			Function:   fn,
			Control:    ControlOriginVMACPresent | ControlDestVMACPresent,
			MessageID:  msg.MessageID,
			OriginVMAC: &from.vmac,
			DestVMAC:   &broadcast,
			Payload:    msg.Payload,
		}
		fwdData, err := fwd.Marshal()
		if err != nil {
			return
		}
		h.mu.RLock()
		targets := make([]*hubConn, 0, len(h.clients)-1)
		for k, c := range h.clients {
			if k != fromKey {
				targets = append(targets, c)
			}
		}
		h.mu.RUnlock()
		for _, c := range targets {
			c.send(fwdData)
		}
	} else {
		// Unicast: look up the destination client.
		destKey := vmacKey(*msg.DestVMAC)
		h.mu.RLock()
		dest, ok := h.clients[destKey]
		h.mu.RUnlock()
		if !ok {
			return // silently discard (AB.5.3.2)
		}
		fwd := &BVLCSCMessage{
			Function:   fn,
			Control:    ControlOriginVMACPresent,
			MessageID:  msg.MessageID,
			OriginVMAC: &from.vmac,
			Payload:    msg.Payload,
		}
		fwdData, err := fwd.Marshal()
		if err != nil {
			return
		}
		dest.send(fwdData)
	}
}

// forwardBroadcast always sends fn to all connected clients except the sender (AB.5.4.1/AB.5.4.2).
// Used for Advertisement and AdvertisementSolicitation which must always broadcast.
func (h *BACnetSCHub) forwardBroadcast(fn BVLCSCFunction, msg *BVLCSCMessage, from *hubConn, fromKey string) {
	broadcast := BroadcastVMAC
	fwd := &BVLCSCMessage{
		Function:   fn,
		Control:    ControlOriginVMACPresent | ControlDestVMACPresent,
		MessageID:  msg.MessageID,
		OriginVMAC: &from.vmac,
		DestVMAC:   &broadcast,
		Payload:    msg.Payload,
	}
	fwdData, err := fwd.Marshal()
	if err != nil {
		return
	}
	h.mu.RLock()
	targets := make([]*hubConn, 0, len(h.clients)-1)
	for k, c := range h.clients {
		if k != fromKey {
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range targets {
		c.send(fwdData)
	}
}
