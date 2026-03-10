package linklayer

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/logger"
	"github.com/gorilla/websocket"
)

const (
	defaultHeartbeatInterval        = 60 * time.Second
	defaultAdvertiseInterval        = 300 * time.Second
	defaultMaxAPDU           uint16 = 65535

	// BACnet/SC WebSocket subprotocol identifiers (ASHRAE 135-2024 Annex AB).
	scSubprotocolHub           = "hub.bsc.bacnet.org"
	scSubprotocolDirectConnect = "dc.bsc.bacnet.org"
)

// BACnetSCConfig holds the configuration for a BACnet/SC datalink.
type BACnetSCConfig struct {
	// VMAC is this device's Virtual MAC Address. Auto-generated if zero.
	VMAC BVMAC
	// DeviceUUID is the 16-byte device UUID sent in Connect-Request/Accept. Auto-generated if zero.
	DeviceUUID [16]byte
	// TLSConfig is the TLS configuration. When nil, InsecureSkipVerify is used (dev mode).
	TLSConfig *tls.Config
	// PrimaryHubURL is the primary hub WebSocket URL (wss://host:port/path).
	PrimaryHubURL string
	// FailoverHubURL is an optional failover hub URL.
	FailoverHubURL string
	// DirectConnectURL is the listen address for incoming direct connections (e.g. ":9999").
	DirectConnectURL string
	// HeartbeatInterval is how often to send Heartbeat-Requests (default 60s).
	HeartbeatInterval time.Duration
	// AdvertiseInterval is how often to broadcast Advertisement (default 300s).
	AdvertiseInterval time.Duration
	// MaxAPDU is the maximum APDU length advertised (default 65535).
	MaxAPDU uint16
}

func (c *BACnetSCConfig) tlsConfig() *tls.Config {
	if c.TLSConfig != nil {
		return c.TLSConfig
	}
	return &tls.Config{InsecureSkipVerify: true} //nolint:gosec // dev mode
}

func (c *BACnetSCConfig) heartbeatInterval() time.Duration {
	if c.HeartbeatInterval > 0 {
		return c.HeartbeatInterval
	}
	return defaultHeartbeatInterval
}

func (c *BACnetSCConfig) advertiseInterval() time.Duration {
	if c.AdvertiseInterval > 0 {
		return c.AdvertiseInterval
	}
	return defaultAdvertiseInterval
}

func (c *BACnetSCConfig) maxAPDU() uint16 {
	if c.MaxAPDU > 0 {
		return c.MaxAPDU
	}
	return defaultMaxAPDU
}

// BACnetSCDatalink is the BACnet/SC datalink layer.
// It manages WebSocket connections to a hub and/or direct peers.
type BACnetSCDatalink struct {
	cfg         BACnetSCConfig
	ports       []*BACnetSCPort
	primaryHub  *scConnection            // nil when not connected
	failoverHub *scConnection            // nil when not connected
	connections map[string]*scConnection // direct peer connections only
	mu          sync.RWMutex
	httpServer  *http.Server
	stopCh      chan struct{}
	wg          sync.WaitGroup
}

// NewBACnetSCDatalink creates a new BACnet/SC datalink with the given config.
// If cfg.VMAC is all-zeros a random VMAC is generated.
// If cfg.DeviceUUID is all-zeros a random UUID is generated.
func NewBACnetSCDatalink(cfg BACnetSCConfig) (*BACnetSCDatalink, error) {
	if cfg.VMAC == (BVMAC{}) {
		v, err := NewRandomVMAC()
		if err != nil {
			return nil, fmt.Errorf("sc: generate VMAC: %w", err)
		}
		cfg.VMAC = v
	}
	var zeroUUID [16]byte
	if cfg.DeviceUUID == zeroUUID {
		if _, err := rand.Read(cfg.DeviceUUID[:]); err != nil {
			return nil, fmt.Errorf("sc: generate device UUID: %w", err)
		}
	}
	return &BACnetSCDatalink{
		cfg:         cfg,
		connections: make(map[string]*scConnection),
		stopCh:      make(chan struct{}),
	}, nil
}

// VMAC returns this device's Virtual MAC Address.
func (dl *BACnetSCDatalink) VMAC() BVMAC {
	return dl.cfg.VMAC
}

// AddPort registers a BACnetSCPort with this datalink.
func (dl *BACnetSCDatalink) AddPort(p *BACnetSCPort) {
	dl.ports = append(dl.ports, p)
	p.datalink = dl
}

// Start dials the hub (if configured) and starts the direct-connect listener (if configured).
func (dl *BACnetSCDatalink) Start() error {
	if dl.cfg.PrimaryHubURL != "" {
		conn, err := dl.connect(dl.cfg.PrimaryHubURL, true)
		if err != nil {
			logger.Error("sc: connect to primary hub: ", err)
			// Non-fatal: device operates without hub connectivity.
		} else {
			dl.mu.Lock()
			dl.primaryHub = conn
			dl.mu.Unlock()
		}
	}

	if dl.cfg.DirectConnectURL != "" {
		if err := dl.startDirectConnectListener(); err != nil {
			return fmt.Errorf("sc: start direct connect listener: %w", err)
		}
	}

	// Periodic advertisement.
	dl.wg.Add(1)
	go func() {
		defer dl.wg.Done()
		ticker := time.NewTicker(dl.cfg.advertiseInterval())
		defer ticker.Stop()
		for {
			select {
			case <-dl.stopCh:
				return
			case <-ticker.C:
				dl.advertise()
			}
		}
	}()
	return nil
}

// Stop disconnects all connections and shuts down the HTTP server.
func (dl *BACnetSCDatalink) Stop() error {
	close(dl.stopCh)
	dl.wg.Wait()

	dl.mu.Lock()
	conns := make([]*scConnection, 0, len(dl.connections)+2)
	if dl.primaryHub != nil {
		conns = append(conns, dl.primaryHub)
	}
	if dl.failoverHub != nil {
		conns = append(conns, dl.failoverHub)
	}
	for _, c := range dl.connections {
		conns = append(conns, c)
	}
	dl.mu.Unlock()

	for _, c := range conns {
		c.disconnect()
	}

	if dl.httpServer != nil {
		return dl.httpServer.Shutdown(context.Background())
	}
	return nil
}

// Send wraps npdu in an EncapsulatedNPDU and routes it to destVMAC.
// If destVMAC is nil or all-zeros, broadcasts via hub (no DestVMAC in header).
func (dl *BACnetSCDatalink) Send(npdu []byte, destVMAC []byte) error {
	var dest BVMAC
	if len(destVMAC) == 6 {
		copy(dest[:], destVMAC)
	}

	if dest != BroadcastVMAC {
		// Unicast: try direct connection first, fall back to hub.
		conn := dl.getConnection(dest)
		if conn == nil {
			conn = dl.getHubConnection()
		}
		if conn == nil {
			return fmt.Errorf("sc: no connection available for dest %s", dest.String())
		}
		msg := dl.buildEncapsulatedNPDU(npdu, &dest, conn.isHub)
		msg.MessageID = conn.nextMsgID()
		return conn.sendMessage(msg)
	}

	// Broadcast via hub using the all-ones broadcast VMAC as destination.
	conn := dl.getHubConnection()
	if conn == nil {
		return fmt.Errorf("sc: no hub connection for broadcast")
	}
	broadcast := BroadcastVMAC
	msg := dl.buildEncapsulatedNPDU(npdu, &broadcast, true)
	msg.MessageID = conn.nextMsgID()
	return conn.sendMessage(msg)
}

// buildEncapsulatedNPDU constructs an EncapsulatedNPDU message.
// Per Annex AB: hub connections omit OriginVMAC (hub knows sender from the connection)
// and always include DestVMAC (BroadcastVMAC for broadcast, target VMAC for unicast).
// Direct connections include both OriginVMAC and DestVMAC.
func (dl *BACnetSCDatalink) buildEncapsulatedNPDU(npdu []byte, dest *BVMAC, isHub bool) *BVLCSCMessage {
	if isHub {
		return &BVLCSCMessage{
			Function: BVLCSCFuncEncapsulatedNPDU,
			Control:  ControlDestVMACPresent,
			DestVMAC: dest,
			Payload:  npdu,
		}
	}
	return &BVLCSCMessage{
		Function:   BVLCSCFuncEncapsulatedNPDU,
		Control:    ControlOriginVMACPresent | ControlDestVMACPresent,
		OriginVMAC: &dl.cfg.VMAC,
		DestVMAC:   dest,
		Payload:    npdu,
	}
}

// getHubConnection returns the active hub connection (primary preferred over failover).
func (dl *BACnetSCDatalink) getHubConnection() *scConnection {
	dl.mu.RLock()
	defer dl.mu.RUnlock()
	if dl.primaryHub != nil && dl.primaryHub.state == connConnected {
		return dl.primaryHub
	}
	if dl.failoverHub != nil && dl.failoverHub.state == connConnected {
		return dl.failoverHub
	}
	return nil
}

// getConnection returns a non-hub direct connection to vmac (if any).
func (dl *BACnetSCDatalink) getConnection(vmac BVMAC) *scConnection {
	dl.mu.RLock()
	defer dl.mu.RUnlock()
	key := vmacKey(vmac)
	c, ok := dl.connections[key]
	if ok && c.state == connConnected {
		return c
	}
	return nil
}

// onMessage dispatches an incoming BVLCSCMessage.
func (dl *BACnetSCDatalink) onMessage(msg *BVLCSCMessage) {
	switch msg.Function {
	case BVLCSCFuncEncapsulatedNPDU:
		dl.handleEncapsulatedNPDU(msg)

	case BVLCSCFuncAddressResolutionACK:
		dl.handleAddressResolutionACK(msg)

	case BVLCSCFuncAdvertisement:
		dl.handleAdvertisement(msg)

	case BVLCSCFuncAdvertisementSolicitation:
		dl.advertise()

	case BVLCSCFuncConnectAccept:
		// Already handled inside connect(); ignore duplicates.

	case BVLCSCFuncDisconnectRequest:
		dl.handleDisconnectRequest(msg)

	case BVLCSCFuncHeartbeatRequest:
		dl.handleHeartbeatRequest(msg)

	case BVLCSCFuncHeartbeatACK:
		// Expected response to our Heartbeat-Request; no action needed.

	case BVLCSCFuncDisconnectACK:
		// Expected response to our Disconnect-Request; handled in disconnect().

	case BVLCSCFuncResult:
		p := &BVLCResultPayload{}
		if err := p.Unmarshal(msg.Payload); err == nil && p.ResultCode != 0 {
			logger.Error("sc: received BVLC-Result error for function ",
				p.Function, ": code=", p.ResultCode, " msg=", p.ErrorMsg)
		}

	default:
		logger.Trace("sc: unhandled BVLC-SC function ", msg.Function)
	}
}

func (dl *BACnetSCDatalink) handleEncapsulatedNPDU(msg *BVLCSCMessage) {
	if len(dl.ports) == 0 {
		return
	}
	port := dl.ports[0]
	if port.npduHandler == nil {
		return
	}
	var sadr bacnet.MAC
	if msg.OriginVMAC != nil {
		v := *msg.OriginVMAC
		sadr = &v
	} else {
		sadr = &dl.cfg.VMAC
	}
	dadr := bacnet.MAC(&dl.cfg.VMAC)
	if err := port.npduHandler.HandleNPDU(dadr, sadr, msg.Payload); err != nil {
		logger.Error("sc: handle NPDU: ", err)
	}
}

func (dl *BACnetSCDatalink) handleAddressResolutionACK(msg *BVLCSCMessage) {
	p := &AddressResolutionACKPayload{}
	if err := p.Unmarshal(msg.Payload); err != nil {
		logger.Error("sc: parse address-resolution-ack: ", err)
		return
	}
	if len(p.URIs) == 0 {
		return
	}
	// Attempt a direct connection to the first URI.
	go func() {
		conn, err := dl.connect(p.URIs[0], false)
		if err != nil {
			logger.Error("sc: direct connect after address resolution: ", err)
			return
		}
		dl.mu.Lock()
		dl.connections[vmacKey(conn.remoteVMAC)] = conn
		dl.mu.Unlock()
	}()
}

func (dl *BACnetSCDatalink) handleAdvertisement(msg *BVLCSCMessage) {
	p := &AdvertisementPayload{}
	if err := p.Unmarshal(msg.Payload); err != nil {
		logger.Error("sc: parse advertisement: ", err)
		return
	}
	var vmacStr string
	if msg.OriginVMAC != nil {
		vmacStr = msg.OriginVMAC.String()
	}
	logger.Trace("sc: advertisement from ", vmacStr, " maxBVLC=", p.MaxBVLCLength, " maxNPDU=", p.MaxNPDULength)
}

func (dl *BACnetSCDatalink) handleDisconnectRequest(msg *BVLCSCMessage) {
	ack := &BVLCSCMessage{
		Function:  BVLCSCFuncDisconnectACK,
		MessageID: msg.MessageID,
	}
	// Find the connection that sent the disconnect request.
	if msg.OriginVMAC != nil {
		conn := dl.getConnection(*msg.OriginVMAC)
		if conn == nil {
			conn = dl.getHubConnection()
		}
		if conn != nil {
			_ = conn.sendMessage(ack)
		}
	}
}

func (dl *BACnetSCDatalink) handleHeartbeatRequest(msg *BVLCSCMessage) {
	ack := &BVLCSCMessage{
		Function:  BVLCSCFuncHeartbeatACK,
		MessageID: msg.MessageID,
	}
	if msg.OriginVMAC != nil {
		conn := dl.getConnection(*msg.OriginVMAC)
		if conn == nil {
			conn = dl.getHubConnection()
		}
		if conn != nil {
			_ = conn.sendMessage(ack)
		}
	}
}

// connect dials a WebSocket URL, performs the Connect-Request/Accept handshake,
// and sends Advertisement. The caller is responsible for storing the returned connection.
func (dl *BACnetSCDatalink) connect(url string, isHub bool) (*scConnection, error) {
	// state: IDLE
	logger.Trace("BACnetSCDatalink.connect: IDLE")
	subprotocol := scSubprotocolHub
	if !isHub {
		subprotocol = scSubprotocolDirectConnect
	}
	dialer := websocket.Dialer{
		TLSClientConfig:  dl.cfg.tlsConfig(),
		HandshakeTimeout: 10 * time.Second,
		Subprotocols:     []string{subprotocol},
	}
	ws, httpResp, err := dialer.Dial(url, nil)
	if err != nil {
		return nil, fmt.Errorf("sc: dial %s: %w", url, err)
	}
	if httpResp != nil && httpResp.Body != nil {
		_ = httpResp.Body.Close()
	}

	// state: AWAITING_WEBSOCKET
	logger.Trace("BACnetSCDatalink.connect: AWAITING_WEBSOCKET")
	// Send Connect-Request.
	crPayload := &ConnectRequestPayload{
		VMAC:          dl.cfg.VMAC,
		DeviceUUID:    dl.cfg.DeviceUUID,
		MaxBVLCLength: dl.cfg.maxAPDU(),
		MaxNPDULength: dl.cfg.maxAPDU(),
	}
	crMsg := &BVLCSCMessage{
		Function:  BVLCSCFuncConnectRequest,
		Control:   0,
		MessageID: 1,
		Payload:   crPayload.Marshal(),
	}
	crData, err := crMsg.Marshal()
	if err != nil {
		_ = ws.Close()
		return nil, fmt.Errorf("sc: marshal connect-request: %w", err)
	}
	if err := ws.WriteMessage(websocket.BinaryMessage, crData); err != nil {
		_ = ws.Close()
		return nil, fmt.Errorf("sc: send connect-request: %w", err)
	}
	// state: AWAITING_ACCEPT
	logger.Trace("BACnetSCDatalink.connect: AWAITING_ACCEPT")

	// Await Connect-Accept.
	_ = ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, data, err := ws.ReadMessage()
	if err != nil {
		_ = ws.Close()
		return nil, fmt.Errorf("sc: read connect-accept: %w", err)
	}
	_ = ws.SetReadDeadline(time.Time{})

	resp := &BVLCSCMessage{}
	if err := resp.Unmarshal(data); err != nil {
		_ = ws.Close()
		return nil, fmt.Errorf("sc: unmarshal connect-accept: %w", err)
	}
	if resp.Function != BVLCSCFuncConnectAccept {
		_ = ws.Close()
		return nil, fmt.Errorf("sc: expected connect-accept (0x07), got 0x%02X", resp.Function)
	}

	logger.Trace("BACnetSCDatalink.connect: CONNECTED")

	var remoteVMAC BVMAC
	if resp.OriginVMAC != nil {
		remoteVMAC = *resp.OriginVMAC
	}

	conn := newSCConnection(ws, remoteVMAC, isHub, dl.onMessage, dl.onConnectionClosed)

	// Start receive loop.
	go conn.recvLoop()

	// Send Advertisement.
	dl.sendAdvertisement(conn)

	// Start heartbeat.
	conn.startHeartbeat(dl.cfg.heartbeatInterval())

	logger.Trace("sc: connected to ", url, " remoteVMAC=", remoteVMAC.String())
	return conn, nil
}

// serveDirectConnect is the HTTP handler for incoming BACnet/SC WebSocket connections.
func (dl *BACnetSCDatalink) serveDirectConnect(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin:  func(_ *http.Request) bool { return true },
		Subprotocols: []string{scSubprotocolDirectConnect, scSubprotocolHub},
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("sc: websocket upgrade: ", err)
		return
	}

	// Read Connect-Request from peer.
	_ = ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, data, err := ws.ReadMessage()
	if err != nil {
		logger.Error("sc: read connect-request: ", err)
		_ = ws.Close()
		return
	}
	_ = ws.SetReadDeadline(time.Time{})

	req := &BVLCSCMessage{}
	if err := req.Unmarshal(data); err != nil {
		logger.Error("sc: unmarshal connect-request: ", err)
		_ = ws.Close()
		return
	}
	if req.Function != BVLCSCFuncConnectRequest {
		logger.Error("sc: expected connect-request, got ", req.Function)
		_ = ws.Close()
		return
	}

	crPayload := &ConnectRequestPayload{}
	if err := crPayload.Unmarshal(req.Payload); err != nil {
		logger.Error("sc: parse connect-request payload: ", err)
		_ = ws.Close()
		return
	}

	// Send Connect-Accept.
	caPayload := &ConnectAcceptPayload{
		VMAC:          dl.cfg.VMAC,
		MaxBVLCLength: dl.cfg.maxAPDU(),
		MaxNPDULength: dl.cfg.maxAPDU(),
		DeviceUUID:    dl.cfg.DeviceUUID,
	}
	caMsg := &BVLCSCMessage{
		Function:   BVLCSCFuncConnectAccept,
		Control:    ControlOriginVMACPresent,
		MessageID:  req.MessageID,
		OriginVMAC: &dl.cfg.VMAC,
		Payload:    caPayload.Marshal(),
	}
	caData, err := caMsg.Marshal()
	if err != nil {
		logger.Error("sc: marshal connect-accept: ", err)
		_ = ws.Close()
		return
	}
	if err := ws.WriteMessage(websocket.BinaryMessage, caData); err != nil {
		logger.Error("sc: send connect-accept: ", err)
		_ = ws.Close()
		return
	}

	conn := newSCConnection(ws, crPayload.VMAC, false, dl.onMessage, dl.onConnectionClosed)

	dl.mu.Lock()
	dl.connections[vmacKey(crPayload.VMAC)] = conn
	dl.mu.Unlock()

	go conn.recvLoop()
	dl.sendAdvertisement(conn)

	logger.Trace("sc: accepted direct connection from ", crPayload.VMAC.String())
}

// startDirectConnectListener starts an HTTP server that accepts BACnet/SC WebSocket connections.
func (dl *BACnetSCDatalink) startDirectConnectListener() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", dl.serveDirectConnect)
	dl.httpServer = &http.Server{
		Addr:              dl.cfg.DirectConnectURL,
		Handler:           mux,
		TLSConfig:         dl.cfg.tlsConfig(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	dl.wg.Add(1)
	go func() {
		defer dl.wg.Done()
		var err error
		if dl.cfg.TLSConfig != nil {
			err = dl.httpServer.ListenAndServeTLS("", "")
		} else {
			err = dl.httpServer.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			logger.Error("sc: direct connect listener: ", err)
		}
	}()
	return nil
}

// hubConnectionState returns the current hub connection state.
func (dl *BACnetSCDatalink) hubConnectionState() HubConnectionState {
	dl.mu.RLock()
	defer dl.mu.RUnlock()
	if dl.primaryHub != nil && dl.primaryHub.state == connConnected {
		return HubConnectionConnected
	}
	if dl.failoverHub != nil && dl.failoverHub.state == connConnected {
		return HubConnectionFailover
	}
	return HubConnectionNoHub
}

// advertise sends an Advertisement message on all current connections.
func (dl *BACnetSCDatalink) advertise() {
	p := &AdvertisementPayload{
		HubConnectionState:      dl.hubConnectionState(),
		AcceptDirectConnections: dl.cfg.DirectConnectURL != "",
		MaxBVLCLength:           dl.cfg.maxAPDU(),
		MaxNPDULength:           dl.cfg.maxAPDU(),
	}
	msg := &BVLCSCMessage{
		Function:   BVLCSCFuncAdvertisement,
		Control:    ControlOriginVMACPresent,
		OriginVMAC: &dl.cfg.VMAC,
		Payload:    p.Marshal(),
	}

	dl.mu.RLock()
	conns := make([]*scConnection, 0, len(dl.connections)+2)
	if dl.primaryHub != nil {
		conns = append(conns, dl.primaryHub)
	}
	if dl.failoverHub != nil {
		conns = append(conns, dl.failoverHub)
	}
	for _, c := range dl.connections {
		conns = append(conns, c)
	}
	dl.mu.RUnlock()

	for _, c := range conns {
		if c.state != connConnected {
			continue
		}
		m := *msg
		m.MessageID = c.nextMsgID()
		if err := c.sendMessage(&m); err != nil {
			logger.Error("sc: send advertisement: ", err)
		}
	}
}

// sendAdvertisement sends an Advertisement on a single connection.
func (dl *BACnetSCDatalink) sendAdvertisement(conn *scConnection) {
	p := &AdvertisementPayload{
		HubConnectionState:      dl.hubConnectionState(),
		AcceptDirectConnections: dl.cfg.DirectConnectURL != "",
		MaxBVLCLength:           dl.cfg.maxAPDU(),
		MaxNPDULength:           dl.cfg.maxAPDU(),
	}
	msg := &BVLCSCMessage{
		Function:   BVLCSCFuncAdvertisement,
		Control:    ControlOriginVMACPresent,
		MessageID:  conn.nextMsgID(),
		OriginVMAC: &dl.cfg.VMAC,
		Payload:    p.Marshal(),
	}
	if err := conn.sendMessage(msg); err != nil {
		logger.Error("sc: send advertisement: ", err)
	}
}

// onConnectionClosed removes the closed connection from hub fields or the peer map.
func (dl *BACnetSCDatalink) onConnectionClosed(vmac BVMAC) {
	dl.mu.Lock()
	switch {
	case dl.primaryHub != nil && dl.primaryHub.remoteVMAC == vmac:
		dl.primaryHub = nil
	case dl.failoverHub != nil && dl.failoverHub.remoteVMAC == vmac:
		dl.failoverHub = nil
	default:
		delete(dl.connections, vmacKey(vmac))
	}
	dl.mu.Unlock()
	logger.Trace("sc: connection closed for VMAC ", vmac.String())
}

func vmacKey(v BVMAC) string {
	return hex.EncodeToString(v[:])
}

// ---------------------------------------------------------------------------
// BACnetSCPort — implements linklayer.DatalinkPort
// ---------------------------------------------------------------------------

// BACnetSCPort is a single logical port on a BACnetSCDatalink.
// It implements the DatalinkPort interface used by the network layer.
type BACnetSCPort struct {
	datalink    *BACnetSCDatalink
	npduHandler NPDUHandler
}

// NewBACnetSCPort creates a port and registers it with the given datalink.
func NewBACnetSCPort(dl *BACnetSCDatalink) *BACnetSCPort {
	p := &BACnetSCPort{}
	dl.AddPort(p)
	return p
}

// SetNPDUHandler registers the network layer handler.
func (p *BACnetSCPort) SetNPDUHandler(h NPDUHandler) {
	p.npduHandler = h
}

// Mac implements DatalinkPort; returns the device's own VMAC.
func (p *BACnetSCPort) Mac() bacnet.MAC {
	return &p.datalink.cfg.VMAC
}

// BroadcastMac implements DatalinkPort; returns the all-zeros broadcast VMAC.
func (p *BACnetSCPort) BroadcastMac() bacnet.MAC {
	v := BroadcastVMAC
	return &v
}

// Send implements DatalinkPort; delegates to the datalink's Send method.
func (p *BACnetSCPort) Send(data []byte, destMAC []byte) error {
	return p.datalink.Send(data, destMAC)
}

// MaxPDULength implements DatalinkPort.
func (p *BACnetSCPort) MaxPDULength() uint {
	return uint(p.datalink.cfg.maxAPDU())
}
