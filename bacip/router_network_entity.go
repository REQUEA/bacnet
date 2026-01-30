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

type RouterNetworkEntity struct {
	CommonNetworkEntity
	ports       []*Port
	scheduler   Scheduler[*NPDU]
	routingCond sync.Cond
}

func NewRouterNetworkEntity() *RouterNetworkEntity {
	result := RouterNetworkEntity{
		CommonNetworkEntity: CommonNetworkEntity{
			routingTable: make(map[bacnet.NetworkNumber]RoutingTableEntry),
		},
		ports:     make([]*Port, 0),
		scheduler: NewWRRScheduler[*NPDU](1, 2, 4, 8),
		routingCond: sync.Cond{
			L: &sync.Mutex{},
		},
	}
	return &result
}

func (e *RouterNetworkEntity) NUnitDataRequest(
	dadr *bacnet.BACnetAddress,
	der bool,
	priority NPDUPriority,
	payload []byte,
) error {
	switch dadr.Network {
	case bacnet.LocalDNET:
		// local transmission
		if len(dadr.Mac.GetBytes()) == 0 {
			// local broadcast
		} else {
			// transmission to a specific host on the network
		}
	case bacnet.BroadcastDNET:
		// global broadcast
		dadr.Mac = nil // make sure the adr field is emtpy
		npdu := NewNPDU().
			SetDest(dadr).
			SetIsExpectingReply(der).
			SetPriority(priority)
		err := e.schedule(int(priority), npdu)
		if err != nil {
			return fmt.Errorf("scheduling global broadcast npdu failed: %w", err)
		}
	default:
		// remote transmission
		npdu := NewNPDU()
		npdu.SetDest(dadr).
			SetIsExpectingReply(der).
			SetPriority(priority)
		// Schedule the NPDU for routing
		err := e.schedule(int(priority), npdu)
		if err != nil {
			return fmt.Errorf("scheduling npdu for network %d failed: %w", dadr.Network, err)
		}
	}
	return nil
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
		if npdu.Destination.Network == bacnet.BroadcastDNET {
			logger.Trace("npdu for broadcast DNET")
			// TODO: need to add SNET to the npdu header before scheduling
			npdu.HopCount--
			if npdu.HopCount > 0 {
				// broadcast on all ports except source
				logger.Trace("broadcasting on all ports except port ", npdu.Source.Network)
				for _, p := range ne.ports {
					if p.Dnet != bacnet.NetworkNumber(npdu.Source.Network) {
						logger.Trace("passing npdu to port ", p.Dnet)
						err := p.ToDataLink(npdu, nil)
						if err != nil {
							logger.Error("error sending to datalink: ", err)
						}
					}
				}
			}
		} else {
			logger.Trace("npdu for network ", npdu.Destination.Network)
			dnet := bacnet.NetworkNumber(npdu.Destination.Network)
			for _, p := range ne.ports {
				if p.Dnet == dnet {
					logger.Trace("routing to directly connected network ", npdu.Destination.Network)
					dstMac := npdu.Destination.Mac
					npdu.SetIsDestPresent(false)
					err := p.ToDataLink(npdu, dstMac.GetBytes())
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
				logger.Trace("no route for DNET ", npdu.Destination.Network)
				newNpdu := NewWhoIsRouterToNetworkNPDU(dnet)
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

func (ne *RouterNetworkEntity) NUnitDataIndication(sport *Port, dadr bacnet.MAC, sadr bacnet.MAC, buf []byte) error {
	if buf[0] != uint8(Version1) {
		return fmt.Errorf("incorrect bacnet version: %d", buf[0])
	}
	npdu := &NPDU{}
	err := npdu.UnmarshalBinary(buf)
	if err != nil {
		return err
	}
	logger.Trace("Handle(", sport.Id, ", ", npdu, ")")
	if npdu.IsNetworkMessage() {
		if !npdu.IsDestPresent() {
			logger.Trace("Network message for local network entity")
			return ne.handleNetworkLayerMessage(sport, sadr, npdu)
		} else if npdu.Destination.Network == bacnet.BroadcastDNET { // DNET present and broadcast
			err := ne.handleNetworkLayerMessage(sport, sadr, npdu)
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
				logger.Error("error scheduling message for dnet ", npdu.Destination.Network, ": ", err)
				return err
			}
			return nil
		}
	} else {
		if !npdu.IsDestPresent() {
			logger.Trace("Handle: npdu for local application layer")
			// find bacnet application entity
			// if found pass payload to the application entity
		} else if npdu.Destination.Network == bacnet.BroadcastDNET { // DNET present and broadcast
			logger.Trace("Handle: npdu for broadcast DNET")
			// find bacnet application entity
			// if found pass payload to the application entity
			source := &bacnet.BACnetAddress{
				Network: sport.Dnet,
				Mac:     sadr,
			}
			npdu.SetSource(source)
			err := ne.schedule(int(npdu.GetPriority()), npdu)
			if err != nil {
				logger.Error("error scheduling global broadcast application message")
				return err
			}
		} else {
			logger.Trace("Handle: npdu for DNET ", npdu.Destination.Network)
			source := &bacnet.BACnetAddress{
				Network: sport.Dnet,
				Mac:     sadr,
			}
			npdu.SetSource(source)
			err := ne.schedule(int(npdu.GetPriority()), npdu)
			if err != nil {
				logger.Error("error shceduling application message for network ", npdu.Destination.Network, ": ", err)
				return err
			}
		}
	}
	return nil
}

func (ne *RouterNetworkEntity) handleNetworkLayerMessage(source *Port, sadr bacnet.MAC, npdu *NPDU) error {
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

func (ne *RouterNetworkEntity) handleWhoIsRouterToNetwork(source *Port, sadr bacnet.MAC, npdu *NPDU) error {
	logger.Trace("handling Who-Is-Router-To-Network")
	if len(npdu.data) == 2 {
		// a DNET was passed
		dnet := bacnet.NetworkNumber(((uint16(npdu.data[0]) << 8) & 0xff00) | (uint16(npdu.data[1]) & 0xff))
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
				newNpdu.Source.Network = source.Dnet
				newNpdu.Source.Mac = sadr
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
		dnets := make([]bacnet.NetworkNumber, 0)
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

func (ne *RouterNetworkEntity) handleIAmRouterToNetwork(source *Port, sadr bacnet.MAC, npdu *NPDU) error {
	logger.Trace("hanlde I-Am-Router-To-Network")
	if len(npdu.data)%2 != 0 {
		return fmt.Errorf("malformed I-Am-Router-To-Network payload")
	}
	data := bytes.NewBuffer(npdu.data)
	dnets := make([]bacnet.NetworkNumber, 0)
	for {
		var net bacnet.NetworkNumber
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

func (ne *RouterNetworkEntity) handleInitializeRoutingTable(source *Port, sadr bacnet.MAC, npdu *NPDU) error {
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
		var dnet bacnet.NetworkNumber
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

func (ne *RouterNetworkEntity) sendIAmRouterToNetwork(dest *Port, dnets ...bacnet.NetworkNumber) error {
	logger.Trace("sending I-Am-Router-To-Network with dnets: ", dnets)
	npdu := NewNPDU().SetNetworkMessageType(IAmRouterToNetwork)
	data := &bytes.Buffer{}
	for _, dnet := range dnets {
		_ = binary.Write(data, binary.BigEndian, dnet)
	}
	npdu.data = data.Bytes()
	return dest.ToDataLink(npdu, nil)
}
