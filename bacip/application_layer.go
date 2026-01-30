package bacip

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/REQUEA/bacnet"
)

const (
	// TODO define those as part of the entity and make them configurable
	Ndup = 10
)

const (
	pduTypeMask  uint8 = 0b11110000
	pduTypeShift       = 4
	segMask      uint8 = 0b00001000
	segShift     int   = 3
	morMask      uint8 = 0b00000100
	morShift           = 2
	saMask       uint8 = 0b00000010
	saShift            = 1
	maxSegsMask  uint8 = 0b01110000
	maxSegsShift       = 4
	maxRespMask  uint8 = 0b00001111
	nakMask      uint8 = 0b00000010
	nakShift           = 1
	srvMask      uint8 = 0b00000001
	allFlagsMask uint8 = 0b00001111
)

type ApplicationEntity struct {
	ServerTransactions      []*ServerTransaction
	ClientTransactions      []*ClientTransaction
	clientTransactionsMutex sync.Mutex
	serverTransactionsMutex sync.Mutex
	serviceLayer            *ServiceLayer
	networkEntity           NetworkEntity
}

func NewApplicationEntity() *ApplicationEntity {
	return &ApplicationEntity{
		ServerTransactions: make([]*ServerTransaction, 0),
		ClientTransactions: make([]*ClientTransaction, 0),
	}
}

func (ae *ApplicationEntity) getServerTransaction(id *TransactionId) *ServerTransaction {
	ae.serverTransactionsMutex.Lock()
	defer ae.serverTransactionsMutex.Unlock()
	for _, t := range ae.ServerTransactions {
		if t.Id.Equal(id) {
			return t
		}
	}
	return nil
}

func (ae *ApplicationEntity) removeServerTransaction(id *TransactionId) *ServerTransaction {
	ae.serverTransactionsMutex.Lock()
	defer ae.serverTransactionsMutex.Unlock()
	for i := range ae.ServerTransactions {
		if ae.ServerTransactions[i].Id.Equal(id) {
			result := ae.ServerTransactions[i]
			ae.ServerTransactions[i] = nil
			return result
		}
	}
	return nil
}

func (ae *ApplicationEntity) addServerTransaction(t *ServerTransaction) {
	ae.serverTransactionsMutex.Lock()
	defer ae.serverTransactionsMutex.Unlock()
	for i := range ae.ServerTransactions {
		if ae.ServerTransactions[i] == nil {
			ae.ServerTransactions[i] = t
			return
		}
	}
	ae.ServerTransactions = append(ae.ServerTransactions, t)
}

func (ae *ApplicationEntity) getClientTransaction(id *TransactionId) *ClientTransaction {
	ae.clientTransactionsMutex.Lock()
	defer ae.clientTransactionsMutex.Unlock()
	for _, t := range ae.ClientTransactions {
		if t.Id.Equal(id) {
			return t
		}
	}
	return nil
}

func (ae *ApplicationEntity) addClientTransaction(t *ClientTransaction) {
	ae.clientTransactionsMutex.Lock()
	defer ae.clientTransactionsMutex.Unlock()
	ae.ClientTransactions = append(ae.ClientTransactions, t)
}

type ConfServFlags struct {
	SegmentedRequest          bool
	MoreSegments              bool
	SegmentedResponseAccepted bool
}

func (f *ConfServFlags) FromByte(b byte) {
	f.SegmentedRequest = (b & segMask) != 0
	f.MoreSegments = ((b & morMask) != 0)
	f.SegmentedResponseAccepted = (b & saMask) != 0
}

func (f *ConfServFlags) ToByte() byte {
	var result byte = 0
	if f.SegmentedRequest {
		result |= segMask
	}
	if f.MoreSegments {
		result |= morMask
	}
	if f.SegmentedResponseAccepted {
		result |= saMask
	}
	return result
}

type ComplexAckFlags struct {
	SegmentedRequest bool
	MoreSegments     bool
}

func (f *ComplexAckFlags) FromByte(b byte) {
	f.SegmentedRequest = (b & segMask) != 0
	f.MoreSegments = ((b & morMask) != 0)
}

func (f *ComplexAckFlags) ToByte() byte {
	var result byte = 0
	if f.SegmentedRequest {
		result |= segMask
	}
	if f.MoreSegments {
		result |= morMask
	}
	return result
}

type SegmentAckFlags struct {
	NegativeAck  bool
	SentByServer bool
}

func (f *SegmentAckFlags) FromByte(b byte) {
	f.NegativeAck = (b & nakMask) != 0
	f.SentByServer = (b & srvMask) != 0
}

func (f *SegmentAckFlags) ToByte() byte {
	var result uint8 = 0
	if f.NegativeAck {
		result |= nakMask
	}
	if f.SentByServer {
		result |= srvMask
	}
	return result
}

type AbortFlags struct {
	SentByServer bool
}

func (f *AbortFlags) FromByte(b byte) {
	f.SentByServer = (b & srvMask) != 0
}

func (f *AbortFlags) ToByte() byte {
	var result uint8 = srvMask
	if f.SentByServer {
		result |= srvMask
	}
	return result
}

type NPDUIndication struct {
	source *bacnet.BACnetAddress
	dest   *bacnet.BACnetAddress
	apdu   []byte
}

type ServerNetworkTransactionEvent struct {
	indication     *NPDUIndication
	transaction    *ServerTransaction
	header         PDUHeader
	serviceRequest []byte
}

func (e *ServerNetworkTransactionEvent) Exec() {
	switch e.header.GetType() {
	case ConfirmedServiceRequest:
		header, ok := e.header.(*ConfirmedServiceRequestHeader)
		if !ok {
			logger.Error("wrong header type for ConfirmedServiceRequest")
			return
		}
		e.transaction.HandleConfirmedServiceRequestPdu(e.indication, header, e.serviceRequest)
	case SegmentAck:
		header, ok := e.header.(*SegmentAckHeader)
		if !ok {
			logger.Error("wrong header type for SegmentAck")
			return
		}
		e.transaction.HandleSegmentAckPdu(header)
	case Abort:
		header, ok := e.header.(*AbortHeader)
		if !ok {
			logger.Error("wrong header type for Abort")
			return
		}
		e.transaction.HandleAbortPdu(int(header.AbortReason))
	default:
		logger.Trace("Invalid PDU received for a server transaction: ", e.header.GetType().String())
	}
}

func (ae *ApplicationEntity) HandleNUNITDATAIndication(indication *NPDUIndication) {
	if len(indication.apdu) < 1 {
		logger.Error("N-UNITDATA.indication handling: empty APDU")
		return
	}
	pduType := PDUType((indication.apdu[0] & pduTypeMask) >> pduTypeShift)
	switch pduType {
	case SimpleAck, ComplexAck, Error, Reject:
		// client
	case ConfirmedServiceRequest:
		// server
		ae.handleConfirmedServiceRequestPDU(indication)
	case UnconfirmedServiceRequest:
		// server
		ae.handleUnconfirmedServiceRequestPDU(indication)
	case SegmentAck:
		// client and server
		ae.handleSegmentAckPDU(indication)
	case Abort:
		// client and server
		ae.handleAbortPDU(indication)
	default:
		logger.Error("unsupported PDU type: ", pduType)
	}
}

const (
	nbSegmentsAcceptedUnspecified = 0b000
	nbSegmentsAccepted2           = 0b001
	nbSegmentsAccepted4           = 0b010
	nbSegmentsAccepted8           = 0b011
	nbSegmentsAccepted16          = 0b100
	nbSegmentsAccepted32          = 0b101
	nbSegmentsAccepted64          = 0b110
	nbSegmentsAcceptedOver64      = 0b111

	maxApduLenMinimum = 0b0000
	maxApduLen128     = 0b0001
	maxApduLen206     = 0b0010
	maxApduLen480     = 0b0011
	maxApduLen1024    = 0b0100
	maxApduLen1476    = 0b0101
)

type PDUHeader interface {
	GetType() PDUType
}

type ConfirmedServiceRequestHeader struct {
	Flags              ConfServFlags
	MaxSegs            int
	MaxResp            int
	InvokeId           uint
	SequenceNumber     uint
	ProposedWindowSize uint
	ServiceChoice      bacnet.BACnetConfirmedServiceChoice
}

func (h *ConfirmedServiceRequestHeader) GetType() PDUType {
	return ConfirmedServiceRequest
}

func (h *ConfirmedServiceRequestHeader) Marshal() ([]byte, error) {
	typeFlag := uint8(ConfirmedServiceRequest<<4) | h.Flags.ToByte()
	segsResp := uint8((h.MaxSegs&0x7)<<4 | (h.MaxResp & 0xf))
	var result []byte
	if h.Flags.SegmentedRequest {
		result = []byte{
			typeFlag, segsResp,
			uint8(h.InvokeId),
			uint8(h.SequenceNumber),
			uint8(h.ProposedWindowSize),
			uint8(h.ServiceChoice),
		}
	} else {
		result = []byte{
			typeFlag, segsResp,
			uint8(h.InvokeId),
			uint8(h.ServiceChoice),
		}
	}
	return result, nil
}

func (ae *ApplicationEntity) handleConfirmedServiceRequestPDU(indication *NPDUIndication) error {
	if indication.dest.Mac.IsBroadcast() || indication.dest.Network == bacnet.BroadcastDNET {
		// Drop
		// TODO: Log?
		return nil
	}
	apdu := indication.apdu
	var header ConfirmedServiceRequestHeader
	header.Flags.FromByte(apdu[0])
	if header.Flags.SegmentedRequest && len(apdu) < 6 {
		return fmt.Errorf("Segmented Confirmed-Service-Request too short")
	} else if !header.Flags.SegmentedRequest && len(apdu) < 4 {
		return fmt.Errorf("Confirmed-Service-Request too short")
	}
	var serviceRequest []byte
	header.MaxSegs = int((apdu[1] & maxSegsMask) >> maxSegsShift)
	header.MaxResp = int(apdu[1] & maxRespMask)
	header.InvokeId = uint(apdu[2])
	if header.Flags.SegmentedRequest {
		header.SequenceNumber = uint(apdu[3])
		header.ProposedWindowSize = uint(apdu[4])
		header.ServiceChoice = bacnet.BACnetConfirmedServiceChoice(apdu[5])
		serviceRequest = apdu[6:]
	} else {
		header.SequenceNumber = 0
		header.ProposedWindowSize = 0
		header.ServiceChoice = bacnet.BACnetConfirmedServiceChoice(apdu[3])
		serviceRequest = apdu[4:]
	}
	transactionId := NewTransactionId(indication.source, header.InvokeId)
	transaction := ae.getServerTransaction(&transactionId)
	if transaction != nil {
		// TODO log or drop?
	}
	tr := NewServerTransaction(&transactionId)
	// Find device from service
	// Need to fetch the device to know the values for the timers
	device := tr.serviceLayer.GetDevice(uint(header.ServiceChoice), serviceRequest)
	if device == nil {
		// reply with an error?
	}
	tr.Device = device
	tr.Source = indication.source
	tr.Dest = indication.dest
	// ConfirmedSegmentedReceived
	deviceObject := device.deviceObject
	maxSegmentsAcceptedProp := deviceObject.GetProperty(bacnet.MaxSegmentsAccepted)
	if maxSegmentsAcceptedProp == nil {
		return fmt.Errorf("device object without max-segments-accepted property")
	}
	maxSegmentsAccepted, ok := maxSegmentsAcceptedProp.GetValue().(uint)
	if !ok {
		// should not be possible
		return fmt.Errorf("max-segments-accepted value is not an unsigned int")
	}
	tr.MaxSegmentsAccepted = maxSegmentsAccepted

	apduTimeoutProp := deviceObject.GetProperty(bacnet.ApduTimeout)
	if apduTimeoutProp == nil {
		return fmt.Errorf("device object without apdu-timeout property")
	}
	apduTimeout, ok := apduTimeoutProp.GetValue().(uint)
	if !ok {
		// should not be possible
		return fmt.Errorf("apdu-timeout value is not an unsigned int")
	}
	tr.RequestTimer = NewTransactionTimer(
		time.Duration(apduTimeout)*time.Millisecond,
		func() {},
	)

	apduSegmentTimeoutProp := deviceObject.GetProperty(bacnet.ApduSegmentTimeout)
	if apduSegmentTimeoutProp == nil {
		return fmt.Errorf("device object without apdu-segment-timeout property")
	}
	apduSegmentTimeout, ok := apduSegmentTimeoutProp.GetValue().(uint)
	if !ok {
		// should not be possible
		return fmt.Errorf("apdu-segment-timeout value is not an unsigned int")
	}
	tr.SegmentTimer = NewTransactionTimer(
		time.Duration(apduSegmentTimeout*4)*time.Millisecond,
		tr.OnSegmentTimerFired,
	)

	numberOfApduRetriesProp := deviceObject.GetProperty(bacnet.NumberOfApduRetries)
	if numberOfApduRetriesProp == nil {
		return fmt.Errorf("device object without number-of-apdu-retries property")
	}
	tr.NumberOfApduRetries, ok = numberOfApduRetriesProp.GetValue().(uint)
	if !ok {
		// should not be possible
		return fmt.Errorf("number-of-apdu-retries value is not an unsigned int")
	}

	ae.addServerTransaction(&tr)
	tr.Start() // tr is in IDLE state and can start processing events
	event := &ServerNetworkTransactionEvent{
		indication:     indication,
		header:         &header,
		transaction:    &tr,
		serviceRequest: serviceRequest,
	}
	tr.PushEvent(event)
	return nil
}

func (ae *ApplicationEntity) handleUnconfirmedServiceRequestPDU(indication *NPDUIndication) error {
	apdu := indication.apdu
	if len(apdu) < 2 {
		return fmt.Errorf("Unconfirmed-Service-Request-PDU too short (len: %d)", len(apdu))
	}
	//serviceChoice := bacnet.BACUnconfirmedServiceChoice(apdu[1])
	//serviceRequest := apdu[2:]
	// TODO: pass the PDU to the user application
	return errors.New("not implemented")
}

type SimpleACKHeader struct {
	InvokeId      uint
	ServiceChoice int
}

func (h *SimpleACKHeader) GetType() PDUType {
	return SimpleAck
}

func (h *SimpleACKHeader) Marshal() ([]byte, error) {
	return []byte{
		uint8(SimpleAck << 4),
		uint8(h.InvokeId),
		uint8(h.ServiceChoice),
	}, nil
}

func (ae *ApplicationEntity) handleSimpleAckPDU(indication *NPDUIndication) error {
	apdu := indication.apdu
	if len(apdu) < 3 {
		return fmt.Errorf("SimpleACK-PDU too short (len: %d)", len(apdu))
	}
	header := SimpleACKHeader{
		InvokeId:      uint(apdu[1]),
		ServiceChoice: int(apdu[2]),
	}

	transactionId := NewTransactionId(indication.source, header.InvokeId)
	transaction := ae.getClientTransaction(&transactionId)
	if transaction == nil {
		// client state machine IDLE state UnexpectedPDU_Received
		// Drop message
		return nil
	}
	transaction.HandleSimpleAckPdu(&header)

	return errors.New("not implemented")
}

type ComplexAckHeader struct {
	Flags              ComplexAckFlags
	InvokeId           uint
	SequenceNumber     uint
	ProposedWindowSize uint
	ServiceAckChoice   int // TODO: define a type and accepted values
}

func (h *ComplexAckHeader) GetType() PDUType {
	return ComplexAck
}

func (h *ComplexAckHeader) Marshal() ([]byte, error) {
	typeFlag := uint8(ComplexAck<<4) | h.Flags.ToByte()
	var result []byte
	if h.Flags.SegmentedRequest {
		result = []byte{
			typeFlag,
			uint8(h.InvokeId),
			uint8(h.ServiceAckChoice),
		}
	} else {
		result = []byte{
			typeFlag,
			uint8(h.InvokeId),
			uint8(h.SequenceNumber),
			uint8(h.ProposedWindowSize),
			uint8(h.ServiceAckChoice),
		}
	}
	return result, nil
}

func (ae *ApplicationEntity) handleComplexAckPDU(indication *NPDUIndication) error {
	// TODO: check apdu length
	var serviceAck []byte
	apdu := indication.apdu
	header := ComplexAckHeader{}
	header.Flags.FromByte(apdu[0])
	header.InvokeId = uint(apdu[1])
	if header.Flags.SegmentedRequest {
		header.SequenceNumber = uint(apdu[2])
		header.ProposedWindowSize = uint(apdu[3])
		header.ServiceAckChoice = int(apdu[4])
		serviceAck = apdu[5:]
	} else {
		header.ServiceAckChoice = int(apdu[2])
		serviceAck = apdu[3:]
	}
	transactionId := NewTransactionId(indication.source, header.InvokeId)
	transaction := ae.getClientTransaction(&transactionId)
	if transaction == nil {
		if header.Flags.SegmentedRequest {
			// client state machine IDLE state UnexpectedSegmentInfoReceived
			// TODO issue a N-UNITDATA.request to transmit a AbortPDU
			return nil
		}
		// else: client state machine IDLE state UnexpectedPDU_Received
		// Drop message
		return nil
	}
	transaction.HandleComplexAckPdu(indication, &header, serviceAck)
	return errors.New("not implemented")
}

type SegmentAckHeader struct {
	Flags            SegmentAckFlags
	InvokeId         uint
	SequenceNumber   uint
	ActualWindowSize uint
}

func (h *SegmentAckHeader) GetType() PDUType {
	return SegmentAck
}

func (h *SegmentAckHeader) Marshal() ([]byte, error) {
	typeFlag := uint8(SegmentAck<<4) | h.Flags.ToByte()
	result := []byte{
		typeFlag, uint8(h.InvokeId), uint8(h.SequenceNumber), uint8(h.ActualWindowSize),
	}
	return result, nil
}

func (ae *ApplicationEntity) handleSegmentAckPDU(indication *NPDUIndication) error {
	apdu := indication.apdu
	if len(apdu) != 4 {
		return fmt.Errorf("Segment-Ack-PDU too short")
	}
	header := SegmentAckHeader{}
	header.Flags.FromByte(apdu[0])
	header.InvokeId = uint(apdu[1])
	header.SequenceNumber = uint(apdu[2])
	header.ActualWindowSize = uint(apdu[3])
	transactionId := NewTransactionId(indication.source, header.InvokeId)
	if header.Flags.SentByServer {
		transaction := ae.getClientTransaction(&transactionId)
		if transaction == nil {
			// client state machine IDLE state UnexpectedSegmentInfoReceived
			// TODO issue a N-UNITDATA.request to transmit a AbortPDU
			return nil
		}
		transaction.HandleSegmentAckPdu(&header)
	} else {
		transaction := ae.getServerTransaction(&transactionId)
		if transaction == nil {
			// TODO log error and drop message
			return nil
		}
		event := &ServerNetworkTransactionEvent{
			indication:     indication,
			header:         &header,
			transaction:    transaction,
			serviceRequest: nil,
		}
		transaction.PushEvent(event)
	}
	return errors.New("not implemented")
}

type ErrorHeader struct {
	InvokeId    uint
	ErrorChoice int
}

func (h *ErrorHeader) GetType() PDUType {
	return Error
}

func (h *ErrorHeader) Marshal() ([]byte, error) {
	return []byte{uint8(Error << 4), uint8(h.InvokeId), uint8(h.ErrorChoice)}, nil
}

func (ae *ApplicationEntity) handleErrorPDU(indication *NPDUIndication) error {
	apdu := indication.apdu
	header := ErrorHeader{
		InvokeId:    uint(apdu[1]),
		ErrorChoice: int(apdu[2]),
	}
	errorData := apdu[3:]
	transactionId := NewTransactionId(indication.source, header.InvokeId)
	transaction := ae.getClientTransaction(&transactionId)
	if transaction == nil {
		// client state machine IDLE state UnexpectedPDU_Received
		// Drop message
		return nil
	}

	transaction.HandleErrorPdu(&header, errorData)

	return errors.New("not implemented")
}

type RejectHeader struct {
	InvokeId     uint8
	RejectReason uint8
}

func (h *RejectHeader) GetType() PDUType {
	return Reject
}

func (h *RejectHeader) Marshal() ([]byte, error) {
	return []byte{uint8(Reject << 4), h.InvokeId, h.RejectReason}, nil
}

func (ae *ApplicationEntity) handleRejectPDU(indication *NPDUIndication) error {
	apdu := indication.apdu
	if len(apdu) != 3 {
		return fmt.Errorf("wrong Reject-PDU size")
	}

	invokeId := uint(apdu[1])
	rejectReason := int(apdu[2])

	transactionId := NewTransactionId(indication.source, invokeId)
	transaction := ae.getClientTransaction(&transactionId)
	if transaction == nil {
		// client state machine IDLE state UnexpectedPDU_Received
		// Drop message
		return nil
	}

	transaction.HandleRejectPdu(rejectReason)

	return errors.New("not implemented")
}

type AbortHeader struct {
	SentByServer bool
	InvokeId     uint8
	AbortReason  uint8
}

func (h *AbortHeader) GetType() PDUType {
	return Abort
}

func (h *AbortHeader) Marshal() ([]byte, error) {
	var flags uint8 = 0
	if h.SentByServer {
		flags = 1
	}
	return []byte{uint8(Abort<<4) | flags, h.InvokeId, h.AbortReason}, nil
}

func (ae *ApplicationEntity) handleAbortPDU(indication *NPDUIndication) error {
	apdu := indication.apdu
	if len(apdu) != 3 {
		return fmt.Errorf("wrong Abort-PDU size")
	}

	sentByServer := (apdu[0] & srvMask) != 0
	invokeId := uint(apdu[1])
	reason := int(apdu[2])
	transactionId := NewTransactionId(indication.source, invokeId)
	if sentByServer {
		transaction := ae.getClientTransaction(&transactionId)
		if transaction == nil {
			// client state machine IDLE state UnexpectedPDU_Received
			// Drop message
			return nil
		}
		return errors.New("not implemented")
		//transaction.HandleAbortPdu(reason)
	} else {
		transaction := ae.getServerTransaction(&transactionId)
		if transaction == nil {
			// TODO log error and drop message
			return nil
		}
		header := AbortHeader{
			AbortReason: uint8(reason),
		}
		event := &ServerNetworkTransactionEvent{
			indication:     indication,
			header:         &header,
			transaction:    transaction,
			serviceRequest: nil,
		}
		transaction.PushEvent(event)
	}

	return nil
}
