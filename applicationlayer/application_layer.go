package applicationlayer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/internal/encoding"
	"github.com/REQUEA/bacnet/logger"
	"github.com/REQUEA/bacnet/networklayer"
	"github.com/REQUEA/bacnet/objectmodel"
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

type APDUIndication struct {
	Source        *bacnet.BACnetAddress
	ExpectedReply bool
	InvokeId      uint
	Data          []byte
}

type ConfServResponseType uint

const (
	ResponseConfirm = 0
	ResponseReject  = 1
	ResponseAbort   = 2
)

type ConfServResponse struct {
	Type       ConfServResponseType
	Indication *APDUIndication
}

type ConfServFuture struct {
	c chan *ConfServResponse
}

func (f *ConfServFuture) WaitResponse(ctx context.Context) *ConfServResponse {
	select {
	case result, ok := <-f.c:
		if !ok {
			return nil
		}
		return result
	case <-ctx.Done():
		return nil
	}
}

func (f *ConfServFuture) WaitResponseWithTimeout(d time.Duration) *ConfServResponse {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return f.WaitResponse(ctx)
}

type ServiceHandler interface {
	GetDevice(bacnet.BACnetConfirmedServiceChoice, []byte) *objectmodel.Device
	HandleConfServIndication(*APDUIndication, bacnet.BACnetConfirmedServiceChoice)
	HandleConfServConfirm(*APDUIndication, bacnet.BACnetConfirmedServiceChoice)
	HandleUnconfServIndication(*APDUIndication, bacnet.BACnetUnconfirmedServiceChoice)
	HandleSegmentAckIndication(*APDUIndication, bacnet.BACnetConfirmedServiceChoice)
	HandleRejectIndication(*APDUIndication, bacnet.BACnetConfirmedServiceChoice)
	HandleAbortIndication(*APDUIndication, bacnet.BACnetConfirmedServiceChoice, uint8)
}

type ApplicationEntity struct {
	ServerTransactions      []*ServerTransaction
	ClientTransactions      []*ClientTransaction
	clientTransactionsMutex sync.Mutex
	serverTransactionsMutex sync.Mutex
	serviceLayer            ServiceHandler
	networkEntity           networklayer.NetworkEntity
	serviceRegistery        *ServiceRegistry
	remoteDeviceCache       *objectmodel.RemoteDeviceCache
	clientInvokeId          uint
}

// Compile-time check: ApplicationEntity must implement networklayer.APDUHandler.
var _ networklayer.APDUHandler = (*ApplicationEntity)(nil)

type ServiceRegistry struct {
	confirmedServices   map[bacnet.BACnetConfirmedServiceChoice]ServiceHandler
	unconfirmedServices map[bacnet.BACnetUnconfirmedServiceChoice]ServiceHandler
}

func NewApplicationEntity() *ApplicationEntity {
	return &ApplicationEntity{
		ServerTransactions: make([]*ServerTransaction, 0),
		ClientTransactions: make([]*ClientTransaction, 0),
		remoteDeviceCache:  &objectmodel.RemoteDeviceCache{},
	}
}

func (ae *ApplicationEntity) SetServiceLayer(sl ServiceHandler) {
	ae.serviceLayer = sl
}

func (ae *ApplicationEntity) SetNetworkEntity(ne networklayer.NetworkEntity) {
	ae.networkEntity = ne
}

func (ae *ApplicationEntity) getServerTransaction(id *TransactionId) *ServerTransaction {
	ae.serverTransactionsMutex.Lock()
	defer ae.serverTransactionsMutex.Unlock()
	for _, t := range ae.ServerTransactions {
		if t != nil && t.Id.Equal(id) {
			return t
		}
	}
	return nil
}

func (ae *ApplicationEntity) removeServerTransaction(id *TransactionId) *ServerTransaction {
	ae.serverTransactionsMutex.Lock()
	defer ae.serverTransactionsMutex.Unlock()
	for i := range ae.ServerTransactions {
		if ae.ServerTransactions[i] != nil && ae.ServerTransactions[i].Id.Equal(id) {
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
		if t != nil && t.Id.Equal(id) {
			return t
		}
	}
	return nil
}

func (ae *ApplicationEntity) addClientTransaction(t *ClientTransaction) {
	ae.clientTransactionsMutex.Lock()
	defer ae.clientTransactionsMutex.Unlock()
	for i := range ae.ClientTransactions {
		if ae.ClientTransactions[i] == nil {
			ae.ClientTransactions[i] = t
			return
		}
	}
	ae.ClientTransactions = append(ae.ClientTransactions, t)
}

func (ae *ApplicationEntity) removeClientTransaction(id *TransactionId) *ClientTransaction {
	ae.clientTransactionsMutex.Lock()
	defer ae.clientTransactionsMutex.Unlock()
	for i := range ae.ClientTransactions {
		if ae.ClientTransactions[i] != nil && ae.ClientTransactions[i].Id.Equal(id) {
			result := ae.ClientTransactions[i]
			ae.ClientTransactions[i] = nil
			return result
		}
	}
	return nil
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

type ServerNetworkTransactionEvent struct {
	indication     *networklayer.NPDUIndication
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

type ClientNetworkTransactionEvent struct {
	indication  *networklayer.NPDUIndication
	transaction *ClientTransaction
	header      PDUHeader
	payload     []byte
}

func (e *ClientNetworkTransactionEvent) Exec() {
	switch e.header.GetType() {
	case SimpleAck:
		header, ok := e.header.(*SimpleAckHeader)
		if !ok {
			logger.Error("wrong header type for SimpleAck")
			return
		}
		e.transaction.HandleSimpleAckPdu(e.indication, header)
	case ComplexAck:
		header, ok := e.header.(*ComplexAckHeader)
		if !ok {
			logger.Error("wrong header type for ComplexAck")
			return
		}
		e.transaction.HandleComplexAckPdu(e.indication, header, e.payload)
	case Error:
		header, ok := e.header.(*ErrorHeader)
		if !ok {
			logger.Error("wrong header type for Error")
			return
		}
		e.transaction.HandleErrorPdu(e.indication, header, e.payload)
	case Reject:
		header, ok := e.header.(*RejectHeader)
		if !ok {
			logger.Error("wrong header type for Reject")
			return
		}
		e.transaction.HandleRejectPdu(e.indication, int(header.RejectReason))
	default:
		logger.Trace("Invalid PDU received for a client transaction: ", e.header.GetType().String())
	}
}

func (ae *ApplicationEntity) HandleNUnitDataIndication(indication *networklayer.NPDUIndication) {
	if len(indication.Apdu) < 1 {
		logger.Error("N-UNITDATA.indication handling: empty APDU")
		return
	}
	pduType := PDUType((indication.Apdu[0] & pduTypeMask) >> pduTypeShift)
	switch pduType {
	case SimpleAck:
		// client
		ae.handleSimpleAckPDU(indication)
	case ComplexAck:
		// client
		ae.handleComplexAckPDU(indication)
	case Error:
		// client
		ae.handleErrorPDU(indication)
	case Reject:
		// client
		ae.handleRejectPDU(indication)
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

func (ae *ApplicationEntity) HandleNUnitReportIndication(indication *networklayer.NPDUIndication) {
	// TODO: write implementation
}

func (ae *ApplicationEntity) nextClientInvokeId() uint {
	ae.clientInvokeId++
	return ae.clientInvokeId
}

func (ae *ApplicationEntity) SendConfServRequest(
	serviceChoice bacnet.BACnetConfirmedServiceChoice,
	dest *bacnet.BACnetAddress,
	expectedReply bool,
	priority networklayer.NPDUPriority,
	data []byte,
) *ConfServFuture {
	id := NewTransactionId(dest, ae.nextClientInvokeId())
	transaction := NewClientTransaction(&id)
	transaction.applicationEntity = ae
	transaction.serviceLayer = ae.serviceLayer

	device := ae.remoteDeviceCache.Get(dest)
	if device != nil {
		transaction.maxPduLength = int(device.MaxAPDULength())
		transaction.segmentationSupported = device.SegmentationSupported()
	} else {
		// TODO: use some default values when the remote device is not yet in the cache
	}

	transaction.serviceChoice = serviceChoice
	transaction.requestPdu = data
	transaction.RequestTimer = NewTransactionTimer(defaultClientApduTimeout, transaction.OnRequestTimerFired)
	transaction.SegmentTimer = NewTransactionTimer(defaultClientSegmentTimeout, transaction.OnSegmentTimerFired)

	future := &ConfServFuture{c: make(chan *ConfServResponse, 1)}
	transaction.future = future

	ae.addClientTransaction(transaction)
	transaction.HandleConfServRequest(dest, expectedReply, priority)
	transaction.Start()
	return future
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

func (ae *ApplicationEntity) handleConfirmedServiceRequestPDU(indication *networklayer.NPDUIndication) error {
	if indication.Dest.Mac.IsBroadcast() || indication.Dest.Network == bacnet.BroadcastDNET {
		// Drop
		// TODO: Log?
		return nil
	}
	apdu := indication.Apdu
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
	transactionId := NewTransactionId(indication.Source, header.InvokeId)
	transaction := ae.getServerTransaction(&transactionId)
	if transaction != nil {
		// TODO log or drop?
	}
	tr := NewServerTransaction(&transactionId)
	tr.applicationEntity = ae
	tr.serviceLayer = ae.serviceLayer
	tr.Source = indication.Source
	tr.Dest = indication.Dest

	// Find device from service to populate transaction timers
	device := ae.serviceLayer.GetDevice(header.ServiceChoice, serviceRequest)
	if device == nil {
		return fmt.Errorf("no device found for service choice %d", header.ServiceChoice)
	}
	tr.Device = device

	deviceObject := device.DeviceObject()
	maxSegmentsAcceptedProp := deviceObject.GetProperty(bacnet.MaxSegmentsAccepted)
	if maxSegmentsAcceptedProp == nil {
		return fmt.Errorf("device object without max-segments-accepted property")
	}
	maxSegmentsAcceptedUnsigned, ok := maxSegmentsAcceptedProp.GetValue().(*encoding.Unsigned)
	if !ok {
		return fmt.Errorf("max-segments-accepted value is not an unsigned int")
	}
	tr.MaxSegmentsAccepted = uint(maxSegmentsAcceptedUnsigned.Value())

	apduTimeoutProp := deviceObject.GetProperty(bacnet.ApduTimeout)
	if apduTimeoutProp == nil {
		return fmt.Errorf("device object without apdu-timeout property")
	}
	apduTimeoutUnsigned, ok := apduTimeoutProp.GetValue().(*encoding.Unsigned)
	if !ok {
		return fmt.Errorf("apdu-timeout value is not an unsigned int")
	}
	tr.RequestTimer = NewTransactionTimer(
		time.Duration(apduTimeoutUnsigned.Value())*time.Millisecond,
		tr.OnRequestTimerFired,
	)

	apduSegmentTimeoutProp := deviceObject.GetProperty(bacnet.ApduSegmentTimeout)
	if apduSegmentTimeoutProp == nil {
		return fmt.Errorf("device object without apdu-segment-timeout property")
	}
	apduSegmentTimeoutUnsigned, ok := apduSegmentTimeoutProp.GetValue().(*encoding.Unsigned)
	if !ok {
		return fmt.Errorf("apdu-segment-timeout value is not an unsigned int")
	}
	tr.SegmentTimer = NewTransactionTimer(
		time.Duration(apduSegmentTimeoutUnsigned.Value()*4)*time.Millisecond,
		tr.OnSegmentTimerFired,
	)

	numberOfApduRetriesProp := deviceObject.GetProperty(bacnet.NumberOfApduRetries)
	if numberOfApduRetriesProp == nil {
		return fmt.Errorf("device object without number-of-apdu-retries property")
	}
	numberOfApduRetriesUnsigned, ok := numberOfApduRetriesProp.GetValue().(*encoding.Unsigned)
	if !ok {
		return fmt.Errorf("number-of-apdu-retries value is not an unsigned int")
	}
	tr.NumberOfApduRetries = uint(numberOfApduRetriesUnsigned.Value())

	ae.addServerTransaction(tr)
	tr.Start() // tr is in IDLE state and can start processing events
	event := &ServerNetworkTransactionEvent{
		indication:     indication,
		header:         &header,
		transaction:    tr,
		serviceRequest: serviceRequest,
	}
	tr.PushEvent(event)
	return nil
}

func (ae *ApplicationEntity) handleUnconfirmedServiceRequestPDU(indication *networklayer.NPDUIndication) error {
	apdu := indication.Apdu
	if len(apdu) < 2 {
		logger.Error("Unconfirmed-Service-Request-PDU too short (len: ", len(apdu), ")")
		return nil
	}
	if ae.serviceLayer == nil {
		logger.Error("no service layer configured, dropping unconfirmed request")
		return nil
	}
	serviceChoice := bacnet.BACnetUnconfirmedServiceChoice(apdu[1])
	apduIndication := &APDUIndication{
		Source:        indication.Source,
		ExpectedReply: indication.ExpectedReply,
		Data:          apdu[2:],
	}
	ae.serviceLayer.HandleUnconfServIndication(apduIndication, serviceChoice)
	return nil
}

func (ae *ApplicationEntity) RemoteDeviceCache() *objectmodel.RemoteDeviceCache {
	return ae.remoteDeviceCache
}

// SendIAmRequest sends an unconfirmed IAm service request PDU. data is the pre-encoded IAmRequest payload.
func (ae *ApplicationEntity) SendIAmRequest(dest *bacnet.BACnetAddress, priority networklayer.NPDUPriority, data []byte) error {
	apdu := make([]byte, 0, 2+len(data))
	apdu = append(apdu, byte(UnconfirmedServiceRequest<<pduTypeShift))
	apdu = append(apdu, byte(bacnet.UnconfirmedServiceChoiceIAm))
	apdu = append(apdu, data...)
	return ae.networkEntity.NUnitDataRequest(dest, false, priority, apdu)
}

const (
	// defaultMaxApduLength is the safe fallback when the remote device's max APDU length is unknown.
	defaultMaxApduLength = 480

	// defaultClientApduTimeout is the default timeout for client confirmed-service requests.
	defaultClientApduTimeout = 3 * time.Second

	// defaultClientSegmentTimeout is the default segment timer for client segmented transactions.
	defaultClientSegmentTimeout = 5 * time.Second

	complexAckUnsegHeaderLen = 3 // type+flags, invoke-id, service-ack-choice
	complexAckSegHeaderLen   = 5 // type+flags, invoke-id, seq-no, window-size, service-ack-choice
)

// SendConfServResponse sends a confirmed-service response for an in-progress server transaction.
// data is the service-ack payload (empty for SimpleAck, non-empty for ComplexAck).
func (ae *ApplicationEntity) SendConfServResponse(
	invokeId uint,
	source *bacnet.BACnetAddress,
	serviceChoice bacnet.BACnetConfirmedServiceChoice,
	data []byte,
) error {
	transactionId := NewTransactionId(source, invokeId)
	tr := ae.getServerTransaction(&transactionId)
	if tr == nil {
		return fmt.Errorf("SendConfServResponse: no server transaction for invokeId=%d source=%s", invokeId, source)
	}
	tr.RequestTimer.Stop()

	maxApduLength := defaultMaxApduLength
	device := ae.remoteDeviceCache.Get(source)
	if device != nil && device.MaxAPDULength() > 0 {
		maxApduLength = int(device.MaxAPDULength())
	}

	if len(data) == 0 {
		// SimpleAck
		ack := SimpleAckHeader{
			InvokeId:      invokeId,
			ServiceChoice: int(serviceChoice),
		}
		ackBytes, err := ack.Marshal()
		if err != nil {
			return fmt.Errorf("SendConfServResponse: could not marshal SimpleAck: %w", err)
		}
		err = ae.networkEntity.NUnitDataRequest(source, false, networklayer.NormalPriority, ackBytes)
		ae.removeServerTransaction(&transactionId)
		return err
	}

	if complexAckUnsegHeaderLen+len(data) <= maxApduLength {
		// Unsegmented ComplexAck
		header := ComplexAckHeader{
			Flags:            ComplexAckFlags{SegmentedRequest: false},
			InvokeId:         invokeId,
			ServiceAckChoice: int(serviceChoice),
		}
		headerBytes, err := header.Marshal()
		if err != nil {
			return fmt.Errorf("SendConfServResponse: could not marshal ComplexAck header: %w", err)
		}
		pdu := append(headerBytes, data...)
		err = ae.networkEntity.NUnitDataRequest(source, false, networklayer.NormalPriority, pdu)
		ae.removeServerTransaction(&transactionId)
		return err
	}

	// Segmented ComplexAck
	tr.responsePdu = data
	tr.requestOffset = 0
	tr.maxPduLength = maxApduLength - complexAckSegHeaderLen
	tr.serviceChoice = serviceChoice
	tr.SentAllSegments = false
	tr.InitialSequenceNumber = 0
	tr.SegmentRetryCount = 0
	tr.ActualWindowSize = 1
	segState := &ServerTransactionSegmentedResponseState{transaction: tr}
	tr.state = segState
	segState.sendNextSegments()
	return nil
}

// SendErrorResponse sends a BACnet Error PDU in response to a confirmed-service request.
func (ae *ApplicationEntity) SendErrorResponse(
	invokeId uint,
	source *bacnet.BACnetAddress,
	serviceChoice bacnet.BACnetConfirmedServiceChoice,
	errClass bacnet.ErrorClass,
	errCode bacnet.ErrorCode,
) error {
	transactionId := NewTransactionId(source, invokeId)
	tr := ae.getServerTransaction(&transactionId)
	if tr != nil {
		tr.RequestTimer.Stop()
		ae.removeServerTransaction(&transactionId)
	}
	// Error PDU: type|flags, invoke-id, service-choice, error-class (enum), error-code (enum)
	pdu := []byte{
		uint8(Error << 4),
		uint8(invokeId),
		uint8(serviceChoice),
		(9 << 4) | 1, byte(errClass),
		(9 << 4) | 1, byte(errCode),
	}
	return ae.networkEntity.NUnitDataRequest(source, false, networklayer.NormalPriority, pdu)
}

// SendUnconfirmedService sends a generic unconfirmed service request PDU.
// data is the pre-encoded service payload.
func (ae *ApplicationEntity) SendUnconfirmedService(
	serviceChoice bacnet.BACnetUnconfirmedServiceChoice,
	dest *bacnet.BACnetAddress,
	priority networklayer.NPDUPriority,
	data []byte,
) error {
	apdu := make([]byte, 0, 2+len(data))
	apdu = append(apdu, byte(UnconfirmedServiceRequest<<pduTypeShift))
	apdu = append(apdu, byte(serviceChoice))
	apdu = append(apdu, data...)
	return ae.networkEntity.NUnitDataRequest(dest, false, priority, apdu)
}

// SendWhoIsRequest sends an unconfirmed WhoIs service request PDU. data is the pre-encoded WhoIsRequest payload.
func (ae *ApplicationEntity) SendWhoIsRequest(dest *bacnet.BACnetAddress, priority networklayer.NPDUPriority, data []byte) error {
	apdu := make([]byte, 0, 2+len(data))
	apdu = append(apdu, byte(UnconfirmedServiceRequest<<pduTypeShift))
	apdu = append(apdu, byte(bacnet.UnconfirmedServiceChoiceWhoIs))
	apdu = append(apdu, data...)
	return ae.networkEntity.NUnitDataRequest(dest, false, priority, apdu)
}

type SimpleAckHeader struct {
	InvokeId      uint
	ServiceChoice int
}

func (h *SimpleAckHeader) GetType() PDUType {
	return SimpleAck
}

func (h *SimpleAckHeader) Marshal() ([]byte, error) {
	return []byte{
		uint8(SimpleAck << 4),
		uint8(h.InvokeId),
		uint8(h.ServiceChoice),
	}, nil
}

func (ae *ApplicationEntity) handleSimpleAckPDU(indication *networklayer.NPDUIndication) error {
	apdu := indication.Apdu
	if len(apdu) < 3 {
		return fmt.Errorf("SimpleACK-PDU too short (len: %d)", len(apdu))
	}
	header := SimpleAckHeader{
		InvokeId:      uint(apdu[1]),
		ServiceChoice: int(apdu[2]),
	}

	transactionId := NewTransactionId(indication.Source, header.InvokeId)
	transaction := ae.getClientTransaction(&transactionId)
	if transaction == nil {
		// client state machine IDLE state UnexpectedPDU_Received
		// Drop message
		return nil
	}
	event := &ClientNetworkTransactionEvent{
		indication:  indication,
		header:      &header,
		transaction: transaction,
		payload:     nil,
	}
	transaction.PushEvent(event)
	return nil
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
			uint8(h.SequenceNumber),
			uint8(h.ProposedWindowSize),
			uint8(h.ServiceAckChoice),
		}
	} else {
		result = []byte{
			typeFlag,
			uint8(h.InvokeId),
			uint8(h.ServiceAckChoice),
		}
	}
	return result, nil
}

func (ae *ApplicationEntity) handleComplexAckPDU(indication *networklayer.NPDUIndication) error {
	// TODO: check apdu length
	var serviceAck []byte
	apdu := indication.Apdu
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
	transactionId := NewTransactionId(indication.Source, header.InvokeId)
	transaction := ae.getClientTransaction(&transactionId)
	if transaction == nil {
		// UnexpectedPDU_Received
		// Drop message
		return nil
	}
	event := &ClientNetworkTransactionEvent{
		indication:  indication,
		header:      &header,
		transaction: transaction,
		payload:     serviceAck,
	}
	transaction.PushEvent(event)
	return nil
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

func (ae *ApplicationEntity) handleSegmentAckPDU(indication *networklayer.NPDUIndication) error {
	apdu := indication.Apdu
	if len(apdu) != 4 {
		return fmt.Errorf("Segment-Ack-PDU too short")
	}
	header := SegmentAckHeader{}
	header.Flags.FromByte(apdu[0])
	header.InvokeId = uint(apdu[1])
	header.SequenceNumber = uint(apdu[2])
	header.ActualWindowSize = uint(apdu[3])
	transactionId := NewTransactionId(indication.Source, header.InvokeId)
	if header.Flags.SentByServer {
		transaction := ae.getClientTransaction(&transactionId)
		if transaction == nil {
			return nil
		}
		event := &ClientNetworkTransactionEvent{
			indication:  indication,
			header:      &header,
			transaction: transaction,
			payload:     nil,
		}
		transaction.PushEvent(event)
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
	return nil
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

func (ae *ApplicationEntity) handleErrorPDU(indication *networklayer.NPDUIndication) error {
	apdu := indication.Apdu
	header := ErrorHeader{
		InvokeId:    uint(apdu[1]),
		ErrorChoice: int(apdu[2]),
	}
	errorData := apdu[3:]
	transactionId := NewTransactionId(indication.Source, header.InvokeId)
	transaction := ae.getClientTransaction(&transactionId)
	if transaction == nil {
		// client state machine IDLE state UnexpectedPDU_Received
		// Drop message
		return nil
	}

	event := &ClientNetworkTransactionEvent{
		indication:  indication,
		header:      &header,
		transaction: transaction,
		payload:     errorData,
	}
	transaction.PushEvent(event)
	return nil
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

func (ae *ApplicationEntity) handleRejectPDU(indication *networklayer.NPDUIndication) error {
	apdu := indication.Apdu
	if len(apdu) != 3 {
		return fmt.Errorf("wrong Reject-PDU size")
	}

	invokeId := uint(apdu[1])
	rejectReason := int(apdu[2])

	transactionId := NewTransactionId(indication.Source, invokeId)
	transaction := ae.getClientTransaction(&transactionId)
	if transaction == nil {
		// client state machine IDLE state UnexpectedPDU_Received
		// Drop message
		return nil
	}

	event := &ClientNetworkTransactionEvent{
		indication:  indication,
		header:      &RejectHeader{InvokeId: uint8(invokeId), RejectReason: uint8(rejectReason)},
		transaction: transaction,
		payload:     nil,
	}
	transaction.PushEvent(event)

	return nil
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

func (ae *ApplicationEntity) handleAbortPDU(indication *networklayer.NPDUIndication) error {
	apdu := indication.Apdu
	if len(apdu) != 3 {
		return fmt.Errorf("wrong Abort-PDU size")
	}

	sentByServer := (apdu[0] & srvMask) != 0
	invokeId := uint(apdu[1])
	reason := int(apdu[2])
	transactionId := NewTransactionId(indication.Source, invokeId)
	if sentByServer {
		transaction := ae.getClientTransaction(&transactionId)
		if transaction == nil {
			// client state machine IDLE state UnexpectedPDU_Received
			// Drop message
			return nil
		}
		event := &ClientNetworkTransactionEvent{
			indication: indication,
			header: &AbortHeader{
				SentByServer: sentByServer,
				InvokeId:     uint8(invokeId),
				AbortReason:  uint8(reason),
			},
			transaction: transaction,
			payload:     nil,
		}
		transaction.PushEvent(event)
		return nil
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
