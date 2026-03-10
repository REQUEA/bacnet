package linklayer

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/REQUEA/bacnet/logger"
	"github.com/gorilla/websocket"
)

// scConnectionState represents the lifecycle of a single BACnet/SC WebSocket connection.
type scConnectionState int

const (
	connIdle scConnectionState = iota
	connConnecting
	connConnected
	connDisconnecting
)

// scConnection manages a single WebSocket connection to a BACnet/SC peer (hub or direct).
type scConnection struct {
	ws         *websocket.Conn
	remoteVMAC BVMAC
	isHub      bool // drives EncapsulatedNPDU encoding (no OriginVMAC on hub connections)
	state      scConnectionState
	msgIDSeq   uint32 // atomic
	heartbeat  *time.Ticker
	done       chan struct{}
	mu         sync.Mutex
	onMessage  func(msg *BVLCSCMessage)
	onClosed   func(vmac BVMAC)
	// heartbeat state; protected by hbMu
	hbMu           sync.Mutex
	lastReceived   time.Time // time of last received BVLC message
	pendingHBMsgID uint16    // non-zero while a Heartbeat-Request awaits ACK
}

func newSCConnection(ws *websocket.Conn, remoteVMAC BVMAC, isHub bool,
	onMessage func(*BVLCSCMessage), onClosed func(BVMAC)) *scConnection {
	return &scConnection{
		ws:           ws,
		remoteVMAC:   remoteVMAC,
		isHub:        isHub,
		state:        connConnected,
		done:         make(chan struct{}),
		onMessage:    onMessage,
		onClosed:     onClosed,
		lastReceived: time.Now(),
	}
}

// nextMsgID returns the next message ID (wrapping uint16).
func (c *scConnection) nextMsgID() uint16 {
	return uint16(atomic.AddUint32(&c.msgIDSeq, 1)) //nolint:gosec
}

// sendMessage marshals msg and writes it as a binary WebSocket frame.
func (c *scConnection) sendMessage(msg *BVLCSCMessage) error {
	data, err := msg.Marshal()
	if err != nil {
		return fmt.Errorf("sc: marshal message: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ws.WriteMessage(websocket.BinaryMessage, data); err != nil {
		return fmt.Errorf("sc: write websocket frame: %w", err)
	}
	return nil
}

// recvLoop reads binary frames from the WebSocket connection and dispatches them.
// It runs as a goroutine and stops when the connection is closed.
func (c *scConnection) recvLoop() {
	defer func() {
		close(c.done)
		if c.onClosed != nil {
			c.onClosed(c.remoteVMAC)
		}
	}()
	for {
		msgType, data, err := c.ws.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				logger.Trace("sc: connection closed: ", err)
			}
			return
		}
		if msgType != websocket.BinaryMessage {
			logger.Trace("sc: ignoring non-binary WebSocket message")
			continue
		}
		msg := &BVLCSCMessage{}
		if err := msg.Unmarshal(data); err != nil {
			logger.Error("sc: unmarshal bvlcsc message: ", err)
			continue
		}
		c.hbMu.Lock()
		c.lastReceived = time.Now()
		c.pendingHBMsgID = 0 // any received message proves connection is alive
		c.hbMu.Unlock()
		if c.onMessage != nil {
			c.onMessage(msg)
		}
	}
}

// startHeartbeat launches a goroutine that sends Heartbeat-Request per the standard:
// only if no BVLC message has been received within the interval, and closes the
// connection if a pending heartbeat is not acknowledged within the next interval.
func (c *scConnection) startHeartbeat(interval time.Duration) {
	c.heartbeat = time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-c.done:
				c.heartbeat.Stop()
				return
			case <-c.heartbeat.C:
				c.hbMu.Lock()
				pending := c.pendingHBMsgID
				elapsed := time.Since(c.lastReceived)
				c.hbMu.Unlock()

				if pending != 0 {
					// Previous heartbeat was not acknowledged — connection is dead.
					logger.Error("sc: heartbeat timeout, closing connection")
					_ = c.ws.Close()
					return
				}
				if elapsed < interval {
					// Recent activity — no heartbeat needed.
					continue
				}

				msgID := c.nextMsgID()
				c.hbMu.Lock()
				c.pendingHBMsgID = msgID
				c.hbMu.Unlock()

				msg := &BVLCSCMessage{
					Function:  BVLCSCFuncHeartbeatRequest,
					MessageID: msgID,
				}
				if err := c.sendMessage(msg); err != nil {
					logger.Error("sc: send heartbeat: ", err)
					return
				}
			}
		}
	}()
}

// disconnect sends a Disconnect-Request and closes the WebSocket connection.
func (c *scConnection) disconnect() {
	c.mu.Lock()
	if c.state == connDisconnecting || c.state == connIdle {
		c.mu.Unlock()
		return
	}
	c.state = connDisconnecting
	c.mu.Unlock()

	msg := &BVLCSCMessage{
		Function:  BVLCSCFuncDisconnectRequest,
		MessageID: c.nextMsgID(),
	}
	if err := c.sendMessage(msg); err != nil {
		logger.Trace("sc: send disconnect request: ", err)
	}

	// Give peer a moment to send Disconnect-ACK before hard close.
	_ = c.ws.SetReadDeadline(time.Now().Add(2 * time.Second))

	// Wait for recvLoop to finish or timeout.
	select {
	case <-c.done:
	case <-time.After(3 * time.Second):
	}
	_ = c.ws.Close()
}
