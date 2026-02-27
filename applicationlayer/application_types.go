package applicationlayer

//go:generate stringer -type=PDUType
type PDUType byte

const (
	ConfirmedServiceRequest   PDUType = 0
	UnconfirmedServiceRequest PDUType = 0x1
	SimpleAck                 PDUType = 0x2
	ComplexAck                PDUType = 0x3
	SegmentAck                PDUType = 0x4
	Error                     PDUType = 0x5
	Reject                    PDUType = 0x6
	Abort                     PDUType = 0x7
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
