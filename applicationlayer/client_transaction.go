package applicationlayer

import (
	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/logger"
	"github.com/REQUEA/bacnet/networklayer"
)

type ClientTransactionState interface {
	HandleSimpleAckPdu(*networklayer.NPDUIndication, *SimpleAckHeader)
	HandleComplexAckPdu(*networklayer.NPDUIndication, *ComplexAckHeader, []byte)
	HandleErrorPdu(*networklayer.NPDUIndication, *ErrorHeader, []byte)
	HandleRejectPdu(*networklayer.NPDUIndication, int)
	HandleSegmentAckPdu(*networklayer.NPDUIndication, *SegmentAckHeader)
	HandleAbortPdu(int)
	HandleConfServRequest(*bacnet.BACnetAddress, bool, networklayer.NPDUPriority)
}

type ClientTransaction struct {
	Transaction
	state                 ClientTransactionState
	requestPdu            []byte
	requestOffset         int
	maxPduLength          int
	segmentationSupported bool
	serviceChoice         bacnet.BACnetConfirmedServiceChoice
	responsePdu           []byte
	future                *ConfServFuture
	lastSentSeqNum        uint
	expectedReply         bool

	// Response consistency state (set from first response segment)
	initialResponseServiceChoice bacnet.BACnetConfirmedServiceChoice
}

func NewClientTransaction(id *TransactionID) *ClientTransaction {
	result := &ClientTransaction{
		Transaction: Transaction{
			ID:     id,
			events: make(chan TransactionEvent, 16),
		},
	}
	result.state = &ClientTransactionIdleState{
		transaction: result,
	}
	return result
}

func (t *ClientTransaction) HandleSimpleAckPdu(indication *networklayer.NPDUIndication, header *SimpleAckHeader) {
	if t.state != nil {
		t.state.HandleSimpleAckPdu(indication, header)
	}
}

func (t *ClientTransaction) HandleComplexAckPdu(
	indication *networklayer.NPDUIndication,
	header *ComplexAckHeader,
	serviceAck []byte,
) {
	if t.state != nil {
		t.state.HandleComplexAckPdu(indication, header, serviceAck)
	}
}

func (t *ClientTransaction) HandleErrorPdu(indication *networklayer.NPDUIndication, header *ErrorHeader, errorData []byte) {
	if t.state != nil {
		t.state.HandleErrorPdu(indication, header, errorData)
	}
}

func (t *ClientTransaction) HandleRejectPdu(indication *networklayer.NPDUIndication, reason int) {
	if t.state != nil {
		t.state.HandleRejectPdu(indication, reason)
	}
}

func (t *ClientTransaction) HandleSegmentAckPdu(indication *networklayer.NPDUIndication, header *SegmentAckHeader) {
	if t.state != nil {
		t.state.HandleSegmentAckPdu(indication, header)
	}
}

func (t *ClientTransaction) HandleAbortPdu(reason int) {
	if t.state != nil {
		t.state.HandleAbortPdu(reason)
	}
}

func (t *ClientTransaction) HandleConfServRequest(
	dest *bacnet.BACnetAddress,
	expectedReply bool,
	priority networklayer.NPDUPriority,
) {
	if t.state != nil {
		t.state.HandleConfServRequest(dest, expectedReply, priority)
	}
}

func (t *ClientTransaction) resolveFuture(resp *ConfServResponse) {
	if t.future != nil {
		t.future.c <- resp
		close(t.future.c)
		t.future = nil
	}
}

// fillWindow sends up to ActualWindowSize ConfirmedServiceRequest segments starting at
// requestOffset. Does NOT advance requestOffset or InitialSequenceNumber — those are updated
// only when a positive SegmentAck is received — so retransmitting the current window is
// achieved by calling fillWindow again. Sets SentAllSegments=true and records lastSentSeqNum
// when the final segment is sent.
func (t *ClientTransaction) fillWindow() {
	seqNum := t.InitialSequenceNumber
	offset := t.requestOffset
	maxApduLength := t.applicationEntity.networkEntity.GetMaxPDULength(t.Dest.Network)
	maxResp := MaxRespFromPduLen(int(maxApduLength)) //nolint:gosec
	for i := uint(0); i < t.ActualWindowSize; i++ {
		end := offset + t.maxPduLength
		if end > len(t.requestPdu) {
			end = len(t.requestPdu)
		}
		moreSegments := end < len(t.requestPdu)
		segData := t.requestPdu[offset:end]
		header := ConfirmedServiceRequestHeader{
			Flags: ConfServFlags{
				SegmentedRequest:          true,
				MoreSegments:              moreSegments,
				SegmentedResponseAccepted: true,
			},
			MaxSegs:            0b111,
			MaxResp:            maxResp,
			SequenceNumber:     seqNum,
			ProposedWindowSize: t.ProposedWindowSize,
			InvokeID:           t.ID.InvokeID,
			ServiceChoice:      t.serviceChoice,
		}
		headerBytes, err := header.Marshal()
		if err != nil {
			logger.Error("could not marshal ConfServ header: ", err)
			return
		}
		pdu := append(headerBytes, segData...)
		err = t.applicationEntity.networkEntity.NUnitDataRequest(t.Dest, t.expectedReply, t.Priority, pdu)
		if err != nil {
			logger.Error("could not send ConfirmedServiceRequest segment: ", err)
		}
		if !moreSegments {
			t.SentAllSegments = true
			t.lastSentSeqNum = seqNum
			break
		}
		seqNum = (seqNum + 1) % 256
		offset = end
	}
	t.SegmentTimer.Reset()
	t.SegmentTimer.Start()
}

func (t *ClientTransaction) OnRequestTimerFired() {
	t.events <- &clientRequestTimerEvent{transaction: t}
}

func (t *ClientTransaction) OnSegmentTimerFired() {
	t.events <- &clientSegmentTimerEvent{transaction: t}
}

type clientRequestTimerEvent struct {
	transaction *ClientTransaction
}

func (e *clientRequestTimerEvent) Exec() {
	tr := e.transaction
	tr.resolveFuture(&ConfServResponse{Type: ResponseAbort, Indication: nil})
	tr.applicationEntity.removeClientTransaction(tr.ID)
}

type clientSegmentTimerEvent struct {
	transaction *ClientTransaction
}

func (e *clientSegmentTimerEvent) Exec() {
	tr := e.transaction
	tr.resolveFuture(&ConfServResponse{Type: ResponseAbort, Indication: nil})
	tr.applicationEntity.removeClientTransaction(tr.ID)
}

////////////////////////////////////////////////////////////////////////////////
// IDLE State
////////////////////////////////////////////////////////////////////////////////

type ClientTransactionIdleState struct {
	transaction *ClientTransaction
}

func (s *ClientTransactionIdleState) HandleSimpleAckPdu(*networklayer.NPDUIndication, *SimpleAckHeader) {
	// never called
}

func (s *ClientTransactionIdleState) HandleComplexAckPdu(
	indication *networklayer.NPDUIndication,
	header *ComplexAckHeader,
	_ []byte,
) {
	if header.Flags.SegmentedRequest {
		tr := s.transaction
		// UnexpectedSegmentInfoReceived
		abortHeader := &AbortHeader{
			SentByServer: false,
			InvokeID:     uint8(header.InvokeID), //nolint:gosec
			AbortReason:  uint8(bacnet.AbortInvalidApduInThisState),
		}
		abortBytes, err := abortHeader.Marshal()
		if err != nil {
			logger.Error("could not marshal abort header: ", err)
			tr.applicationEntity.removeClientTransaction(tr.ID)
			return
		}
		err = tr.applicationEntity.networkEntity.NUnitDataRequest(
			indication.Source,
			false,
			networklayer.NormalPriority,
			abortBytes,
		)
		if err != nil {
			logger.Error("could not send Abort PDU: ", err)
		}
		return
	}
}

func (s *ClientTransactionIdleState) HandleErrorPdu(*networklayer.NPDUIndication, *ErrorHeader, []byte) {
	// never called
}

func (s *ClientTransactionIdleState) HandleRejectPdu(*networklayer.NPDUIndication, int) {
	// never called
}

func (s *ClientTransactionIdleState) HandleSegmentAckPdu(_ *networklayer.NPDUIndication, header *SegmentAckHeader) {
	if header.Flags.SentByServer { //nolint:staticcheck // TODO: issue a N-UNITDATA.request to transmit a AbortPDU
	}
}
func (s *ClientTransactionIdleState) HandleAbortPdu(int) {
	// never called
}

func MaxRespFromPduLen(pduLen int) int {
	if pduLen <= 50 {
		return 0
	} else if pduLen <= 128 {
		return 1
	} else if pduLen <= 206 {
		return 2
	} else if pduLen <= 480 {
		return 3
	} else if pduLen <= 1024 {
		return 4
	} else if pduLen <= 1476 {
		return 5
	}
	return 0
}

func (s *ClientTransactionIdleState) HandleConfServRequest(
	dest *bacnet.BACnetAddress,
	expectedReply bool,
	priority networklayer.NPDUPriority,
) {
	tr := s.transaction
	maxApduLength := tr.applicationEntity.networkEntity.GetMaxPDULength(dest.Network)
	if len(tr.requestPdu) > int(maxApduLength) { //nolint:gosec
		// SendConfirmedSegmented
		tr.SentAllSegments = false
		tr.RetryCount = 0
		tr.SegmentRetryCount = 0
		tr.InitialSequenceNumber = 0
		tr.requestOffset = 0
		tr.ProposedWindowSize = 5
		tr.ActualWindowSize = 1
		tr.Dest = dest
		tr.Priority = priority
		tr.expectedReply = expectedReply
		tr.fillWindow()
		tr.state = &ClientTransactionSegmentedRequestState{tr}
	} else {
		// SendConfirmedUnsegmented
		maxResp := MaxRespFromPduLen(int(maxApduLength)) //nolint:gosec
		header := ConfirmedServiceRequestHeader{
			Flags: ConfServFlags{
				SegmentedRequest:          false,
				MoreSegments:              false,
				SegmentedResponseAccepted: true,
			},
			MaxSegs:       0b111,
			MaxResp:       maxResp,
			InvokeID:      tr.ID.InvokeID,
			ServiceChoice: tr.serviceChoice,
		}
		confReqBytes, err := header.Marshal()
		if err != nil {
			logger.Error("could not marshal ConfServ header: ", err)
			return
		}
		confReqBytes = append(confReqBytes, tr.requestPdu...)
		err = tr.applicationEntity.networkEntity.NUnitDataRequest(dest, expectedReply, priority, confReqBytes)
		if err != nil {
			logger.Error("could not send ConfirmedServiceRequest: ", err)
		}
		tr.RequestTimer.Start()
		tr.state = &ClientTransactionAwaitConfirmationState{tr}
	}
}

////////////////////////////////////////////////////////////////////////////////
// AWAIT_CONFIRMATION State
////////////////////////////////////////////////////////////////////////////////

type ClientTransactionAwaitConfirmationState struct {
	transaction *ClientTransaction
}

func (s *ClientTransactionAwaitConfirmationState) HandleSimpleAckPdu(
	indication *networklayer.NPDUIndication,
	header *SimpleAckHeader,
) {
	// SimpleACK_Received
	tr := s.transaction
	tr.RequestTimer.Stop()
	serviceChoice := bacnet.BACnetConfirmedServiceChoice(header.ServiceChoice) //nolint:gosec
	apduInd := &APDUIndication{
		Source:        indication.Source,
		ExpectedReply: false,
		Data:          nil,
	}
	tr.applicationEntity.serviceLayer.HandleConfServConfirm(apduInd, serviceChoice)
	tr.resolveFuture(&ConfServResponse{Type: ResponseConfirm, Indication: apduInd})
	tr.applicationEntity.removeClientTransaction(tr.ID)
}

func (s *ClientTransactionAwaitConfirmationState) HandleComplexAckPdu(
	indication *networklayer.NPDUIndication,
	header *ComplexAckHeader,
	serviceAck []byte,
) {
	tr := s.transaction
	if header.Flags.SegmentedRequest {
		if header.SequenceNumber == 0 {
			// SegmentedComplexACK_Received: record initial response parameters for consistency checks
			tr.initialResponseServiceChoice = bacnet.BACnetConfirmedServiceChoice(header.ServiceAckChoice) //nolint:gosec

			tr.responsePdu = append(tr.responsePdu, serviceAck...)
			tr.RequestTimer.Stop()
			// TODO: refine the calculation ActualWindowSize ('based [...] and on local conditions')
			tr.ActualWindowSize = min(tr.MaxSegmentsAccepted, header.ProposedWindowSize)
			segmentAckHeader := &SegmentAckHeader{
				Flags:            SegmentAckFlags{NegativeAck: false, SentByServer: false},
				InvokeID:         tr.ID.InvokeID,
				SequenceNumber:   header.SequenceNumber,
				ActualWindowSize: tr.ActualWindowSize,
			}
			segmentAckBytes, err := segmentAckHeader.Marshal()
			if err != nil {
				logger.Error("could not marshal segment-ack header: ", err)
			} else {
				err = tr.applicationEntity.networkEntity.NUnitDataRequest(
					indication.Source, false, networklayer.NormalPriority, segmentAckBytes)
				if err != nil {
					logger.Error("could not send SegmentAck PDU: ", err)
				}
			}
			tr.SegmentTimer.Start()
			tr.LastSequenceNumber = 0
			tr.InitialSequenceNumber = 0
			tr.DuplicateCount = 0
			tr.state = &ClientTransactionSegmentedConfState{tr}
		}
	} else {
		// UnsegmentedComplexACK_Received
		tr.RequestTimer.Stop()
		tr.responsePdu = serviceAck
		serviceChoice := bacnet.BACnetConfirmedServiceChoice(header.ServiceAckChoice) //nolint:gosec
		apduInd := &APDUIndication{
			Source:        indication.Source,
			ExpectedReply: false,
			Data:          serviceAck,
		}
		tr.applicationEntity.serviceLayer.HandleConfServConfirm(apduInd, serviceChoice)
		tr.resolveFuture(&ConfServResponse{Type: ResponseConfirm, Indication: apduInd})
		tr.applicationEntity.removeClientTransaction(tr.ID)
	}
}

func (s *ClientTransactionAwaitConfirmationState) HandleErrorPdu(indication *networklayer.NPDUIndication, _ *ErrorHeader, errorData []byte) {
	// ErrorPDU_Received
	tr := s.transaction
	tr.RequestTimer.Stop()
	apduInd := &APDUIndication{
		Source:        indication.Source,
		ExpectedReply: false,
		Data:          errorData,
	}
	tr.resolveFuture(&ConfServResponse{Type: ResponseReject, Indication: apduInd})
	tr.applicationEntity.removeClientTransaction(tr.ID)
}

func (s *ClientTransactionAwaitConfirmationState) HandleRejectPdu(indication *networklayer.NPDUIndication, _ int) {
	// RejectPDU_Received
	tr := s.transaction
	tr.RequestTimer.Stop()
	apduInd := &APDUIndication{Source: indication.Source, ExpectedReply: false}
	tr.resolveFuture(&ConfServResponse{Type: ResponseAbort, Indication: apduInd})
	tr.applicationEntity.removeClientTransaction(tr.ID)
}

func (s *ClientTransactionAwaitConfirmationState) HandleSegmentAckPdu(*networklayer.NPDUIndication, *SegmentAckHeader) {
	// SegmentACK_Received
	// Drop PDU
}

func (s *ClientTransactionAwaitConfirmationState) HandleAbortPdu(_ int) {
	// AbortPDU_Received
	tr := s.transaction
	tr.RequestTimer.Stop()
	tr.resolveFuture(&ConfServResponse{Type: ResponseAbort, Indication: nil})
	tr.applicationEntity.removeClientTransaction(tr.ID)
}

func (s *ClientTransactionAwaitConfirmationState) HandleConfServRequest(
	_ *bacnet.BACnetAddress,
	_ bool,
	_ networklayer.NPDUPriority,
) {
}

////////////////////////////////////////////////////////////////////////////////
// SEGMENTED_REQUEST State
////////////////////////////////////////////////////////////////////////////////

type ClientTransactionSegmentedRequestState struct {
	transaction *ClientTransaction
}

func (s *ClientTransactionSegmentedRequestState) HandleSimpleAckPdu(
	indication *networklayer.NPDUIndication,
	header *SimpleAckHeader,
) {
	tr := s.transaction
	if tr.SentAllSegments { //nolint:dupl
		// SimpleACK_Received
		tr.SegmentTimer.Stop()
		serviceChoice := bacnet.BACnetConfirmedServiceChoice(header.ServiceChoice) //nolint:gosec
		apduInd := &APDUIndication{
			Source:        indication.Source,
			ExpectedReply: false,
			Data:          nil,
		}
		tr.applicationEntity.serviceLayer.HandleConfServConfirm(apduInd, serviceChoice)
		tr.resolveFuture(&ConfServResponse{Type: ResponseConfirm, Indication: apduInd})
		tr.applicationEntity.removeClientTransaction(tr.ID)
	} else {
		// UnexpectedPDU_Received
		// Stop SegmentTimer
		tr.SegmentTimer.Stop()
		// Issue N-UNITDATA.request to send a AbortPDU
		header := &AbortHeader{
			SentByServer: false,
			InvokeID:     uint8(tr.ID.InvokeID), //nolint:gosec
			AbortReason:  uint8(bacnet.AbortInvalidApduInThisState),
		}
		abortBytes, err := header.Marshal()
		if err != nil {
			logger.Error("could not marshal abort header: ", err)
		} else {
			err = tr.applicationEntity.networkEntity.NUnitDataRequest(
				indication.Source, false, networklayer.NormalPriority, abortBytes)
			if err != nil {
				logger.Error("could not send Abort PDU: ", err)
			}
		}
		// TODO: issue a ABORT.indication to the local application program
		// Discard transaction
		tr.applicationEntity.removeClientTransaction(tr.ID)
	}
}
func (s *ClientTransactionSegmentedRequestState) HandleComplexAckPdu(
	indication *networklayer.NPDUIndication,
	header *ComplexAckHeader,
	serviceAck []byte,
) {
	tr := s.transaction
	if header.Flags.SegmentedRequest {
		if header.SequenceNumber == 0 {
			if tr.SentAllSegments {
				// SegmentedComplexACK_Received
				tr.responsePdu = append(tr.responsePdu, serviceAck...)
				tr.SegmentTimer.Stop()
				// Compute ActualWindowSize
				// TODO: improve calculation
				tr.ActualWindowSize = min(tr.MaxSegmentsAccepted, header.ProposedWindowSize)
				// Issue N-UNITDATA.request to send a SegmentACK PDU
				segmentAckHeader := &SegmentAckHeader{
					Flags: SegmentAckFlags{
						NegativeAck:  false,
						SentByServer: false,
					},
					InvokeID:         tr.ID.InvokeID,
					SequenceNumber:   header.SequenceNumber,
					ActualWindowSize: tr.ActualWindowSize,
				}
				segmentAckBytes, err := segmentAckHeader.Marshal()
				if err != nil {
					logger.Error("could not marshal segment ack header: ", err)
				} else {
					err = tr.applicationEntity.networkEntity.NUnitDataRequest(
						indication.Source, false, networklayer.NormalPriority, segmentAckBytes)
					if err != nil {
						logger.Error("could not send SegmentACK PDU: ", err)
					}
				}
				tr.SegmentTimer.Start()
				tr.LastSequenceNumber = 0
				tr.InitialSequenceNumber = 0
				tr.DuplicateCount = 0
				tr.state = &ClientTransactionSegmentedConfState{tr}
			}
		} else {
			// UnexpectedPDU_Received
			tr.SegmentTimer.Stop()
			// Issue N-UNITDATA.request to send a AbortPDU
			abortHeader := &AbortHeader{
				SentByServer: false,
				InvokeID:     uint8(tr.ID.InvokeID), //nolint:gosec
				AbortReason:  uint8(bacnet.AbortInvalidApduInThisState),
			}
			abortBytes, err := abortHeader.Marshal()
			if err != nil {
				logger.Error("could not marshal abort header: ", err)
			} else {
				err = tr.applicationEntity.networkEntity.NUnitDataRequest(
					indication.Source, false, networklayer.NormalPriority, abortBytes)
				if err != nil {
					logger.Error("could not send Abort PDU: ", err)
				}
			}
			// TODO: issue a ABORT.indication to the local application program
			// Discard transaction
			tr.applicationEntity.removeClientTransaction(tr.ID)
		}
	} else {
		if tr.SentAllSegments { //nolint:dupl
			// UnsegmentedComplexACK_Received
			tr.SegmentTimer.Stop()
			serviceChoice := bacnet.BACnetConfirmedServiceChoice(header.ServiceAckChoice) //nolint:gosec
			apduInd := &APDUIndication{
				Source:        indication.Source,
				ExpectedReply: false,
				Data:          serviceAck,
			}
			tr.applicationEntity.serviceLayer.HandleConfServConfirm(apduInd, serviceChoice)
			tr.resolveFuture(&ConfServResponse{Type: ResponseConfirm, Indication: apduInd})
			tr.applicationEntity.removeClientTransaction(tr.ID)
		} else {
			// UnexpectedPDU_Received
			tr.SegmentTimer.Stop()
			// Issue N-UNITDATA.request to send a AbortPDU
			abortHeader := &AbortHeader{
				SentByServer: false,
				InvokeID:     uint8(tr.ID.InvokeID), //nolint:gosec
				AbortReason:  uint8(bacnet.AbortInvalidApduInThisState),
			}
			abortBytes, err := abortHeader.Marshal()
			if err != nil {
				logger.Error("could not marshal abort header: ", err)
			} else {
				err = tr.applicationEntity.networkEntity.NUnitDataRequest(
					indication.Source, false, networklayer.NormalPriority, abortBytes)
				if err != nil {
					logger.Error("could not send Abort PDU: ", err)
				}
			}
			// TODO: issue a ABORT.indication to the local application program
			tr.applicationEntity.removeClientTransaction(tr.ID)
		}
	}
}
func (s *ClientTransactionSegmentedRequestState) HandleErrorPdu(indication *networklayer.NPDUIndication, _ *ErrorHeader, _ []byte) {
	tr := s.transaction
	if tr.SentAllSegments {
		// ErrorPDU_Received
		tr.SegmentTimer.Stop()
		// TODO: issue CONF_SERV.confirm(-) to the local application
		// Discard transaction
		tr.applicationEntity.removeClientTransaction(tr.ID)
	} else {
		// UnexpectedPDU_Received
		tr.SegmentTimer.Stop()
		// Issue N-UNITDATA.request to send a AbortPDU
		abortHeader := &AbortHeader{
			SentByServer: false,
			InvokeID:     uint8(tr.ID.InvokeID), //nolint:gosec
			AbortReason:  uint8(bacnet.AbortInvalidApduInThisState),
		}
		abortBytes, err := abortHeader.Marshal()
		if err != nil {
			logger.Error("could not marshal abort header: ", err)
		} else {
			err = tr.applicationEntity.networkEntity.NUnitDataRequest(
				indication.Source, false, networklayer.NormalPriority, abortBytes)
			if err != nil {
				logger.Error("could not send Abort PDU: ", err)
			}
		}
		// TODO: issue a ABORT.indication to the local application program
		// Discard transaction
		tr.applicationEntity.removeClientTransaction(tr.ID)
	}
}
func (s *ClientTransactionSegmentedRequestState) HandleRejectPdu(indication *networklayer.NPDUIndication, _ int) {
	// RejectPDU_Received
	tr := s.transaction
	tr.SegmentTimer.Stop()
	apduInd := &APDUIndication{Source: indication.Source, ExpectedReply: false}
	tr.resolveFuture(&ConfServResponse{Type: ResponseAbort, Indication: apduInd})
	tr.applicationEntity.removeClientTransaction(tr.ID)
}
func (s *ClientTransactionSegmentedRequestState) HandleSegmentAckPdu(_ *networklayer.NPDUIndication, header *SegmentAckHeader) {
	tr := s.transaction
	if tr.InWindow(header.SequenceNumber, tr.InitialSequenceNumber) {
		if tr.SentAllSegments && header.SequenceNumber == tr.lastSentSeqNum {
			// FinalACK_Received
			tr.SegmentTimer.Stop()
			tr.RequestTimer.Start()
			tr.state = &ClientTransactionAwaitConfirmationState{tr}
		} else {
			// NewACK_Received
			nackedSegs := int((header.SequenceNumber-tr.InitialSequenceNumber+256)%256) + 1 //nolint:gosec
			tr.requestOffset += nackedSegs * tr.maxPduLength
			if tr.requestOffset > len(tr.requestPdu) {
				tr.requestOffset = len(tr.requestPdu)
			}
			tr.InitialSequenceNumber = (header.SequenceNumber + 1) % 256
			tr.ActualWindowSize = header.ActualWindowSize
			tr.SegmentRetryCount = 0
			tr.fillWindow()
		}
	} else {
		// DuplicateACK_Received: retransmit current window
		tr.fillWindow()
	}
}
func (s *ClientTransactionSegmentedRequestState) HandleAbortPdu(_ int) {
	// AbortPDU_Received
	tr := s.transaction
	tr.SegmentTimer.Stop()
	tr.resolveFuture(&ConfServResponse{Type: ResponseAbort, Indication: nil})
	tr.applicationEntity.removeClientTransaction(tr.ID)
}

func (s *ClientTransactionSegmentedRequestState) HandleConfServRequest(
	_ *bacnet.BACnetAddress,
	_ bool,
	_ networklayer.NPDUPriority,
) {
}

////////////////////////////////////////////////////////////////////////////////
// SEGMENTED_CONF State
////////////////////////////////////////////////////////////////////////////////

type ClientTransactionSegmentedConfState struct {
	transaction *ClientTransaction
}

func (s *ClientTransactionSegmentedConfState) HandleSimpleAckPdu(
	indication *networklayer.NPDUIndication,
	_ *SimpleAckHeader,
) {
	// UnexpectedPDU_Received
	tr := s.transaction
	tr.SegmentTimer.Stop()
	// Issue a N-UNITDATA.request to send Abort PDU
	abortHeader := &AbortHeader{
		SentByServer: false,
		InvokeID:     uint8(tr.ID.InvokeID), //nolint:gosec
		AbortReason:  uint8(bacnet.AbortInvalidApduInThisState),
	}
	abortBytes, err := abortHeader.Marshal()
	if err != nil {
		logger.Error("could not marshal abort header: ", err)
	} else {
		err = tr.applicationEntity.networkEntity.NUnitDataRequest(
			indication.Source, false, networklayer.NormalPriority, abortBytes)
		if err != nil {
			logger.Error("could not send Abort PDU: ", err)
		}
	}
	// TODO: send ABORT.indication to user layer (InvalidApduInThisState)
	// Discard transaction
	tr.applicationEntity.removeClientTransaction(tr.ID)
}

func (s *ClientTransactionSegmentedConfState) HandleComplexAckPdu(
	indication *networklayer.NPDUIndication,
	header *ComplexAckHeader,
	serviceAck []byte,
) {
	tr := s.transaction
	if header.Flags.SegmentedRequest {
		// NewSegmentReceived_InconsistentAttributes: abort if service choice changed.
		if bacnet.BACnetConfirmedServiceChoice(header.ServiceAckChoice) != tr.initialResponseServiceChoice { //nolint:gosec
			tr.SegmentTimer.Stop()
			abortHeader := &AbortHeader{
				SentByServer: false,
				InvokeID:     uint8(tr.ID.InvokeID), //nolint:gosec
				AbortReason:  uint8(bacnet.AbortInvalidApduInThisState),
			}
			abortBytes, err := abortHeader.Marshal()
			if err != nil {
				logger.Error("could not marshal abort header: ", err)
			} else {
				err = tr.applicationEntity.networkEntity.NUnitDataRequest(
					indication.Source, false, networklayer.NormalPriority, abortBytes)
				if err != nil {
					logger.Error("could not send Abort PDU: ", err)
				}
			}
			tr.resolveFuture(&ConfServResponse{Type: ResponseAbort, Indication: nil})
			tr.applicationEntity.removeClientTransaction(tr.ID)
			return
		}
		if header.SequenceNumber == (tr.LastSequenceNumber+1)%256 {
			// TODO: support NewSegmentReceived_NoSpace
			if header.Flags.MoreSegments {
				if header.SequenceNumber == (tr.InitialSequenceNumber+tr.ActualWindowSize)%256 {
					// LastSegmentOfGroup_Received
					tr.responsePdu = append(tr.responsePdu, serviceAck...)
					tr.LastSequenceNumber = (tr.LastSequenceNumber + 1) % 256
					tr.InitialSequenceNumber = tr.LastSequenceNumber
					tr.DuplicateCount = 0
					// Issue N-UNITDATA.request to send SegmentACK PDU
					segmentAckHeader := &SegmentAckHeader{
						Flags:            SegmentAckFlags{NegativeAck: false, SentByServer: false},
						InvokeID:         tr.ID.InvokeID,
						SequenceNumber:   header.SequenceNumber,
						ActualWindowSize: tr.ActualWindowSize,
					}
					segmentAckBytes, err := segmentAckHeader.Marshal()
					if err != nil {
						logger.Error("could not marshal segment ack header: ", err)
					} else {
						err = tr.applicationEntity.networkEntity.NUnitDataRequest(
							indication.Source, false, networklayer.NormalPriority, segmentAckBytes)
						if err != nil {
							logger.Error("could not send SegmentACK PDU: ", err)
						}
					}
					tr.SegmentTimer.Restart(false)
				} else {
					// NewSegmentReceived
					tr.responsePdu = append(tr.responsePdu, serviceAck...)
					tr.LastSequenceNumber = (tr.LastSequenceNumber + 1) % 256
					tr.SegmentTimer.Restart(false)
					// Issue N-RELEASE.request to inform datalink layer
					err := tr.applicationEntity.networkEntity.NReleaseRequest(indication.Source)
					if err != nil {
						logger.Error("could not send N-RELEASE.request: ", err)
					}
				}
			} else {
				// LastSegmentOfComplexACK_Received
				tr.responsePdu = append(tr.responsePdu, serviceAck...)
				tr.SegmentTimer.Stop()
				// Issue N-UNITDATA.request to send SegmentACK PDU
				segmentAckHeader := &SegmentAckHeader{
					Flags:            SegmentAckFlags{NegativeAck: false, SentByServer: false},
					InvokeID:         tr.ID.InvokeID,
					SequenceNumber:   header.SequenceNumber,
					ActualWindowSize: tr.ActualWindowSize,
				}
				segmentAckBytes, err := segmentAckHeader.Marshal()
				if err != nil {
					logger.Error("could not marshal segment ack header: ", err)
				} else {
					err = tr.applicationEntity.networkEntity.NUnitDataRequest(
						indication.Source, false, networklayer.NormalPriority, segmentAckBytes)
					if err != nil {
						logger.Error("could not send SegmentACK PDU: ", err)
					}
				}
				apduInd := &APDUIndication{
					Source:        indication.Source,
					ExpectedReply: false,
					Data:          tr.responsePdu,
				}
				tr.applicationEntity.serviceLayer.HandleConfServConfirm(apduInd, tr.serviceChoice)
				tr.resolveFuture(&ConfServResponse{Type: ResponseConfirm, Indication: apduInd})
				tr.applicationEntity.removeClientTransaction(tr.ID)
			}
		} else {
			if tr.DuplicateInWindow(header.SequenceNumber) {
				if tr.DuplicateCount < Ndup {
					// DuplicateSegmentReceived
					// Drop message
					tr.SegmentTimer.Restart(false)
					tr.DuplicateCount++
				} else {
					// TooManyDuplicateSegmentsReceived
					// Drop message
					// Issue a N-UNITDATA.request to send SegmentACK PDU
					segmentAckHeader := &SegmentAckHeader{
						Flags:            SegmentAckFlags{NegativeAck: true, SentByServer: false},
						InvokeID:         tr.ID.InvokeID,
						SequenceNumber:   tr.LastSequenceNumber,
						ActualWindowSize: tr.ActualWindowSize,
					}
					segmentAckBytes, err := segmentAckHeader.Marshal()
					if err != nil {
						logger.Error("could not marshal segment ack header: ", err)
					} else {
						err = tr.applicationEntity.networkEntity.NUnitDataRequest(
							indication.Source, false, networklayer.NormalPriority, segmentAckBytes)
						if err != nil {
							logger.Error("could not send SegmentACK PDU: ", err)
						}
					}
					tr.SegmentTimer.Restart(false)
					tr.DuplicateCount = 0
				}
			} else {
				// SegmentReceivedOutOfOrder
				// Drop message
				// Issue N-UNITDATA.request to send SegmentACK PDU
				segmentAckHeader := &SegmentAckHeader{
					Flags:            SegmentAckFlags{NegativeAck: true, SentByServer: false},
					InvokeID:         tr.ID.InvokeID,
					SequenceNumber:   tr.LastSequenceNumber,
					ActualWindowSize: tr.ActualWindowSize,
				}
				segmentAckBytes, err := segmentAckHeader.Marshal()
				if err != nil {
					logger.Error("could not marshal segment ack header: ", err)
				} else {
					err = tr.applicationEntity.networkEntity.NUnitDataRequest(
						indication.Source, false, networklayer.NormalPriority, segmentAckBytes)
					if err != nil {
						logger.Error("could not send SegmentACK PDU: ", err)
					}
				}
				tr.SegmentTimer.Restart(false)
				tr.InitialSequenceNumber = tr.LastSequenceNumber
				tr.DuplicateCount = 0
			}
		}
	} else {
		// UnexpectedPDU_Received
		tr.SegmentTimer.Stop()
		// Issue a N-UNITDATA.request to send Abort PDU
		abortHeader := &AbortHeader{
			SentByServer: false,
			InvokeID:     uint8(tr.ID.InvokeID), //nolint:gosec
			AbortReason:  uint8(bacnet.AbortInvalidApduInThisState),
		}
		abortBytes, err := abortHeader.Marshal()
		if err != nil {
			logger.Error("could not marshal abort header: ", err)
		} else {
			err = tr.applicationEntity.networkEntity.NUnitDataRequest(
				indication.Source, false, networklayer.NormalPriority, abortBytes)
			if err != nil {
				logger.Error("could not send Abort PDU: ", err)
			}
		}
		// TODO: send ABORT.indication to the user layer
		// Discard transaction
		tr.applicationEntity.removeClientTransaction(tr.ID)
	}
}

func (s *ClientTransactionSegmentedConfState) HandleErrorPdu(
	indication *networklayer.NPDUIndication,
	_ *ErrorHeader,
	payload []byte,
) {
	// UnexpectedPDU_Received
	tr := s.transaction
	tr.SegmentTimer.Stop()
	// Issue a N-UNITDATA.request to send Abort PDU
	abortHeader := &AbortHeader{
		SentByServer: false,
		InvokeID:     uint8(tr.ID.InvokeID), //nolint:gosec
		AbortReason:  uint8(bacnet.AbortInvalidApduInThisState),
	}
	abortBytes, err := abortHeader.Marshal()
	if err != nil {
		logger.Error("could not marshal abort header: ", err)
	} else {
		err = tr.applicationEntity.networkEntity.NUnitDataRequest(
			indication.Source, false, networklayer.NormalPriority, abortBytes)
		if err != nil {
			logger.Error("could not send Abort PDU: ", err)
		}
	}
	apduInd := &APDUIndication{Source: indication.Source, ExpectedReply: false, Data: payload}
	tr.resolveFuture(&ConfServResponse{Type: ResponseReject, Indication: apduInd})
	tr.applicationEntity.removeClientTransaction(tr.ID)
}

func (s *ClientTransactionSegmentedConfState) HandleRejectPdu(
	indication *networklayer.NPDUIndication,
	_ int,
) {
	// UnexpectedPDU_Received
	tr := s.transaction
	tr.SegmentTimer.Stop()
	// Issue a N-UNITDATA.request to send Abort PDU
	abortHeader := &AbortHeader{
		SentByServer: false,
		InvokeID:     uint8(tr.ID.InvokeID), //nolint:gosec
		AbortReason:  uint8(bacnet.AbortInvalidApduInThisState),
	}
	abortBytes, err := abortHeader.Marshal()
	if err != nil {
		logger.Error("could not marshal abort header: ", err)
	} else {
		err = tr.applicationEntity.networkEntity.NUnitDataRequest(
			indication.Source, false, networklayer.NormalPriority, abortBytes)
		if err != nil {
			logger.Error("could not send Abort PDU: ", err)
		}
	}
	apduInd := &APDUIndication{Source: indication.Source, ExpectedReply: false}
	tr.resolveFuture(&ConfServResponse{Type: ResponseAbort, Indication: apduInd})
	tr.applicationEntity.removeClientTransaction(tr.ID)
}

func (s *ClientTransactionSegmentedConfState) HandleSegmentAckPdu(indication *networklayer.NPDUIndication, _ *SegmentAckHeader) {
	// UnexpectedPDU_Received
	tr := s.transaction
	tr.SegmentTimer.Stop()
	// Issue a N-UNITDATA.request to send Abort PDU
	abortHeader := &AbortHeader{
		SentByServer: false,
		InvokeID:     uint8(tr.ID.InvokeID), //nolint:gosec
		AbortReason:  uint8(bacnet.AbortInvalidApduInThisState),
	}
	abortBytes, err := abortHeader.Marshal()
	if err != nil {
		logger.Error("could not marshal abort header: ", err)
	} else {
		err = tr.applicationEntity.networkEntity.NUnitDataRequest(
			indication.Source, false, networklayer.NormalPriority, abortBytes)
		if err != nil {
			logger.Error("could not send Abort PDU: ", err)
		}
	}
	// TODO: send ABORT.indication to the user layer
	// Discard transaction
	tr.applicationEntity.removeClientTransaction(tr.ID)
}

func (s *ClientTransactionSegmentedConfState) HandleAbortPdu(_ int) {
	// AbortPDU_Received
	tr := s.transaction
	tr.SegmentTimer.Stop()
	tr.resolveFuture(&ConfServResponse{Type: ResponseAbort, Indication: nil})
	tr.applicationEntity.removeClientTransaction(tr.ID)
}

func (s *ClientTransactionSegmentedConfState) HandleConfServRequest(
	_ *bacnet.BACnetAddress,
	_ bool,
	_ networklayer.NPDUPriority,
) {
}
