package networklayer

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/linklayer"
	"github.com/REQUEA/bacnet/logger"
)

type Port struct {
	Id            int // used for management, cannot be 0
	Dnet          bacnet.NetworkNumber
	portInfo      []byte
	networkEntity NetworkEntity
	datalinkPort  linklayer.DatalinkPort
}

func NewPort(id, dnet int, dlPort linklayer.DatalinkPort) *Port {
	return &Port{
		Id:           id,
		Dnet:         bacnet.NetworkNumber(dnet),
		datalinkPort: dlPort,
	}
}

func (p *Port) SetDatalinkPort(e linklayer.DatalinkPort) {
	p.datalinkPort = e
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

func (p *Port) HandleNPDU(dadr bacnet.MAC, sadr bacnet.MAC, buf []byte) error {
	// TODO: might have to place the incoming message in an input queue for
	// the network entity
	return p.networkEntity.NUnitDataIndication(p, dadr, sadr, buf)
}

func (p *Port) ToDataLink(npdu *NPDU, destMAC []byte) error {
	data, err := npdu.MarshalBinary()
	if err != nil {
		return fmt.Errorf("could not marshal NPDU: %w", err)
	}
	if destMAC != nil {
		return p.datalinkPort.Send(data, destMAC)
	}
	return fmt.Errorf("dest MAC not specified")
}

func (p *Port) Broadcast(npdu *NPDU) error {
	data, err := npdu.MarshalBinary()
	if err != nil {
		return fmt.Errorf("could not marshal NPDU: %w", err)
	}
	return p.datalinkPort.Send(data, p.datalinkPort.BroadcastMac().GetBytes())
}

func (p *Port) Mac() bacnet.MAC {
	return p.datalinkPort.Mac()
}

func (p *Port) BroadcastMac() bacnet.MAC {
	return p.datalinkPort.BroadcastMac()
}

type NPDUIndication struct {
	Source        *bacnet.BACnetAddress
	Dest          *bacnet.BACnetAddress
	Priority      NPDUPriority
	ExpectedReply bool
	Apdu          []byte
}

type APDUHandler interface {
	HandleNUnitDataIndication(*NPDUIndication)
	HandleNUnitReportIndication(*NPDUIndication)
}

type NetworkEntity interface {
	NUnitDataIndication(source *Port, dadr bacnet.MAC, sadr bacnet.MAC, buf []byte) error
	NUnitDataRequest(dadr *bacnet.BACnetAddress, der bool, priority NPDUPriority, payload []byte) error
	NReleaseRequest(dadr *bacnet.BACnetAddress) error
	GetMaxPDULength(dnet bacnet.NetworkNumber) uint
}

type CommonNetworkEntity struct {
	routingTable map[bacnet.NetworkNumber]RoutingTableEntry
	apduHandler  APDUHandler
}

func (e *CommonNetworkEntity) NUnitDataIndication(source *Port, dadr bacnet.MAC, sadr bacnet.MAC, buf []byte) error {
	return nil
}

func (e *CommonNetworkEntity) NUnitDataRequest(dadr *bacnet.BACnetAddress, der bool, priority NPDUPriority, payload []byte) error {
	return nil
}

func (e *CommonNetworkEntity) NReleaseRequest(dadr *bacnet.BACnetAddress) error {
	return nil
}

func (ne *CommonNetworkEntity) updateRoutingTable(net bacnet.NetworkNumber, nextHop bacnet.MAC, port *Port) {
	logger.Trace(
		"updating routing table with entry [", net, ", ", nextHop, ", ", port.Id, "]",
	)
	entry, ok := ne.routingTable[net]
	if !ok {
		entry = RoutingTableEntry{NextHop: nextHop, Port: port}
	} else {
		entry.NextHop = nextHop
		entry.Port = port
	}
	ne.routingTable[net] = entry
}

type RoutingTableEntry struct {
	// NextHop can be nil (after receiving Initialize-Routing-Table"), maybe
	// put this information in the port structure
	NextHop bacnet.MAC
	Port    *Port
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
	UrgentPriority    NPDUPriority = 1
	NormalPriority    NPDUPriority = 0
)

type NPDU struct {
	Version Version //Always one
	Control uint8

	Destination *bacnet.BACnetAddress
	Source      *bacnet.BACnetAddress
	HopCount    byte
	//The two are only significant if IsNetworkLayerMessage is true
	NetworkMessageType NetworkMessageType
	VendorID           uint16

	data []byte
}

func NewNPDU() *NPDU {
	return &NPDU{
		Version:  Version1,
		HopCount: 255,
	}
}

func NewWhoIsRouterToNetworkNPDU(net bacnet.NetworkNumber) *NPDU {
	result := NewNPDU().SetNetworkMessageType(WhoIsRouterToNetwork)
	result.data = []byte{byte(net >> 8), byte(net & 0xff)}
	return result
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
			npdu.Destination.Network,
			len(npdu.Destination.Mac.GetBytes()),
		)
	}
	if npdu.IsSourcePresent() {
		result = fmt.Sprintf("%s | SNET: %d, SLEN: %d",
			result,
			npdu.Source.Network,
			len(npdu.Source.Mac.GetBytes()),
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
		if npdu.NetworkMessageType >= 0x80 {
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
		_ = binary.Write(b, binary.BigEndian, npdu.Destination.Network)
		_ = binary.Write(b, binary.BigEndian, byte(len(npdu.Destination.Mac.GetBytes())))
		_ = binary.Write(b, binary.BigEndian, npdu.Destination.Mac)
	}
	if npdu.IsSourcePresent() {
		_ = binary.Write(b, binary.BigEndian, npdu.Source.Network)
		_ = binary.Write(b, binary.BigEndian, byte(len(npdu.Source.Mac.GetBytes())))
		_ = binary.Write(b, binary.BigEndian, npdu.Source.Mac)
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
	// TODO verify if bytes is needed
	bytes := b.Bytes()
	bytes = append(bytes, npdu.data...)
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

func (npdu *NPDU) SetDest(addr *bacnet.BACnetAddress) *NPDU {
	npdu.Destination = addr
	npdu.SetIsDestPresent(true)
	return npdu
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

func (npdu *NPDU) SetSource(addr *bacnet.BACnetAddress) *NPDU {
	npdu.Source = addr
	npdu.SetIsSourcePresent(true)
	return npdu
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
		npdu.Destination = &bacnet.BACnetAddress{}
		err := binary.Read(buf, binary.BigEndian, &npdu.Destination.Network)
		if err != nil {
			return fmt.Errorf("read NPDU dest Address.Net: %w", err)
		}
		var length byte
		err = binary.Read(buf, binary.BigEndian, &length)
		if err != nil {
			return fmt.Errorf("read NPDU dest Address.Len: %w", err)
		}
		destBytes := make([]byte, int(length))
		err = binary.Read(buf, binary.BigEndian, &npdu.Destination.Mac)
		if err != nil {
			return fmt.Errorf("read NPDU dest Address.Net: %w", err)
		}
		npdu.Destination.Mac.FromBytes(destBytes)
	}

	if npdu.IsSourcePresent() {
		npdu.Source = &bacnet.BACnetAddress{}
		err := binary.Read(buf, binary.BigEndian, &npdu.Source.Network)
		if err != nil {
			return fmt.Errorf("read NPDU src Address.Net: %w", err)
		}
		var length byte
		err = binary.Read(buf, binary.BigEndian, &length)
		if err != nil {
			return fmt.Errorf("read NPDU src Address.Len: %w", err)
		}
		sourceBytes := make([]byte, int(length))
		err = binary.Read(buf, binary.BigEndian, &npdu.Source.Mac)
		if err != nil {
			return fmt.Errorf("read NPDU src Address.Net: %w", err)
		}
		npdu.Source.Mac.FromBytes(sourceBytes)
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
