package networklayer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/logger"
)

type NonRouterNetworkEntity struct {
	CommonNetworkEntity
	port *Port
}

func NewNonRouterNetworkEntity(p *Port) *NonRouterNetworkEntity {
	return &NonRouterNetworkEntity{
		CommonNetworkEntity: CommonNetworkEntity{
			routingTable: make(map[bacnet.NetworkNumber]RoutingTableEntry),
		},
		port: p,
	}
}

func (ne *NonRouterNetworkEntity) NUnitDataIndication(sport *Port, dadr bacnet.MAC, sadr bacnet.MAC, buf []byte) error {
	if buf[0] != uint8(Version1) {
		return fmt.Errorf("incorrect bacnet version: %d", buf[0])
	}
	npdu := &NPDU{}
	err := npdu.UnmarshalBinary(buf)
	if err != nil {
		return err
	}
	logger.Trace("Handle(", sport.Id, ", ", npdu, ")")
	if !npdu.IsDestPresent() || npdu.Destination.Network == bacnet.BroadcastDNET {
		if npdu.IsNetworkMessage() {
			logger.Trace("Handle: network message payload")
			return ne.handleNetworkLayerMessage(sport, sadr, npdu)
		} else {
			logger.Trace("Handle: application message payload")
			indication := NPDUIndication{Apdu: npdu.data}
			if npdu.IsSourcePresent() {
				indication.Source = npdu.Source
			} else {
				indication.Source = &bacnet.BACnetAddress{Mac: sadr}
			}
			if npdu.IsDestPresent() {
				indication.Dest = npdu.Destination
			} else {
				indication.Dest = &bacnet.BACnetAddress{Mac: sadr}
			}
			ne.apduHandler.HandleNUnitDataIndication(&indication)
		}
	}
	// In other cases a node that is not a router must discard the message
	return nil
}

func (ne *NonRouterNetworkEntity) handleNetworkLayerMessage(sport *Port, sadr bacnet.MAC, npdu *NPDU) error {
	switch npdu.NetworkMessageType {
	case IAmRouterToNetwork:
		return ne.handleIAmRouterToNetwork(sport, sadr, npdu)
	default:
		logger.Trace(npdu.NetworkMessageType, " is not supported")
	}
	return nil
}

func (ne *NonRouterNetworkEntity) handleIAmRouterToNetwork(sport *Port, sadr bacnet.MAC, npdu *NPDU) error {
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
		ne.updateRoutingTable(net, sadr, sport)
		dnets = append(dnets, net)
	}
	return nil
}

func (ne *NonRouterNetworkEntity) NUnitDataRequest(
	dadr *bacnet.BACnetAddress,
	der bool,
	priority NPDUPriority,
	payload []byte,
) error {
	switch dadr.Network {
	case bacnet.LocalDNET:
		npdu := NewNPDU().SetIsExpectingReply(der).SetPriority(priority)
		npdu.data = payload
		if len(dadr.Mac.GetBytes()) == 0 {
			// local broadcast
			err := ne.port.Broadcast(npdu)
			if err != nil {
				return fmt.Errorf("passing local unicast NPDU to datalink failed: %w", err)
			}
		} else {
			// transmission to a specific host on the network
			err := ne.port.ToDataLink(npdu, dadr.Mac.GetBytes())
			if err != nil {
				return fmt.Errorf("passing local unicast NPDU to datalink failed: %w", err)
			}
		}
	case bacnet.BroadcastDNET:
		dadr.Mac = nil // make sure the adr field is empty
		npdu := NewNPDU().
			SetDest(dadr).
			SetIsExpectingReply(der).
			SetPriority(priority)
		npdu.data = payload
		err := ne.port.Broadcast(npdu)
		if err != nil {
			return fmt.Errorf("passing global broadcast NPDU to datalink failed: %w", err)
		}
	default:
		// transmission to remote network
		entry, ok := ne.routingTable[bacnet.NetworkNumber(dadr.Network)]
		if !ok {
			// TODO: drop or queue the message?
			npdu := NewWhoIsRouterToNetworkNPDU(bacnet.NetworkNumber(dadr.Network))
			err := ne.port.Broadcast(npdu)
			if err != nil {
				return fmt.Errorf("sending Who-Is-Router-To-Network %d failed: %w",
					dadr.Network, err)
			}
		} else {
			npdu := NewNPDU().
				SetDest(dadr).
				SetIsExpectingReply(der).
				SetPriority(priority)
			npdu.data = payload
			err := ne.port.ToDataLink(npdu, entry.NextHop.GetBytes())
			if err != nil {
				return fmt.Errorf("passing remote unicast NPDU to datalink failed: %w", err)
			}
		}
	}
	return nil
}

func (ne *NonRouterNetworkEntity) GetMaxPDULength(_ bacnet.NetworkNumber) uint {
	return ne.port.datalinkPort.MaxPDULength()
}

func (ne *NonRouterNetworkEntity) NReleaseRequest(_ *bacnet.BACnetAddress) error {
	// BACnet/IP is connectionless; nothing to release.
	return nil
}
