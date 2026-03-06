// Package bacnet provides various types to represent Bacnet related concepts
package bacnet

import (
	"container/list"
	"errors"
	"fmt"
)

const (
	MaxInstance   = 0x3FFFFF
	instanceBits  = 22
	maxObjectType = 0x400
)

// ObjectType is the category of an object
type ObjectType uint16

// BACnetObjectIdentifier is a unique identifier of an bacnet object
type BACnetObjectIdentifier uint32

//go:generate stringer -type=ObjectType
const (
	AnalogInput           ObjectType = 0x00
	AnalogOutput          ObjectType = 0x01
	AnalogValue           ObjectType = 0x02
	BinaryInput           ObjectType = 0x03
	BinaryOutput          ObjectType = 0x04
	BinaryValue           ObjectType = 0x05
	Calendar              ObjectType = 0x06
	Command               ObjectType = 0x07
	BacnetDevice          ObjectType = 0x08
	EventEnrollment       ObjectType = 0x09
	File                  ObjectType = 0x0A
	Group                 ObjectType = 0x0B
	Loop                  ObjectType = 0x0C
	MultiStateInput       ObjectType = 0x0D
	MultiStateOutput      ObjectType = 0x0E
	NotificationClass     ObjectType = 0x0F
	Program               ObjectType = 0x10
	Schedule              ObjectType = 0x11
	Averaging             ObjectType = 0x12
	MultiStateValue       ObjectType = 0x13
	Trendlog              ObjectType = 0x14
	LifeSafetyPoint       ObjectType = 0x15
	LifeSafetyZone        ObjectType = 0x16
	Accumulator           ObjectType = 0x17
	PulseConverter        ObjectType = 0x18
	EventLog              ObjectType = 0x19
	GlobalGroup           ObjectType = 0x1A
	TrendLogMultiple      ObjectType = 0x1B
	LoadControl           ObjectType = 0x1C
	StructuredView        ObjectType = 0x1D
	AccessDoor            ObjectType = 0x1E
	Timer                 ObjectType = 0x1F
	AccessCredential      ObjectType = 0x20 // Addendum 2008-j
	AccessPoint           ObjectType = 0x21
	AccessRights          ObjectType = 0x22
	AccessUser            ObjectType = 0x23
	AccessZone            ObjectType = 0x24
	CredentialDataInput   ObjectType = 0x25 // Authentication-factor-input
	NetworkSecurity       ObjectType = 0x26 // Addendum 2008-g
	BitstringValue        ObjectType = 0x27 // Addendum 2008-w
	CharacterstringValue  ObjectType = 0x28 // Addendum 2008-w
	DatePatternValue      ObjectType = 0x29 // Addendum 2008-w
	DateValue             ObjectType = 0x2a // Addendum 2008-w
	DatetimePatternValue  ObjectType = 0x2b // Addendum 2008-w
	DatetimeValue         ObjectType = 0x2c // Addendum 2008-w
	IntegerValue          ObjectType = 0x2d // Addendum 2008-w
	LargeAnalogValue      ObjectType = 0x2e // Addendum 2008-w
	OctetstringValue      ObjectType = 0x2f // Addendum 2008-w
	PositiveIntegerValue  ObjectType = 0x30 // Addendum 2008-w
	TimePatternValue      ObjectType = 0x31 // Addendum 2008-w
	TimeValue             ObjectType = 0x32 // Addendum 2008-w
	NotificationForwarder ObjectType = 0x33 // Addendum 2010-af
	AlertEnrollment       ObjectType = 0x34 // Addendum 2010-af
	Channel               ObjectType = 0x35 // Addendum 2010-aa
	LightingOutput        ObjectType = 0x36 // Addendum 2010-i
	BinaryLightingOutput  ObjectType = 0x37 // Addendum 135-2012az
	NetworkPort           ObjectType = 0x38 // Addendum 135-2012az
	ProprietaryMin        ObjectType = 0x80
	Proprietarymax        ObjectType = 0x3ff
)

type BACnetDeviceStatus uint16

const (
	DeviceStatusOperational         BACnetDeviceStatus = 0
	DeviceStatusOperationalReadOnly BACnetDeviceStatus = 1
	DeviceStatusDownloadRequired    BACnetDeviceStatus = 2
	DeviceStatusDownloadInProgress  BACnetDeviceStatus = 3
	DeviceStatusNonOperational      BACnetDeviceStatus = 4
	DeviceStatusBackupInProgress    BACnetDeviceStatus = 5
)

type BACnetServicesSupported uint

const (
	ServicesSupportedAcknowledgeAlaram                  BACnetServicesSupported = 0
	ServicesSupportedConfirmedCovNotification           BACnetServicesSupported = 1
	ServicesSupportedConfirmedCovNotificationMultiple   BACnetServicesSupported = 42
	ServicesSupportedConfirmedEventNotification         BACnetServicesSupported = 2
	ServicesSupportedGetAlarmSummary                    BACnetServicesSupported = 3
	ServicesSupportedGetEnrollmentSummary               BACnetServicesSupported = 4
	ServicesSupportedGetEventInformation                BACnetServicesSupported = 39
	ServicesSupportedLifeSafetyOperation                BACnetServicesSupported = 37
	ServicesSupportedSubscribeCov                       BACnetServicesSupported = 5
	ServicesSupportedSubscribeCovProperty               BACnetServicesSupported = 38
	ServicesSupportedSubscribeCovPropertyMultiple       BACnetServicesSupported = 41
	ServicesSupportedConfirmedAuditNotification         BACnetServicesSupported = 44
	ServicesSupportedAtomicReadFile                     BACnetServicesSupported = 6
	ServicesSupportedAtomicWriteFile                    BACnetServicesSupported = 7
	ServicesSupportedAddListElement                     BACnetServicesSupported = 8
	ServicesSupportedRemoveListElement                  BACnetServicesSupported = 9
	ServicesSupportedCreateObject                       BACnetServicesSupported = 10
	ServicesSupportedDeleteObject                       BACnetServicesSupported = 11
	ServicesSupportedReadProperty                       BACnetServicesSupported = 12
	ServicesSupportedReadPropertyMultiple               BACnetServicesSupported = 14
	ServicesSupportedReadRange                          BACnetServicesSupported = 35
	ServicesSupportedWriteGroup                         BACnetServicesSupported = 40
	ServicesSupportedWriteProperty                      BACnetServicesSupported = 15
	ServicesSupportedWritePropertyMultiple              BACnetServicesSupported = 16
	ServicesSupportedAuditLogQuery                      BACnetServicesSupported = 45
	ServicesSupportedDeviceCommunicationControl         BACnetServicesSupported = 17
	ServicesSupportedConfirmedPrivateTransfer           BACnetServicesSupported = 18
	ServicesSupportedConfirmedTextMessage               BACnetServicesSupported = 19
	ServicesSupportedReinitializeDevice                 BACnetServicesSupported = 20
	ServicesSupportedWhoAmI                             BACnetServicesSupported = 47
	ServicesSupportedYouAre                             BACnetServicesSupported = 48
	ServicesSupportedAuthRequest                        BACnetServicesSupported = 49
	ServicesSupportedVtOpen                             BACnetServicesSupported = 21
	ServicesSupportedVtClose                            BACnetServicesSupported = 22
	ServicesSupportedVtData                             BACnetServicesSupported = 23
	ServicesSupportedIAm                                BACnetServicesSupported = 26
	ServicesSupportedIHave                              BACnetServicesSupported = 27
	ServicesSupportedUnconfirmedCovNotification         BACnetServicesSupported = 28
	ServicesSupportedUnconfirmedCovNotificationMultiple BACnetServicesSupported = 43
	ServicesSupportedUnconfirmedEventNotification       BACnetServicesSupported = 29
	ServicesSupportedUnconfirmedPrivateTransfer         BACnetServicesSupported = 30
	ServicesSupportedUnconfirmedTextMessage             BACnetServicesSupported = 31
	ServicesSupportedTimeSynchronization                BACnetServicesSupported = 32
	ServicesSupportedUtcTimeSynchronization             BACnetServicesSupported = 36
	ServicesSupportedWhoHas                             BACnetServicesSupported = 33
	ServicesSupportedWhoIs                              BACnetServicesSupported = 34
	ServicesSupportedUnconfirmedAuditNotification       BACnetServicesSupported = 46
	// Removed services
	ServicesSupportedReadPropertyConditional BACnetServicesSupported = 13
	ServicesSupportedAuthenticate            BACnetServicesSupported = 24
	ServicesSupportedRequestKey              BACnetServicesSupported = 25
)

type BACnetObjectTypesSupported uint

const (
	ObjectTypesSupportedAnalogInput           BACnetObjectTypesSupported = 0
	ObjectTypesSupportedAnalogOutput          BACnetObjectTypesSupported = 1
	ObjectTypesSupportedAnalogValue           BACnetObjectTypesSupported = 2
	ObjectTypesSupportedBinaryInput           BACnetObjectTypesSupported = 3
	ObjectTypesSupportedBinaryOutput          BACnetObjectTypesSupported = 4
	ObjectTypesSupportedBinaryValue           BACnetObjectTypesSupported = 5
	ObjectTypesSupportedCalendar              BACnetObjectTypesSupported = 6
	ObjectTypesSupportedCommand               BACnetObjectTypesSupported = 7
	ObjectTypesSupportedDevice                BACnetObjectTypesSupported = 8
	ObjectTypesSupportedEventEnrollment       BACnetObjectTypesSupported = 9
	ObjectTypesSupportedFile                  BACnetObjectTypesSupported = 10
	ObjectTypesSupportedGroup                 BACnetObjectTypesSupported = 11
	ObjectTypesSupportedLoop                  BACnetObjectTypesSupported = 12
	ObjectTypesSupportedMultiStateInput       BACnetObjectTypesSupported = 13
	ObjectTypesSupportedMultiStateOutput      BACnetObjectTypesSupported = 14
	ObjectTypesSupportedNotificationClass     BACnetObjectTypesSupported = 15
	ObjectTypesSupportedProgram               BACnetObjectTypesSupported = 16
	ObjectTypesSupportedSchedule              BACnetObjectTypesSupported = 17
	ObjectTypesSupportedAveraging             BACnetObjectTypesSupported = 18
	ObjectTypesSupportedMultiStateValue       BACnetObjectTypesSupported = 19
	ObjectTypesSupportedTrendLog              BACnetObjectTypesSupported = 20
	ObjectTypesSupportedLifeSafetyPoint       BACnetObjectTypesSupported = 21
	ObjectTypesSupportedLifeSafetyZone        BACnetObjectTypesSupported = 22
	ObjectTypesSupportedAccumulator           BACnetObjectTypesSupported = 23
	ObjectTypesSupportedPulseConverter        BACnetObjectTypesSupported = 24
	ObjectTypesSupportedEventLog              BACnetObjectTypesSupported = 25
	ObjectTypesSupportedGlobalGroup           BACnetObjectTypesSupported = 26
	ObjectTypesSupportedTrendLogMultiple      BACnetObjectTypesSupported = 27
	ObjectTypesSupportedLoadControl           BACnetObjectTypesSupported = 28
	ObjectTypesSupportedStructuredView        BACnetObjectTypesSupported = 29
	ObjectTypesSupportedAccessDoor            BACnetObjectTypesSupported = 30
	ObjectTypesSupportedTimer                 BACnetObjectTypesSupported = 31
	ObjectTypesSupportedAccessCredential      BACnetObjectTypesSupported = 33
	ObjectTypesSupportedAccessPoint           BACnetObjectTypesSupported = 34
	ObjectTypesSupportedAccessRights          BACnetObjectTypesSupported = 34
	ObjectTypesSupportedAccessUser            BACnetObjectTypesSupported = 35
	ObjectTypesSupportedAccessZone            BACnetObjectTypesSupported = 36
	ObjectTypesSupportedCredentialInput       BACnetObjectTypesSupported = 37
	ObjectTypesSupportedBitstringValue        BACnetObjectTypesSupported = 39 // 38 removed
	ObjectTypesSupportedCharacterstringValue  BACnetObjectTypesSupported = 40
	ObjectTypesSupportedDatePatternValue      BACnetObjectTypesSupported = 41
	ObjectTypesSupportedDateValue             BACnetObjectTypesSupported = 42
	ObjectTypesSupportedDatetimePatternValue  BACnetObjectTypesSupported = 43
	ObjectTypesSupportedDatetimeValue         BACnetObjectTypesSupported = 44
	ObjectTypesSupportedIntegerValue          BACnetObjectTypesSupported = 45
	ObjectTypesSupportedLargeAnalogValue      BACnetObjectTypesSupported = 46
	ObjectTypesSupportedOctetStringValue      BACnetObjectTypesSupported = 47
	ObjectTypesSupportedPositiveIntegerValue  BACnetObjectTypesSupported = 48
	ObjectTypesSupportedTimePatternValue      BACnetObjectTypesSupported = 49
	ObjectTypesSupportedTimeValue             BACnetObjectTypesSupported = 50
	ObjectTypesSupportedNotificationForwarder BACnetObjectTypesSupported = 51
	ObjectTypesSupportedAlertEnrollment       BACnetObjectTypesSupported = 52
	ObjectTypesSupportedChannel               BACnetObjectTypesSupported = 53
	ObjectTypesSupportedLightingOutput        BACnetObjectTypesSupported = 54
	ObjectTypesSupportedBinaryLightingOutput  BACnetObjectTypesSupported = 55
	ObjectTypesSupportedNetworkPort           BACnetObjectTypesSupported = 56
	ObjectTypesSupportedElevatorGroup         BACnetObjectTypesSupported = 57
	ObjectTypesSupportedEscalator             BACnetObjectTypesSupported = 58
	ObjectTypesSupportedLift                  BACnetObjectTypesSupported = 59
	ObjectTypesSupportedStaging               BACnetObjectTypesSupported = 60
	ObjectTypesSupportedAuditLog              BACnetObjectTypesSupported = 61
	ObjectTypesSupportedAuditReporter         BACnetObjectTypesSupported = 62
	ObjectTypesSupportedColor                 BACnetObjectTypesSupported = 63
	ObjectTypesSupportedColorTemperature      BACnetObjectTypesSupported = 64
)

type BACnetConfirmedServiceChoice uint

const (
	ConfirmedServiceChoiceAcknowledgeAlaram                BACnetConfirmedServiceChoice = 0
	ConfirmedServiceChoiceConfimedAuditNotification        BACnetConfirmedServiceChoice = 32
	ConfirmedServiceChoiceConfirmedCovNotification         BACnetConfirmedServiceChoice = 1
	ConfirmedServiceChoiceConfirmedCovNotificaitonMultiple BACnetConfirmedServiceChoice = 31
	ConfirmedServiceChoiceConfirmedEventNotification       BACnetConfirmedServiceChoice = 2
	ConfirmedServiceChoiceGetAlramSummary                  BACnetConfirmedServiceChoice = 3
	ConfirmedServiceChoiceGetEnrollmentSummary             BACnetConfirmedServiceChoice = 4
	ConfirmedServiceChoiceGetEventInformation              BACnetConfirmedServiceChoice = 29
	ConfirmedServiceChoiceLifeSafetyOperation              BACnetConfirmedServiceChoice = 27
	ConfirmedServiceChoiceSubscribeCov                     BACnetConfirmedServiceChoice = 5
	ConfirmedServiceChoiceSubscribeCovProperty             BACnetConfirmedServiceChoice = 28
	ConfirmedServiceChoiceSubscribeCovPropertyMultiple     BACnetConfirmedServiceChoice = 30
	ConfirmedServiceChoiceAtomicReadFile                   BACnetConfirmedServiceChoice = 6
	ConfirmedServiceChoiceAtomicWriteFile                  BACnetConfirmedServiceChoice = 7
	ConfirmedServiceChoiceAddListElement                   BACnetConfirmedServiceChoice = 8
	ConfirmedServiceChoiceRemoveListElement                BACnetConfirmedServiceChoice = 9
	ConfirmedServiceChoiceCreateObject                     BACnetConfirmedServiceChoice = 10
	ConfirmedServiceChoiceDeleteObject                     BACnetConfirmedServiceChoice = 11
	ConfirmedServiceChoiceReadProperty                     BACnetConfirmedServiceChoice = 12
	ConfirmedServiceChoiceReadPropertyMultiple             BACnetConfirmedServiceChoice = 14
	ConfirmedServiceChoiceReadRange                        BACnetConfirmedServiceChoice = 26
	ConfirmedServiceChoiceWriteProperty                    BACnetConfirmedServiceChoice = 15
	ConfirmedServiceChoiceWritePropertyMultiple            BACnetConfirmedServiceChoice = 16
	ConfirmedServiceChoiceAuditLogQuery                    BACnetConfirmedServiceChoice = 33
	ConfirmedServiceChoiceDeviceCommunicationControl       BACnetConfirmedServiceChoice = 17
	ConfirmedServiceChoiceConfirmedPrivateTransfer         BACnetConfirmedServiceChoice = 18
	ConfirmedServiceChoiceConfirmedTextMessage             BACnetConfirmedServiceChoice = 19
	ConfirmedServiceChoiceReinitializeDevice               BACnetConfirmedServiceChoice = 20
	ConfirmedServiceChoiceAuthRequest                      BACnetConfirmedServiceChoice = 34
	ConfirmedServiceChoiceVtOpen                           BACnetConfirmedServiceChoice = 21
	ConfirmedServiceChoiceVtClose                          BACnetConfirmedServiceChoice = 22
	ConfirmedServiceChoiceVtData                           BACnetConfirmedServiceChoice = 23
)

type BACnetUnconfirmedServiceChoice uint

const (
	UnconfirmedServiceChoiceIAm                                BACnetUnconfirmedServiceChoice = 0
	UnconfirmedServiceChoiceIHave                              BACnetUnconfirmedServiceChoice = 1
	UnconfirmedServiceChoiceUnconfirmedCovNotification         BACnetUnconfirmedServiceChoice = 2
	UnconfirmedServiceChoiceUnconfirmedEventNotification       BACnetUnconfirmedServiceChoice = 3
	UnconfirmedServiceChoiceUnconfirmedPrivateTransfer         BACnetUnconfirmedServiceChoice = 4
	UnconfirmedServiceChoiceUnconfirmedTextMessage             BACnetUnconfirmedServiceChoice = 5
	UnconfirmedServiceChoiceTimeSynchronization                BACnetUnconfirmedServiceChoice = 6
	UnconfirmedServiceChoiceWhoHas                             BACnetUnconfirmedServiceChoice = 7
	UnconfirmedServiceChoiceWhoIs                              BACnetUnconfirmedServiceChoice = 8
	UnconfirmedServiceChoiceUtcTimeSynchronization             BACnetUnconfirmedServiceChoice = 9
	UnconfirmedServiceChoiceWriteGroup                         BACnetUnconfirmedServiceChoice = 10
	UnconfirmedServiceChoiceUnconfirmedCovNotificationMultiple BACnetUnconfirmedServiceChoice = 11
	UnconfirmedServiceChoiceUnconfirmedAuditNotification       BACnetUnconfirmedServiceChoice = 12
	UnconfirmedServiceChoiceWhoAmI                             BACnetUnconfirmedServiceChoice = 13
	UnconfirmedServiceChoiceYouAre                             BACnetUnconfirmedServiceChoice = 14
)

const (
	LocalDNET     = 0x0
	BroadcastDNET = 0xffff
)

type NetworkNumber uint16

type MAC interface {
	GetBytes() []byte
	FromBytes([]byte)
	String() string
	IsBroadcast() bool
	Equal(MAC) bool
}

// AbstractMAC is a MAC address backed by a plain byte slice.
type AbstractMAC []byte

func (m AbstractMAC) GetBytes() []byte    { return []byte(m) }
func (m *AbstractMAC) FromBytes(b []byte) { *m = AbstractMAC(b) }
func (m AbstractMAC) String() string      { return fmt.Sprintf("%x", []byte(m)) }
func (m AbstractMAC) IsBroadcast() bool   { return false }
func (m AbstractMAC) Equal(o MAC) bool {
	ob := o.GetBytes()
	if len(m) != len(ob) {
		return false
	}
	for i := range m {
		if m[i] != ob[i] {
			return false
		}
	}
	return true
}

// ObjectID represent the type of a bacnet object and it's instance number
type ObjectID struct {
	Type     ObjectType
	Instance BACnetObjectIdentifier
}

// Encode turns the object ID into a uint32 for encoding.  Returns an
// error if the ObjectID is invalid
func (o ObjectID) Encode() (uint32, error) {
	if o.Instance > MaxInstance {
		return 0, errors.New("invalid ObjectID: instance too high")
	}
	if o.Type > maxObjectType {
		return 0, errors.New("invalid ObjectID: objectType too high too high")
	}
	v := uint32(o.Type)<<instanceBits | (uint32(o.Instance))
	return v, nil
}

func ObjectIDFromUint32(v uint32) ObjectID {
	return ObjectID{
		Type:     ObjectType(v >> instanceBits), //nolint:gosec
		Instance: BACnetObjectIdentifier(v & MaxInstance),
	}
}

// Device represent a bacnet device. Note: A bacnet device is different
// from a bacnet object. A device "contains" several object. Only the device has a bacnet address
type Device struct {
	ID           ObjectID
	MaxApdu      uint32
	Segmentation SegmentationSupport
	Vendor       uint32
	Addr         BACnetAddress
}

// Address is the bacnet address of an device.
type Address struct {
	// mac_len = 0 is a broadcast address
	// note: MAC for IP addresses uses 4 bytes for addr, 2 bytes for port
	Mac []byte
	// the following are used if the device is behind a router
	// net = 0 indicates local
	Net uint16 // BACnet network number
	Adr []byte // hwaddr (MAC) address
}

//go:generate stringer -type=SegmentationSupport
type SegmentationSupport byte

const (
	SegmentationSupportBoth     SegmentationSupport = 0x00
	SegmentationSupportTransmit SegmentationSupport = 0x01
	SegmentationSupportReceive  SegmentationSupport = 0x02
	SegmentationSupportNone     SegmentationSupport = 0x03
)

// NbSegmentsAccepted* constants encode the MaxSegmentsAccepted field of a confirmed-request APDU header.
const (
	NbSegmentsAcceptedUnspecified = 0b000
	NbSegmentsAccepted2           = 0b001
	NbSegmentsAccepted4           = 0b010
	NbSegmentsAccepted8           = 0b011
	NbSegmentsAccepted16          = 0b100
	NbSegmentsAccepted32          = 0b101
	NbSegmentsAccepted64          = 0b110
	NbSegmentsAcceptedOver64      = 0b111
)

// MaxApduLen* constants encode the MaxAPDU length accepted field of a confirmed-request APDU header.
const (
	MaxApduLenMinimum = 0b0000
	MaxApduLen128     = 0b0001
	MaxApduLen206     = 0b0010
	MaxApduLen480     = 0b0011
	MaxApduLen1024    = 0b0100
	MaxApduLen1476    = 0b0101
)

// PropertyIdentifierComplex is used to control a ReadProperty request
type PropertyIdentifierComplex struct {
	Type PropertyIdentifier
	//Not null if it's an array property and we want only one index of
	//this array
	ArrayIndex *uint32
}

//go:generate stringer -type=PriorityList
type PriorityList uint8

const (
	ManualLifeSafety1          PriorityList = 1
	ManualLifeSafety2          PriorityList = 2
	Available3                 PriorityList = 3
	Available4                 PriorityList = 4
	CriticalEquipementControl5 PriorityList = 5
	MinimumOnOff6              PriorityList = 6
	Available7                 PriorityList = 7
	ManualOperator8            PriorityList = 8
	Available9                 PriorityList = 9
	Available10                PriorityList = 10
	Available11                PriorityList = 11
	Available12                PriorityList = 12
	Available13                PriorityList = 13
	Available14                PriorityList = 14
	Available15                PriorityList = 15
	Available16                PriorityList = 16
)

type PropertyValue struct {
	Type  byte
	Value any
}

type BitString struct {
	unusedBits uint
	octets     []byte
}

func NewBitString() *BitString {
	return &BitString{
		unusedBits: 0,
		octets:     make([]byte, 0),
	}
}

func (s *BitString) SetBit(position uint) *BitString {
	octetIndex := position / 8
	bitIndex := 7 - (position % 8)
	if octetIndex >= uint(len(s.octets)) {
		// need to grow octets
		fill := make([]byte, int(octetIndex)-len(s.octets)+1) //nolint:gosec
		s.octets = append(s.octets, fill...)
		s.unusedBits = 8
	}
	s.octets[len(s.octets)-1] |= (1 << bitIndex)
	if s.unusedBits > bitIndex {
		s.unusedBits = bitIndex
	}
	return s
}

func (s *BitString) UnsetBit(position uint) *BitString {
	octetIndex := position / 8
	bitIndex := 7 - (position % 8)
	if octetIndex >= uint(len(s.octets)) {
		// the bit is out of s.octets. Nothing to do
		return s
	}
	mask := byte(^(1 << bitIndex))
	s.octets[len(s.octets)-1] &= mask
	var idx int
	for idx = len(s.octets) - 1; idx >= 0; idx-- {
		if s.octets[idx] != 0 {
			break
		}
	}
	s.unusedBits = 0
	s.octets = s.octets[:idx+1]
	if len(s.octets) != 0 {
		for i := 0; i < 8; i++ {
			mask := byte(1 << i)
			if s.octets[idx]&mask != 0 {
				s.unusedBits = uint(i)
			}
		}
	}
	return s
}

func (s *BitString) UnusedBits() uint { return s.unusedBits }
func (s *BitString) Octets() []byte   { return s.octets }

func (s *BitString) IsBitSet(position uint) bool {
	octetIndex := position / 8
	if octetIndex >= uint(len(s.octets)) {
		return false
	}
	bitIndex := 7 - (position % 8)
	mask := byte((1 << bitIndex))
	return s.octets[octetIndex]&mask != 0
}

type BACnetArray[T any] struct {
	array     []T
	resizable bool
}

func NewBACnetArray[T any](size int, resizable bool) BACnetArray[T] {
	return BACnetArray[T]{
		array:     make([]T, size),
		resizable: resizable,
	}
}

func (a *BACnetArray[T]) Len() int {
	return len(a.array)
}

func (a *BACnetArray[T]) Set(values ...T) error {
	if !a.resizable && len(values) != len(a.array) {
		return fmt.Errorf("array is not resizable")
	}
	a.array = make([]T, 0)
	a.array = append(a.array, values...)
	return nil
}

func (a *BACnetArray[T]) SetAt(position uint, v any) error {
	if position == 0 {
		if !a.resizable {
			return fmt.Errorf("array not resizable")
		}
		newSize, ok := v.(uint)
		if !ok {
			return fmt.Errorf("new size should be an unsigned int")
		}
		if int(newSize) < len(a.array) { //nolint:gosec
			a.array = a.array[:newSize]
		} else if int(newSize) > len(a.array) { //nolint:gosec
			arrayCopy := make([]T, newSize)
			copy(arrayCopy, a.array)
			a.array = arrayCopy
		}
		return nil
	}
	index := position - 1
	if int(index) >= len(a.array) { //nolint:gosec
		return fmt.Errorf("out of bound")
	}
	newValue, ok := v.(T)
	if !ok {
		return fmt.Errorf("wrong value type")
	}
	a.array[index] = newValue
	return nil
}

func (a *BACnetArray[T]) Get(position uint) (any, error) {
	if position == 0 {
		return uint(len(a.array)), nil
	}
	index := int(position - 1) //nolint:gosec
	if index >= len(a.array) {
		return nil, fmt.Errorf("out of bound")
	}
	return a.array[index], nil
}

// TODO: replace any with a comparable type
type BACnetList[T any] struct {
	list *list.List
}

func NewBACnetList[T any]() BACnetList[T] {
	return BACnetList[T]{
		list: list.New(),
	}
}

type BACnetAddress struct {
	Mac     MAC
	Network NetworkNumber
}

func (a *BACnetAddress) Equal(o *BACnetAddress) bool {
	return a.Mac.Equal(o.Mac) && a.Network == o.Network
}

func (a *BACnetAddress) String() string {
	return fmt.Sprintf("{ Net: %d, Mac: %s }", a.Network, a.Mac.String())

}

type BACnetAddresBinding struct {
	DeviceIdentifer BACnetObjectIdentifier
	DeviceAddress   BACnetAddress
}

// BACnetEventState enumerates the event states for input objects.
type BACnetEventState uint32

const (
	EventStateNormal          BACnetEventState = 0
	EventStateFault           BACnetEventState = 1
	EventStateOffnormal       BACnetEventState = 2
	EventStateHighLimit       BACnetEventState = 3
	EventStateLowLimit        BACnetEventState = 4
	EventStateLifeSafetyAlarm BACnetEventState = 5
)

// BACnetStatusFlag represents bit positions within the StatusFlags property.
type BACnetStatusFlag uint

const (
	StatusFlagInAlarm      BACnetStatusFlag = 0
	StatusFlagFault        BACnetStatusFlag = 1
	StatusFlagOverridden   BACnetStatusFlag = 2
	StatusFlagOutOfService BACnetStatusFlag = 3
)
