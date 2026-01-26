package bacip

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// //go:generate stringer -type=PDUType
type PDUType byte

// TODO: Maybe do from 0 to 7
const (
	ConfirmedServiceRequest   PDUType = 0
	UnconfirmedServiceRequest PDUType = 0x10
	SimpleAck                 PDUType = 0x20
	ComplexAck                PDUType = 0x30
	SegmentAck                PDUType = 0x40
	Error                     PDUType = 0x50
	Reject                    PDUType = 0x60
	Abort                     PDUType = 0x70
)

type ServiceType byte

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

// Todo: support more complex APDU
type APDU struct {
	DataType    PDUType
	ServiceType ServiceType
	Payload     Payload
	//Only meaningfully for confirmed and ack
	InvokeID byte
	// MaxSegs
	// Segmented message
	// MoreFollow
	// SegmentedResponseAccepted
	// MaxApdu int
	// Sequence                  uint8
	// WindowNumber              uint8
}

func (apdu APDU) MarshalBinary() ([]byte, error) {
	b := &bytes.Buffer{}
	b.WriteByte(byte(apdu.DataType))
	if apdu.DataType == ConfirmedServiceRequest {
		b.WriteByte(5) //Todo: Write other  control flag here
		b.WriteByte(apdu.InvokeID)
	}
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
		return fmt.Errorf("read APDU DataType: %w", err)
	}
	if apdu.DataType == ComplexAck || apdu.DataType == Error {
		apdu.InvokeID, err = buf.ReadByte()
		if err != nil {
			return err
		}
	}
	//Todo refactor
	err = binary.Read(buf, binary.BigEndian, &apdu.ServiceType)
	if err != nil {
		return fmt.Errorf("read APDU ServiceType: %w", err)
	}
	if apdu.DataType == UnconfirmedServiceRequest && apdu.ServiceType == ServiceUnconfirmedWhoIs {
		apdu.Payload = &WhoIs{}

	} else if apdu.DataType == UnconfirmedServiceRequest && apdu.ServiceType == ServiceUnconfirmedIAm {
		apdu.Payload = &Iam{}

	} else if apdu.DataType == ComplexAck && apdu.ServiceType == ServiceConfirmedReadProperty {
		apdu.Payload = &ReadProperty{}

	} else if apdu.DataType == Error {
		apdu.Payload = &ApduError{}
	} else {
		// Just pass raw data, decoding is not yet ready
		apdu.Payload = &DataPayload{}
	}
	return apdu.Payload.UnmarshalBinary(buf.Bytes())

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
