package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

type BacnetVersion byte

const BacnetVersion1 BacnetVersion = 1

type NPDUPriority byte

const (
	LifeSafety        NPDUPriority = 3
	CriticalEquipment NPDUPriority = 2
	Urgent            NPDUPriority = 1
	Normal            NPDUPriority = 0
)

type Address struct {
	// mac_len = 0 is a broadcast address
	MacLen byte
	// note: MAC for IP addresses uses 4 bytes for addr, 2 bytes for port
	// use de/encode_unsigned32/16 for re/storing the IP address
	Mac []byte
	// DNET,DLEN,DADR or SNET,SLEN,SADR
	// the following are used if the device is behind a router
	// net = 0 indicates local
	Net uint16 // BACnet network number
	// LEN = 0 denotes broadcast MAC ADR and ADR field is absent
	// LEN > 0 specifies length of ADR field
	Len byte   // length of MAC address
	Adr []byte // hwaddr (MAC) address
}

type NPDU struct {
	Version BacnetVersion //Always one
	// This 3 fields are packed in the control byte
	IsNetworkLayerMessage bool //If true, there is no APDU
	ExpectingReply        bool
	Priority              NPDUPriority

	Destination *Address
	Source      *Address
	HopCount    byte
	//The two are only significant if IsNetworkLayerMessage is true
	NetworkMessageType byte
	VendorID           uint16

	ADPU *APDU
}

func (npdu NPDU) MarshalBinary() ([]byte, error) {
	b := &bytes.Buffer{}
	b.WriteByte(byte(npdu.Version))
	var control byte
	var hasSrc, hasDest, isNetworkMessage bool
	if npdu.IsNetworkLayerMessage {
		control += 1 << 7
		isNetworkMessage = true
	}
	if npdu.ExpectingReply {
		control += 1 << 2
	}
	if npdu.Priority > 3 {
		return nil, fmt.Errorf("Invalid Priority %d", npdu.Priority)
	}
	control += byte(npdu.Priority)
	if npdu.Source != nil && npdu.Source.Net != 0 {
		control += 1 << 3
		hasSrc = true
	}
	if npdu.Destination != nil && npdu.Destination.Net != 0 {
		control += 1 << 5
		hasDest = true
	}
	b.WriteByte(control)
	if hasDest {
		binary.Write(b, binary.BigEndian, npdu.Destination.Net)
		binary.Write(b, binary.BigEndian, npdu.Destination.Len)
		binary.Write(b, binary.BigEndian, npdu.Destination.Adr)
	}
	if hasSrc {
		binary.Write(b, binary.BigEndian, npdu.Source.Net)
		binary.Write(b, binary.BigEndian, npdu.Source.Len)
		binary.Write(b, binary.BigEndian, npdu.Source.Adr)
	}
	if hasDest {
		b.WriteByte(npdu.HopCount)
	}
	if isNetworkMessage {
		b.WriteByte(npdu.NetworkMessageType)
		if npdu.NetworkMessageType >= 0x80 {
			binary.Write(b, binary.BigEndian, npdu.VendorID)
		}
	}
	bytes := b.Bytes()
	if npdu.ADPU != nil {
		bytesapdu, err := npdu.ADPU.MarshalBinary()
		if err != nil {
			return nil, err
		}
		bytes = append(bytes, bytesapdu...)
	}
	return bytes, nil
}

type PDUType byte

//TODO: Maybe do from 0 to 7
const (
	ConfirmedServiceRequest   PDUType = 0
	UnconfirmedServiceRequest PDUType = 0x10
	ComplexAck                PDUType = 0x30
	SegmentAck                PDUType = 0x40
	Error                     PDUType = 0x50
	Reject                    PDUType = 0x60
	Abort                     PDUType = 0x70
)

const (
	ServiceUnconfirmedIAm               ServiceType = 0
	ServiceUnconfirmedIHave             ServiceType = 1
	ServiceUnconfirmedCOVNotification   ServiceType = 2
	ServiceUnconfirmedEventNotification ServiceType = 3
	ServiceUnconfirmedPrivateTransfer   ServiceType = 4
	ServiceUnconfirmedTextMessage       ServiceType = 5
	ServiceUnconfirmedTimeSync          ServiceType = 6
	ServiceUnconfirmedWhoHas            ServiceType = 7
	ServiceUnconfirmedWhoIs             ServiceType = 8
	ServiceUnconfirmedUTCTimeSync       ServiceType = 9
	ServiceUnconfirmedWriteGroup        ServiceType = 10
	/* Other services to be added as they are defined. */
	/* All choice values in this production are reserved */
	/* for definition by ASHRAE. */
	/* Proprietary extensions are made by using the */
	/* UnconfirmedPrivateTransfer service. See Clause 23. */
	MaxServiceUnconfirmed ServiceType = 11
)

type ServiceType byte

const (
	/* Alarm and Event Services */
	ServiceConfirmedAcknowledgeAlarm     ServiceType = 0
	ServiceConfirmedCOVNotification      ServiceType = 1
	ServiceConfirmedEventNotification    ServiceType = 2
	ServiceConfirmedGetAlarmSummary      ServiceType = 3
	ServiceConfirmedGetEnrollmentSummary ServiceType = 4
	ServiceConfirmedGetEventInformation  ServiceType = 29
	ServiceConfirmedSubscribeCOV         ServiceType = 5
	ServiceConfirmedSubscribeCOVProperty ServiceType = 28
	ServiceConfirmedLifeSafetyOperation  ServiceType = 27
	/* File Access Services */
	ServiceConfirmedAtomicReadFile  ServiceType = 6
	ServiceConfirmedAtomicWriteFile ServiceType = 7
	/* Object Access Services */
	ServiceConfirmedAddListElement      ServiceType = 8
	ServiceConfirmedRemoveListElement   ServiceType = 9
	ServiceConfirmedCreateObject        ServiceType = 10
	ServiceConfirmedDeleteObject        ServiceType = 11
	ServiceConfirmedReadProperty        ServiceType = 12
	ServiceConfirmedReadPropConditional ServiceType = 13
	ServiceConfirmedReadPropMultiple    ServiceType = 14
	ServiceConfirmedReadRange           ServiceType = 26
	ServiceConfirmedWriteProperty       ServiceType = 15
	ServiceConfirmedWritePropMultiple   ServiceType = 16
	/* Remote Device Management Services */
	ServiceConfirmedDeviceCommunicationControl ServiceType = 17
	ServiceConfirmedPrivateTransfer            ServiceType = 18
	ServiceConfirmedTextMessage                ServiceType = 19
	ServiceConfirmedReinitializeDevice         ServiceType = 20
	/* Virtual Terminal Services */
	ServiceConfirmedVTOpen  ServiceType = 21
	ServiceConfirmedVTClose ServiceType = 22
	ServiceConfirmedVTData  ServiceType = 23
	/* Security Services */
	ServiceConfirmedAuthenticate ServiceType = 24
	ServiceConfirmedRequestKey   ServiceType = 25
	/* Services added after 1995 */
	/* readRange (26) see Object Access Services */
	/* lifeSafetyOperation (27) see Alarm and Event Services */
	/* subscribeCOVProperty (28) see Alarm and Event Services */
	/* getEventInformation (29) see Alarm and Event Services */
	maxBACnetConfirmedService ServiceType = 30
)

func (apdu APDU) MarshalBinary() ([]byte, error) {
	b := &bytes.Buffer{}
	b.WriteByte(byte(apdu.DataType))
	b.WriteByte(byte(apdu.ServiceType))
	bytes, err := apdu.Payload.MarshalBinary()
	if err != nil {
		return nil, err
	}
	b.Write(bytes)
	return b.Bytes(), nil
}

type Payload interface {
	MarshalBinary() ([]byte, error)
	UnmarshalBinary([]byte) error
}
type APDU struct {
	DataType    PDUType
	ServiceType ServiceType
	Payload     Payload
}

type DataPayload struct {
	Bytes []byte
}

func (p DataPayload) MarshalBinary() ([]byte, error) {
	return p.Bytes, nil
}

func (p *DataPayload) UnmarshalBinary(data []byte) error {
	p.Bytes = make([]byte, len(data))
	copy(p.Bytes, data)
	return nil
}
