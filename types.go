package bacnet

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
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

func AddressFromUDP(udp *net.UDPAddr) Address {
	b := bytes.NewBuffer(udp.IP)
	port := uint16(udp.Port)
	_ = binary.Write(b, binary.BigEndian, port)
	return Address{
		MacLen: byte(len(b.Bytes())),
		Mac:    b.Bytes(),
	}
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
	if npdu.Destination != nil && npdu.Destination.Net != 0 {
		control += 1 << 5
		hasDest = true
	}
	if npdu.Source != nil && npdu.Source.Net != 0 {
		control += 1 << 3
		hasSrc = true
	}
	b.WriteByte(control)
	if hasDest {
		_ = binary.Write(b, binary.BigEndian, npdu.Destination.Net)
		_ = binary.Write(b, binary.BigEndian, npdu.Destination.Len)
		_ = binary.Write(b, binary.BigEndian, npdu.Destination.Adr)
	}
	if hasSrc {
		_ = binary.Write(b, binary.BigEndian, npdu.Source.Net)
		_ = binary.Write(b, binary.BigEndian, npdu.Source.Len)
		_ = binary.Write(b, binary.BigEndian, npdu.Source.Adr)
	}
	if hasDest {
		b.WriteByte(npdu.HopCount)
	}
	if isNetworkMessage {
		b.WriteByte(npdu.NetworkMessageType)
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
	}
	return bytes, nil
}

func (npdu *NPDU) UnmarshallBinary(data []byte) error {
	buf := bytes.NewBuffer(data)
	err := binary.Read(buf, binary.BigEndian, &npdu.Version)
	if err != nil {
		return fmt.Errorf("Failed to read NPDU version: %w", err)
	}
	if npdu.Version != BacnetVersion1 {
		return fmt.Errorf("Invalid NPDU version %d", npdu.Version)
	}
	control, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("Failed to read NPDU control byte:  %w", err)
	}
	if control&(1<<7) > 0 {
		npdu.IsNetworkLayerMessage = true
	}
	if control&(1<<2) > 0 {
		npdu.ExpectingReply = true
	}
	npdu.Priority = NPDUPriority(control & 0x3)

	if control&(1<<5) > 0 {
		npdu.Destination = &Address{}
		err := binary.Read(buf, binary.BigEndian, &npdu.Destination.Net)
		if err != nil {
			return fmt.Errorf("Failed to read NPDU dest Address.Net: %w", err)
		}
		err = binary.Read(buf, binary.BigEndian, &npdu.Destination.Len)
		if err != nil {
			return fmt.Errorf("Failed to read NPDU dest Address.Len: %w", err)
		}
		npdu.Destination.Adr = make([]byte, int(npdu.Destination.Len))
		err = binary.Read(buf, binary.BigEndian, &npdu.Destination.Adr)
		if err != nil {
			return fmt.Errorf("Failed to read NPDU dest Address.Net: %w", err)
		}
	}

	if control&(1<<3) > 0 {
		npdu.Source = &Address{}
		err := binary.Read(buf, binary.BigEndian, &npdu.Source.Net)
		if err != nil {
			return fmt.Errorf("Failed to read NPDU src Address.Net: %w", err)
		}
		err = binary.Read(buf, binary.BigEndian, &npdu.Source.Len)
		if err != nil {
			return fmt.Errorf("Failed to read NPDU src Address.Len: %w", err)
		}
		npdu.Source.Adr = make([]byte, int(npdu.Source.Len))
		err = binary.Read(buf, binary.BigEndian, &npdu.Source.Adr)
		if err != nil {
			return fmt.Errorf("Failed to read NPDU src Address.Net: %w", err)
		}
	}

	if npdu.Destination != nil {
		err := binary.Read(buf, binary.BigEndian, &npdu.HopCount)
		if err != nil {
			return fmt.Errorf("Failed to read NPDU HopCount: %w", err)
		}
	}

	if npdu.IsNetworkLayerMessage {
		err := binary.Read(buf, binary.BigEndian, &npdu.NetworkMessageType)
		if err != nil {
			return fmt.Errorf("Failed to read NPDU NetworkMessageType: %w", err)
		}
		if npdu.NetworkMessageType > 0x80 {
			err := binary.Read(buf, binary.BigEndian, &npdu.VendorID)
			if err != nil {
				return fmt.Errorf("Failed to read NPDU VendorId: %w", err)
			}
		}
	} else {
		npdu.ADPU = &APDU{}
		return npdu.ADPU.UnmarshalBinary(buf.Bytes())

	}
	return nil
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
	//MaxBACnetConfirmedService ServiceType = 30
)

type APDU struct {
	DataType    PDUType
	ServiceType ServiceType
	Payload     Payload
}

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
func (apdu *APDU) UnmarshalBinary(data []byte) error {
	buf := bytes.NewBuffer(data)
	err := binary.Read(buf, binary.BigEndian, &apdu.DataType)
	if err != nil {
		return fmt.Errorf("Failed to read APDU DataType: %w", err)
	}
	err = binary.Read(buf, binary.BigEndian, &apdu.ServiceType)
	if err != nil {
		return fmt.Errorf("Failed to read APDU ServiceType: %w", err)
	}
	if apdu.DataType == UnconfirmedServiceRequest && apdu.ServiceType == ServiceUnconfirmedWhoIs {
		apdu.Payload = &WhoIs{}
		return apdu.Payload.UnmarshalBinary(buf.Bytes())

	} else if apdu.DataType == UnconfirmedServiceRequest && apdu.ServiceType == ServiceUnconfirmedIAm {
		apdu.Payload = &Iam{}
		return apdu.Payload.UnmarshalBinary(buf.Bytes())

	} else {
		// Just pass raw data, decoding is not yet ready
		apdu.Payload = &DataPayload{buf.Bytes()}
	}

	return nil
}

type Payload interface {
	MarshalBinary() ([]byte, error)
	UnmarshalBinary([]byte) error
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

type BVLCType byte

const BVLCTypeBacnetIP BVLCType = 0x81

// Bacnet Fuction
type BacFunc byte

// List of possible BACnet functions
const (
	BacFuncResult                          BacFunc = 0
	BacFuncWriteBroadcastDistributionTable BacFunc = 1
	BacFuncBroadcastDistributionTable      BacFunc = 2
	BacFuncBroadcastDistributionTableAck   BacFunc = 3
	BacFuncForwardedNPDU                   BacFunc = 4
	BacFuncUnicast                         BacFunc = 10
	BacFuncBroadcast                       BacFunc = 11
)

type BVLC struct {
	Type     BVLCType
	Function BacFunc
	//maybe Payload here ?
	NPDU NPDU
}

func (bvlc BVLC) MarshalBinary() ([]byte, error) {
	b := &bytes.Buffer{}
	b.WriteByte(byte(bvlc.Type))
	b.WriteByte(byte(bvlc.Function))
	data, err := bvlc.NPDU.MarshalBinary()
	if err != nil {
		return nil, err
	}
	len := uint16(4 + len(data)) //len includes Type,Function and itself
	_ = binary.Write(b, binary.BigEndian, len)
	b.Write(data)
	return b.Bytes(), nil
}

func (bvlc *BVLC) UnmarshalBinary(data []byte) error {
	buf := bytes.NewBuffer(data)
	bvlcType, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("Failed to read bvlc type: %w", err)
	}
	bvlcFunc, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("Failed to read bvlc func: %w", err)
	}
	var length uint16
	err = binary.Read(buf, binary.BigEndian, &length)
	if err != nil {
		return fmt.Errorf("Failed to read bvlc length: %w", err)
	}
	remaining := buf.Bytes()

	bvlc.Type = BVLCType(bvlcType)
	bvlc.Function = BacFunc(bvlcFunc)
	if len(remaining) != int(length)-4 {
		return fmt.Errorf("Incoherent Length field in BVCL. Advertized payload size is %d, real size  %d", length-4, len(remaining))
	}
	return bvlc.NPDU.UnmarshallBinary(remaining)
}

type ObjectType uint16
type ObjectInstance uint32

type ObjectID struct {
	Type     ObjectType
	Instance ObjectInstance
}

type SegmentationSupport byte

const (
	SegmentationSupportBoth     SegmentationSupport = 0x00
	SegmentationSupportTransmit SegmentationSupport = 0x01
	SegmentationSupportReceive  SegmentationSupport = 0x02
	SegmentationSupportNone     SegmentationSupport = 0x03
)
