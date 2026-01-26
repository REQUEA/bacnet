package bacip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/REQUEA/bacnet"
)

const (
	broadcastDNET = 0xffff
)

type NetworkNumber uint16

type MAC interface {
	GetBytes() []byte
	FromBytes([]byte)
	String() string
}

// TODO: should be an interface to allow for the implementation of technologies
// other than IP
type Port struct {
	Id            int // used for management, cannot be 0
	Dnet          NetworkNumber
	Mac           MAC
	BroadcastMac  MAC
	portInfo      []byte
	networkEntity NetworkEntity
	datalink      DatalinkEntity
}

func (p *Port) SetDatalink(e DatalinkEntity) {
	p.datalink = e
}

func (p *Port) SetPortInfo(portInfo []byte) {
	p.portInfo = portInfo
}

func (p *Port) PortInfo() []byte {
	return p.portInfo
}

func (p *Port) SetNetworkEntity(e NetworkEntity) {
	p.networkEntity = e
}

func (p *Port) ToNetworkEntity(sadr MAC, buf []byte) error {
	// TODO: might have to place the incoming message in an input queue for
	// the network entity
	return p.networkEntity.Handle(p, sadr, buf)
}

func (p *Port) ToDataLink(npdu *NPDU, destMAC []byte) error {
	data, err := npdu.MarshalBinary()
	if err != nil {
		return fmt.Errorf("could not marshal NPDU: %w", err)
	}
	if destMAC != nil {
		return p.datalink.Send(data, destMAC)
	}
	return p.datalink.Send(data, p.BroadcastMac.GetBytes())
}

type NetworkEntity interface {
	Handle(source *Port, sadr MAC, buf []byte) error
}

type RoutingTableEntry struct {
	// NextHop can be nil (after receiving Initialize-Routing-Table"), maybe
	// put this information in the port structure
	NextHop MAC
	Port    *Port
}

type RouterNetworkEntity struct {
	ports        []*Port
	scheduler    Scheduler[*NPDU]
	routingTable map[NetworkNumber]RoutingTableEntry
	routingCond  sync.Cond
}

func NewRouterNetworkEntity() *RouterNetworkEntity {
	result := RouterNetworkEntity{
		ports:        make([]*Port, 0),
		scheduler:    NewWRRScheduler[*NPDU](1, 2, 4, 8),
		routingTable: make(map[NetworkNumber]RoutingTableEntry),
		routingCond: sync.Cond{
			L: &sync.Mutex{},
		},
	}
	return &result
}

func (e *RouterNetworkEntity) AddPort(p *Port) *RouterNetworkEntity {
	// TODO: add check for existing port with the same DNET
	e.ports = append(e.ports, p)
	return e
}

func (ne *RouterNetworkEntity) Start() {
	go ne.route()
}

func (ne *RouterNetworkEntity) route() {
routeloop:
	for {
		var npdu *NPDU
		var ok bool
		ne.routingCond.L.Lock()
		for {
			npdu, ok = ne.scheduler.GetNext()
			if !ok {
				ne.routingCond.Wait()
				//time.Sleep(10 * time.Millisecond)
			} else {
				break
			}
		}
		ne.routingCond.L.Unlock()
		logger.Trace("routing NPDU ", npdu)
		if npdu.Destination.Net == broadcastDNET {
			logger.Trace("npdu for broadcast DNET")
			// TODO: need to add SNET to the npdu header before scheduling
			npdu.HopCount--
			if npdu.HopCount > 0 {
				// broadcast on all ports except source
				logger.Trace("broadcasting on all ports except port ", npdu.Source.Net)
				for _, p := range ne.ports {
					if p.Dnet != NetworkNumber(npdu.Source.Net) {
						logger.Trace("passing npdu to port ", p.Dnet)
						err := p.ToDataLink(npdu, nil)
						if err != nil {
							logger.Error("error sending to datalink: ", err)
						}
					}
				}
			}
		} else {
			logger.Trace("npdu for network ", npdu.Destination.Net)
			dnet := NetworkNumber(npdu.Destination.Net)
			for _, p := range ne.ports {
				if p.Dnet == dnet {
					logger.Trace("routing to directly connected network ", npdu.Destination.Net)
					dstMac := npdu.Destination.Adr
					npdu.SetIsDestPresent(false)
					err := p.ToDataLink(npdu, dstMac)
					if err != nil {
						logger.Error("error sending to datalink: ", err)
					}
					continue routeloop
				}
			}
			dest, ok := ne.routingTable[dnet]
			if ok {
				npdu.HopCount--
				if npdu.HopCount > 0 {
					logger.Trace("routing to next router for network ", dest.Port.Dnet)
					err := dest.Port.ToDataLink(npdu, dest.NextHop.GetBytes())
					if err != nil {
						logger.Error("error sending to datalink: ", err)
					}
				}
			} else {
				logger.Trace("no route for DNET ", npdu.Destination.Net)
				newNpdu := NewNPDU().SetNetworkMessageType(WhoIsRouterToNetwork)
				for _, p := range ne.ports {
					err := p.ToDataLink(newNpdu, nil)
					if err != nil {
						logger.Error("error sending to datalink: ", err)
					}
				}
			}
		}
	}
}

func (ne *RouterNetworkEntity) schedule(priority int, npdu *NPDU) error {
	ne.routingCond.L.Lock()
	defer ne.routingCond.L.Unlock()
	err := ne.scheduler.Schedule(priority, npdu)
	if err != nil {
		return err
	}
	// wake up routing goroutine
	ne.routingCond.Broadcast()
	return nil
}

func (ne *RouterNetworkEntity) Handle(source *Port, sadr MAC, buf []byte) error {
	if buf[0] != uint8(Version1) {
		return fmt.Errorf("incorrect bacnet version: %d", buf[0])
	}
	npdu := &NPDU{}
	err := npdu.UnmarshalBinary(buf)
	if err != nil {
		return err
	}
	logger.Trace("Handle(", source.Id, ", ", npdu, ")")
	if npdu.IsNetworkMessage() {
		if !npdu.IsDestPresent() {
			logger.Trace("Network message for local network entity")
			return ne.handleNetworkLayerMessage(source, sadr, npdu)
		} else if npdu.Destination.Net == broadcastDNET { // DNET present and broadcast
			err := ne.handleNetworkLayerMessage(source, sadr, npdu)
			if err != nil {
				logger.Error("error handling network message: ", err)
				return err
			}
			err = ne.schedule(int(npdu.GetPriority()), npdu)
			if err != nil {
				logger.Error("error scheduling global broadcast network message: ", err)
				return err
			}
			return nil
		} else {
			if npdu.NetworkMessageType == RejectMessageToNetwork {
				// TODO handle RejectMessageToNetwork
			}
			err := ne.schedule(int(npdu.GetPriority()), npdu)
			if err != nil {
				logger.Error("error scheduling message for dnet ", npdu.Destination.Net, ": ", err)
				return err
			}
			return nil
		}
	} else {
		if !npdu.IsDestPresent() {
			logger.Trace("Handle: npdu for local application layer")
			// find bacnet application entity
			// if found pass payload to the application entity
		} else if npdu.Destination.Net == broadcastDNET { // DNET present and broadcast
			logger.Trace("Handle: npdu for broadcast DNET")
			// find bacnet application entity
			// if found pass payload to the application entity
			npdu.Source = &bacnet.Address{
				Net: uint16(source.Dnet),
				Adr: sadr.GetBytes(),
			}
			npdu.SetIsSourcePresent(true)
			err := ne.schedule(int(npdu.GetPriority()), npdu)
			if err != nil {
				logger.Error("error scheduling global broadcast application message")
				return err
			}
		} else {
			logger.Trace("Handle: npdu for DNET ", npdu.Destination.Net)
			npdu.Source = &bacnet.Address{
				Net: uint16(source.Dnet),
				Adr: sadr.GetBytes(),
			}
			npdu.SetIsSourcePresent(true)
			err := ne.schedule(int(npdu.GetPriority()), npdu)
			if err != nil {
				logger.Error("error shceduling application message for network ", npdu.Destination.Net, ": ", err)
				return err
			}
		}
	}
	return nil
}

func (ne *RouterNetworkEntity) handleNetworkLayerMessage(source *Port, sadr MAC, npdu *NPDU) error {
	switch npdu.NetworkMessageType {
	case WhoIsRouterToNetwork:
		return ne.handleWhoIsRouterToNetwork(source, sadr, npdu)
	case IAmRouterToNetwork:
		return ne.handleIAmRouterToNetwork(source, sadr, npdu)
	case ICouldBeRouterToNetwork:
		logger.Trace(npdu.NetworkMessageType, " is not supported")
	case RejectMessageToNetwork:
		logger.Trace(npdu.NetworkMessageType, " is not supported")
	case RouterBusyToNewtork:
		logger.Trace(npdu.NetworkMessageType, " is not supported")
	case RouterAvailableToNetwork:
		logger.Trace(npdu.NetworkMessageType, " is not supported")
	case InitializeRoutingTable:
		return ne.handleInitializeRoutingTable(source, sadr, npdu)
	case InitializeRoutingTableAck:
		logger.Trace(npdu.NetworkMessageType, " is not supported")
	case EstablishConnectionToNetwork:
		logger.Trace(npdu.NetworkMessageType, " is not supported")
	case DisconnectConnectionToNetwork:
		logger.Trace(npdu.NetworkMessageType, " is not supported")
	case ChallengeRequest:
		logger.Trace(npdu.NetworkMessageType, " is not supported")
	case SecurityPayload:
		logger.Trace(npdu.NetworkMessageType, " is not supported")
	case SecurityResponse:
		logger.Trace(npdu.NetworkMessageType, " is not supported")
	case RequestKeyUpdate:
		logger.Trace(npdu.NetworkMessageType, " is not supported")
	case UpdateKeySet:
		logger.Trace(npdu.NetworkMessageType, " is not supported")
	case UpdateDistributionKey:
		logger.Trace(npdu.NetworkMessageType, " is not supported")
	case RequestMasterKey:
		logger.Trace(npdu.NetworkMessageType, " is not supported")
	case SetMasterKey:
		logger.Trace(npdu.NetworkMessageType, " is not supported")
	case WhatIsNetworkNumber:
		logger.Trace(npdu.NetworkMessageType, " is not supported")
	case NetworkNumberIs:
		logger.Trace(npdu.NetworkMessageType, " is not supported")
	default:
		logger.Trace(npdu.NetworkMessageType, " is not supported")
	}
	return nil
}

func (ne *RouterNetworkEntity) handleWhoIsRouterToNetwork(source *Port, sadr MAC, npdu *NPDU) error {
	logger.Trace("handling Who-Is-Router-To-Network")
	if len(npdu.data) == 2 {
		// a DNET was passed
		dnet := NetworkNumber(((uint16(npdu.data[0]) << 8) & 0xff00) | (uint16(npdu.data[1]) & 0xff))
		logger.Trace("got query for DNET ", dnet)
		logger.Trace("checking directly connected networks")
		for _, p := range ne.ports {
			if p.Dnet == dnet {
				logger.Trace("DNET ", dnet, " is directly connected. Replying to source")
				return ne.sendIAmRouterToNetwork(source, dnet)
			}
		}
		logger.Trace("checking routing table")
		_, ok := ne.routingTable[dnet]
		if ok {
			logger.Trace("DNET ", dnet, " found in routing table. Replying to source")
			// TODO: check for availability of the link
			// send an I-Am-Router-To-Network using broadcast MAC on the source
			// Port
			return ne.sendIAmRouterToNetwork(source, dnet)
		} else {
			logger.Trace("DNET not in routing table. Sending a Who-Is-Router-To-Network")
			// generate a Who-Is-Router-To-Network with DNET
			// if SNET and SADR are not present, add them to the NPDU
			// send it to all the ports except source
			newNpdu := NewNPDU().SetNetworkMessageType(WhoIsRouterToNetwork)
			if !npdu.IsSourcePresent() {
				newNpdu.Source.Net = uint16(source.Dnet)
				newNpdu.Source.Adr = sadr.GetBytes()
			}
			data := &bytes.Buffer{}
			_ = binary.Write(data, binary.BigEndian, dnet)
			npdu.data = data.Bytes()
			var err error
			for _, p := range ne.ports {
				if p.Dnet != source.Dnet {
					err = p.ToDataLink(npdu, nil)
					if err != nil {
						// log error
						logger.Error("transmitting Who-Is-Router-To-Network on port ", p.Id, " failed")
					}
				}
			}
			if err != nil {
				return fmt.Errorf("an error occured when broadcasting Who-Is-Router-To-Network %d", dnet)
			}
			return nil
		}
	} else if len(npdu.data) == 0 {
		logger.Trace(("handle Who-Is-Router-To-Network: empty payload. Building full list"))
		// build the list of all the networks not reachable via source
		// and send an I-Am-Router-To-Network message using broadcast MAC
		// on the source Port
		dnets := make([]NetworkNumber, 0)
		for dnet, entry := range ne.routingTable {
			if entry.Port.Dnet != source.Dnet {
				dnets = append(dnets, dnet)
			}
		}
		// Also add the directly connected networks
		for _, p := range ne.ports {
			if p.Dnet != source.Dnet {
				dnets = append(dnets, p.Dnet)
			}
		}
		return ne.sendIAmRouterToNetwork(source, dnets...)
	} else {
		logger.Error("Handle Who-Is-Router-To-Network: payload len unexpeced: ", len(npdu.data))
	}
	return nil
}

func (ne *RouterNetworkEntity) handleIAmRouterToNetwork(source *Port, sadr MAC, npdu *NPDU) error {
	logger.Trace("hanlde I-Am-Router-To-Network")
	if len(npdu.data)%2 != 0 {
		return fmt.Errorf("malformed I-Am-Router-To-Network payload")
	}
	data := bytes.NewBuffer(npdu.data)
	dnets := make([]NetworkNumber, 0)
	for {
		var net NetworkNumber
		err := binary.Read(data, binary.BigEndian, net)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("error while parsing I-Am-Router-To-Network payload: %w", err)
		}
		ne.updateRoutingTable(net, sadr, source)
		dnets = append(dnets, net)
	}
	var err error
	for _, p := range ne.ports {
		if p.Dnet != source.Dnet {
			err = ne.sendIAmRouterToNetwork(p, dnets...)
			if err != nil {
				// log error
			}
		}
	}
	if err != nil {
		return fmt.Errorf("could not relay I-Am-Router-To-Network on some ports")
	}
	return nil
}

func (ne *RouterNetworkEntity) handleInitializeRoutingTable(source *Port, sadr MAC, npdu *NPDU) error {
	data := bytes.NewBuffer(npdu.data)
	numberOfPorts, err := data.ReadByte()
	if err != nil {
		return fmt.Errorf("could not read number of ports in Initialize-Routing-Table: %w", err)
	}
	if numberOfPorts == 0 {
		// send Ack with full routing table
		ackNpdu := NewNPDU().SetNetworkMessageType(InitializeRoutingTableAck)
		ackNpdu.data = ne.prepareInitializeRoutingTableContent()
		return source.ToDataLink(ackNpdu, sadr.GetBytes())
	}
	for i := 0; i < int(numberOfPorts); i++ {
		var dnet NetworkNumber
		err := binary.Read(data, binary.BigEndian, dnet)
		if err != nil {
			return fmt.Errorf("malformed Initialize-Routing-Table payload: %w", err)
		}
		portId, err := data.ReadByte()
		if err != nil {
			return fmt.Errorf("malformed Initialize-Routing-Table payload: %w", err)
		}
		portInfoLen, err := data.ReadByte()
		if err != nil {
			return fmt.Errorf("malformed Initialize-Routing-Table payload: %w", err)
		}
		var portInfo []byte
		if portInfoLen != 0 {
			portInfo = make([]byte, portInfoLen)
			n, err := data.Read(portInfo)
			if err != nil {
				return fmt.Errorf("malformed Initialize-Routing-Table payload: %w", err)
			}
			if n < int(portInfoLen) {
				return fmt.Errorf("short read of portInfo for entry %d in Initialize-Routing-Table payload", i)
			}
		}
		if portId == 0 {
			delete(ne.routingTable, dnet)
		} else {
			newPort := ne.getPort(int(portId))
			if newPort == nil {
				// TODO: log something
				continue
			}
			newPort.SetPortInfo(portInfo)
			ne.updateRoutingTable(dnet, nil, newPort)
		}
	}
	ackNpdu := NewNPDU().SetNetworkMessageType(InitializeRoutingTableAck)
	return source.ToDataLink(ackNpdu, sadr.GetBytes())
}

func (ne *RouterNetworkEntity) prepareInitializeRoutingTableContent() []byte {
	result := &bytes.Buffer{}
	numberOfPorts := len(ne.routingTable)
	if numberOfPorts == 0 {
		return nil
	}
	err := result.WriteByte(uint8(numberOfPorts))
	if err != nil {
		// TODO log message
		return nil
	}
	for dnet, entry := range ne.routingTable {
		err = binary.Write(result, binary.BigEndian, dnet)
		if err != nil {
			// TODO log message
			return nil
		}
		err = result.WriteByte(uint8(entry.Port.Id))
		if err != nil {
			// TODO log message
			return nil
		}
		portInfo := entry.Port.PortInfo()
		err = result.WriteByte(uint8(len(portInfo)))
		if err != nil {
			// TODO log message
			return nil
		}
		_, err = result.Write(portInfo)
		if err != nil {
			// TODO log message
			return nil
		}
	}
	return result.Bytes()
}

func (ne *RouterNetworkEntity) getPort(id int) *Port {
	for _, p := range ne.ports {
		if p.Id == id {
			return p
		}
	}
	return nil
}

func (ne *RouterNetworkEntity) updateRoutingTable(net NetworkNumber, nextHop MAC, port *Port) {
	logger.Trace("updating routing table with entry [",
		net,
		nextHop,
		port.Id,
		"]",
	)
	entry, ok := ne.routingTable[net]
	if !ok {
		entry = RoutingTableEntry{
			NextHop: nextHop,
			Port:    port,
		}
	} else {
		entry.NextHop = nextHop
		entry.Port = port
	}
	ne.routingTable[net] = entry
}

func (ne *RouterNetworkEntity) sendIAmRouterToNetwork(dest *Port, dnets ...NetworkNumber) error {
	logger.Trace("sending I-Am-Router-To-Network with dnets: ", dnets)
	npdu := NewNPDU().SetNetworkMessageType(IAmRouterToNetwork)
	data := &bytes.Buffer{}
	for _, dnet := range dnets {
		_ = binary.Write(data, binary.BigEndian, dnet)
	}
	npdu.data = data.Bytes()
	return dest.ToDataLink(npdu, nil)
}

type Version byte

const Version1 Version = 1

//go:generate stringer -type=NetworkMessageType
type NetworkMessageType uint8

const (
	WhoIsRouterToNetwork          NetworkMessageType = 0x00
	IAmRouterToNetwork            NetworkMessageType = 0x01
	ICouldBeRouterToNetwork       NetworkMessageType = 0x02
	RejectMessageToNetwork        NetworkMessageType = 0x03
	RouterBusyToNewtork           NetworkMessageType = 0x04
	RouterAvailableToNetwork      NetworkMessageType = 0x05
	InitializeRoutingTable        NetworkMessageType = 0x06
	InitializeRoutingTableAck     NetworkMessageType = 0x07
	EstablishConnectionToNetwork  NetworkMessageType = 0x08
	DisconnectConnectionToNetwork NetworkMessageType = 0x09
	ChallengeRequest              NetworkMessageType = 0x0A
	SecurityPayload               NetworkMessageType = 0x0B
	SecurityResponse              NetworkMessageType = 0x0C
	RequestKeyUpdate              NetworkMessageType = 0x0D
	UpdateKeySet                  NetworkMessageType = 0x0E
	UpdateDistributionKey         NetworkMessageType = 0x0F
	RequestMasterKey              NetworkMessageType = 0x10
	SetMasterKey                  NetworkMessageType = 0x11
	WhatIsNetworkNumber           NetworkMessageType = 0x12
	NetworkNumberIs               NetworkMessageType = 0x13
)

//go:generate stringer -type=NPDUPriority
type NPDUPriority uint8

const (
	LifeSafety        NPDUPriority = 3
	CriticalEquipment NPDUPriority = 2
	Urgent            NPDUPriority = 1
	Normal            NPDUPriority = 0
)

type NPDU struct {
	Version Version //Always one
	Control uint8

	Destination *bacnet.Address
	Source      *bacnet.Address
	HopCount    byte
	//The two are only significant if IsNetworkLayerMessage is true
	NetworkMessageType NetworkMessageType
	VendorID           uint16

	data []byte
	ADPU *APDU
}

func NewNPDU() *NPDU {
	return &NPDU{
		Version:  Version1,
		HopCount: 255,
	}
}

func (npdu *NPDU) String() string {
	result := fmt.Sprintf(
		"Version: %d | CTRL: %08b (NL: %v, Dst: %v, Src: %v, DER: %v, Pri: %d)",
		npdu.Version,
		npdu.Control,
		npdu.IsNetworkMessage(),
		npdu.IsDestPresent(),
		npdu.IsSourcePresent(),
		npdu.IsExpectingReply(),
		npdu.GetPriority(),
	)
	if npdu.IsDestPresent() {
		result = fmt.Sprintf("%s | DNET: %d, DLEN: %d",
			result,
			npdu.Destination.Net,
			len(npdu.Destination.Adr),
		)
	}
	if npdu.IsSourcePresent() {
		result = fmt.Sprintf("%s | SNET: %d, SLEN: %d",
			result,
			npdu.Source.Net,
			len(npdu.Source.Adr),
		)
	}
	if npdu.IsDestPresent() {
		result = fmt.Sprintf("%s | HopCount: %d",
			result,
			npdu.HopCount,
		)

	}
	if npdu.IsNetworkMessage() {
		result = fmt.Sprintf("%s | Type: %v",
			result,
			npdu.NetworkMessageType,
		)
		if npdu.NetworkMessageType >= 0x80 && npdu.NetworkMessageType <= 0xff {
			result = fmt.Sprintf("%s | VendorID: %v",
				result,
				npdu.VendorID,
			)
		}
	}
	return result
}

func (npdu *NPDU) MarshalBinary() ([]byte, error) {
	b := &bytes.Buffer{}
	b.WriteByte(byte(npdu.Version))
	b.WriteByte(npdu.Control)
	if npdu.IsDestPresent() {
		_ = binary.Write(b, binary.BigEndian, npdu.Destination.Net)
		_ = binary.Write(b, binary.BigEndian, byte(len(npdu.Destination.Adr)))
		_ = binary.Write(b, binary.BigEndian, npdu.Destination.Adr)
	}
	if npdu.IsSourcePresent() {
		_ = binary.Write(b, binary.BigEndian, npdu.Source.Net)
		_ = binary.Write(b, binary.BigEndian, byte(len(npdu.Source.Adr)))
		_ = binary.Write(b, binary.BigEndian, npdu.Source.Adr)
	}
	if npdu.IsDestPresent() {
		b.WriteByte(npdu.HopCount)
	}
	if npdu.IsNetworkMessage() {
		b.WriteByte(byte(npdu.NetworkMessageType))
		if npdu.NetworkMessageType >= 0x80 {
			_ = binary.Write(b, binary.BigEndian, npdu.VendorID)
		}
	}
	bytes := b.Bytes()
	if npdu.ADPU != nil {
		bytesapdu, err := npdu.ADPU.MarshalBinary()
		if err != nil {
			return nil, err
		}
		bytes = append(bytes, bytesapdu...)
	} else {
		bytes = append(bytes, npdu.data...)
	}
	return bytes, nil
}

const (
	// bits 6 and 4 are reserved for future use
	CtrlNLMask       = uint8(0b10000000)
	CtrlDestMask     = uint8(0b00100000)
	CtrlSrcMask      = uint8(0b00001000)
	CtrlDERMask      = uint8(0b00000100)
	CtrlPriorityMask = uint8(0b00000011)
)

func (npdu *NPDU) IsNetworkMessage() bool {
	return (npdu.Control & CtrlNLMask) > 0
}

func (npdu *NPDU) SetIsNetworkMessage(v bool) *NPDU {
	if v {
		npdu.Control |= CtrlNLMask
	} else {
		npdu.Control &= ^CtrlNLMask
	}
	return npdu
}

func (npdu *NPDU) SetNetworkMessageType(t NetworkMessageType) *NPDU {
	npdu.SetIsNetworkMessage(true)
	npdu.NetworkMessageType = t
	return npdu
}

func (npdu *NPDU) IsDestPresent() bool {
	return (npdu.Control & CtrlDestMask) > 0
}

func (npdu *NPDU) SetIsDestPresent(v bool) *NPDU {
	if v {
		npdu.Control |= CtrlDestMask
	} else {
		npdu.Control &= ^CtrlDestMask
	}
	return npdu
}

func (npdu *NPDU) IsSourcePresent() bool {
	return (npdu.Control & CtrlSrcMask) > 0
}

func (npdu *NPDU) SetIsSourcePresent(v bool) *NPDU {
	if v {
		npdu.Control |= CtrlSrcMask
	} else {
		npdu.Control &= ^CtrlSrcMask
	}
	return npdu
}

func (npdu *NPDU) IsExpectingReply() bool {
	return (npdu.Control & CtrlDERMask) > 0
}

func (npdu *NPDU) SetIsExpectingReply(v bool) *NPDU {
	if v {
		npdu.Control |= CtrlDERMask
	} else {
		npdu.Control &= ^CtrlDERMask
	}
	return npdu
}

func (npdu *NPDU) GetPriority() NPDUPriority {
	return NPDUPriority(npdu.Control & CtrlPriorityMask)
}

func (npdu *NPDU) SetPriority(p NPDUPriority) *NPDU {
	npdu.Control &= ^CtrlPriorityMask
	npdu.Control |= uint8(p) & CtrlPriorityMask
	return npdu
}

func (npdu *NPDU) UnmarshalBinary(data []byte) error {
	buf := bytes.NewBuffer(data)
	err := binary.Read(buf, binary.BigEndian, &npdu.Version)
	if err != nil {
		return fmt.Errorf("read NPDU version: %w", err)
	}
	if npdu.Version != Version1 {
		return fmt.Errorf("invalid NPDU version %d", npdu.Version)
	}
	control, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("read NPDU control byte:  %w", err)
	}
	npdu.Control = control

	if npdu.IsDestPresent() {
		npdu.Destination = &bacnet.Address{}
		err := binary.Read(buf, binary.BigEndian, &npdu.Destination.Net)
		if err != nil {
			return fmt.Errorf("read NPDU dest Address.Net: %w", err)
		}
		var length byte
		err = binary.Read(buf, binary.BigEndian, &length)
		if err != nil {
			return fmt.Errorf("read NPDU dest Address.Len: %w", err)
		}
		npdu.Destination.Adr = make([]byte, int(length))
		err = binary.Read(buf, binary.BigEndian, &npdu.Destination.Adr)
		if err != nil {
			return fmt.Errorf("read NPDU dest Address.Net: %w", err)
		}
	}

	if npdu.IsSourcePresent() {
		npdu.Source = &bacnet.Address{}
		err := binary.Read(buf, binary.BigEndian, &npdu.Source.Net)
		if err != nil {
			return fmt.Errorf("read NPDU src Address.Net: %w", err)
		}
		var length byte
		err = binary.Read(buf, binary.BigEndian, &length)
		if err != nil {
			return fmt.Errorf("read NPDU src Address.Len: %w", err)
		}
		npdu.Source.Adr = make([]byte, int(length))
		err = binary.Read(buf, binary.BigEndian, &npdu.Source.Adr)
		if err != nil {
			return fmt.Errorf("read NPDU src Address.Net: %w", err)
		}
	}

	if npdu.IsDestPresent() {
		err := binary.Read(buf, binary.BigEndian, &npdu.HopCount)
		if err != nil {
			return fmt.Errorf("read NPDU HopCount: %w", err)
		}
	}

	if npdu.IsNetworkMessage() {
		err := binary.Read(buf, binary.BigEndian, &npdu.NetworkMessageType)
		if err != nil {
			return fmt.Errorf("read NPDU NetworkMessageType: %w", err)
		}
		if npdu.NetworkMessageType > 0x80 {
			err := binary.Read(buf, binary.BigEndian, &npdu.VendorID)
			if err != nil {
				return fmt.Errorf("read NPDU VendorId: %w", err)
			}
		}
	}
	npdu.data = buf.Bytes()
	return nil
}
