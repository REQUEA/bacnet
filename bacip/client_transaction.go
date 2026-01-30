package bacip

type ClientTransactionState interface {
	HandleSimpleAckPdu(*SimpleACKHeader)
	HandleComplexAckPdu(*NPDUIndication, *ComplexAckHeader, []byte)
	HandleErrorPdu(*ErrorHeader, []byte)
	HandleRejectPdu(int)
	HandleSegmentAckPdu(*SegmentAckHeader)
	HandleAbortPdu(int)
}

type ClientTransaction struct {
	Transaction
	state ClientTransactionState
}

func NewClientTransaction(id *TransactionId) ClientTransaction {
	result := ClientTransaction{
		Transaction: Transaction{
			Id: id,
		},
	}
	result.state = &ClientTransactionIdleState{
		transaction: &result,
	}
	return result
}

func (t *ClientTransaction) HandleSimpleAckPdu(header *SimpleACKHeader) {
	if t.state != nil {
		t.state.HandleSimpleAckPdu(header)
	}
}

func (t *ClientTransaction) HandleComplexAckPdu(
	indication *NPDUIndication,
	header *ComplexAckHeader,
	serviceAck []byte,
) {
	if t.state != nil {
		t.state.HandleComplexAckPdu(indication, header, serviceAck)
	}
}

func (t *ClientTransaction) HandleErrorPdu(header *ErrorHeader, errorData []byte) {
	if t.state != nil {
		t.state.HandleErrorPdu(header, errorData)
	}
}

func (t *ClientTransaction) HandleRejectPdu(reason int) {
	if t.state != nil {
		t.state.HandleRejectPdu(reason)
	}
}

func (t *ClientTransaction) HandleSegmentAckPdu(header *SegmentAckHeader) {
	if t.state != nil {
		t.state.HandleSegmentAckPdu(header)
	}
}

func (t *ClientTransaction) HandleAbortPdu(reason int) {
	if t.state != nil {
		t.state.HandleAbortPdu(reason)
	}
}

////////////////////////////////////////////////////////////////////////////////
// IDLE State
////////////////////////////////////////////////////////////////////////////////

type ClientTransactionIdleState struct {
	transaction *ClientTransaction
}

// All the PDUs are handled directly from the ApplicationEntity
func (s *ClientTransactionIdleState) HandleSimpleAckPdu(*SimpleACKHeader) {}
func (s *ClientTransactionIdleState) HandleComplexAckPdu(*NPDUIndication, *ComplexAckHeader, []byte) {
}
func (s *ClientTransactionIdleState) HandleErrorPdu(*ErrorHeader, []byte)   {}
func (s *ClientTransactionIdleState) HandleRejectPdu(int)                   {}
func (s *ClientTransactionIdleState) HandleSegmentAckPdu(*SegmentAckHeader) {}
func (s *ClientTransactionIdleState) HandleAbortPdu(int)                    {}

////////////////////////////////////////////////////////////////////////////////
// AWAIT_CONFIRMATION State
////////////////////////////////////////////////////////////////////////////////

type ClientTransactionAwaitConfirmationState struct {
	transaction *ClientTransaction
}

func (s *ClientTransactionAwaitConfirmationState) HandleSimpleAckPdu(*SimpleACKHeader) {
	// SimpleACK_Received
	// TODO: stop RequestTimer
	// TODO: issue a CONF_SERV.confirm to the local application
	// TODO: discard transaction
}

func (s *ClientTransactionAwaitConfirmationState) HandleComplexAckPdu(
	indication *NPDUIndication,
	header *ComplexAckHeader,
	serviceAck []byte,
) {
	tr := s.transaction
	if header.Flags.SegmentedRequest {
		if header.SequenceNumber == 0 {
			// SegmentedComplexACK_Received
			// TODO: save PDU segment
			// TODO: stop RequestTimer
			// TODO: compute ActualWindowSize
			// TODO: issue N-UNITDATA.request to send SegmentACK PDU
			// TODO: start SegmentTimer
			tr.LastSequenceNumber = 0
			tr.InitialSequenceNumber = 0
			tr.DuplicateCount = 0
			tr.state = &ClientTransactionSegmentedConfState{tr}
		}
	} else {
		// UnsegmentedComplexACK_Received
		// TODO: stop RequestTimer
		// TODO: issue CONF_SERV.confirm to the local application
		// TODO: discard transaction
	}
}

func (s *ClientTransactionAwaitConfirmationState) HandleErrorPdu(*ErrorHeader, []byte) {
	// ErrorPDU_Received
	// TODO: Stop RequestTimer
	// TODO: issue N-UNITDATA.requeste to send Abort PDU
	// TODO: discard transaction
}

func (s *ClientTransactionAwaitConfirmationState) HandleRejectPdu(int)                   {}
func (s *ClientTransactionAwaitConfirmationState) HandleSegmentAckPdu(*SegmentAckHeader) {}
func (s *ClientTransactionAwaitConfirmationState) HandleAbortPdu(int)                    {}

////////////////////////////////////////////////////////////////////////////////
// SEGMENTED_REQUEST State
////////////////////////////////////////////////////////////////////////////////

type ClientTransactionSegmentedRequestState struct {
	transaction *ClientTransaction
}

func (s *ClientTransactionSegmentedRequestState) HandleSimpleAckPdu(*SimpleACKHeader) {
	tr := s.transaction
	if tr.SentAllSegments {
		// SimpleACK_Received
		// TODO: stop SegmentTimer
		// TODO: issue CONF_SERV.confirm to local application
		// TODO: discard transaction
	} else {
		// UnexpectedPDU_Received
		// TODO: stop SegmentTimer
		// TODO: issue N-UNITDATA.request to send a AbortPDU
		// TODO: issue a ABORT.indication to the local application program
		// TODO: discard transaction
	}
}
func (s *ClientTransactionSegmentedRequestState) HandleComplexAckPdu(
	indication *NPDUIndication,
	header *ComplexAckHeader,
	serviceAck []byte,
) {
	tr := s.transaction
	if header.Flags.SegmentedRequest {
		if header.SequenceNumber == 0 {
			if tr.SentAllSegments {
				// SegmentedComplexACK_Received
				// TODO: Save PDU segment
				// TODO: stop SegmentTimer
				// TODO: compute ActualWindowSize
				// TODO: issue N-UNITDATA.request to send a SegmentACK PDU
				// TODO: start SegmentTimer
				tr.LastSequenceNumber = 0
				tr.InitialSequenceNumber = 0
				tr.DuplicateCount = 0
				tr.state = &ClientTransactionSegmentedConfState{tr}
			}
		} else {
			// UnexpectedPDU_Received
			// TODO: stop SegmentTimer
			// TODO: issue N-UNITDATA.request to send a AbortPDU
			// TODO: issue a ABORT.indication to the local application program
			// TODO: discard transaction
		}
	} else {
		if tr.SentAllSegments {
			// UnsegmentedComplexACK_Received
			// TODO: Stop SegmentTimer
			// TODO: issue CONF_SERV.confirm to local application
			// TODO: discard transaction
		} else {
			// UnexpectedPDU_Received
			// TODO: stop SegmentTimer
			// TODO: issue N-UNITDATA.request to send a AbortPDU
			// TODO: issue a ABORT.indication to the local application program
			// TODO: discard transaction
		}
	}
}
func (s *ClientTransactionSegmentedRequestState) HandleErrorPdu(*ErrorHeader, []byte) {
	tr := s.transaction
	if tr.SentAllSegments {
		// ErrorPDU_Received
		// TODO: Stop SegmentTimer
		// TODO: issue CONF_SERV.confirm to the local application
		// TODO: discard transaction
	} else {
		// UnexpectedPDU_Received
		// TODO: stop SegmentTimer
		// TODO: issue N-UNITDATA.request to send a AbortPDU
		// TODO: issue a ABORT.indication to the local application program
		// TODO: discard transaction
	}
}
func (s *ClientTransactionSegmentedRequestState) HandleRejectPdu(int) {
	// RejectPDU_Received
	// TODO: stop SegmentTimer
	// TODO: issue a REJECT.indication to the local application
	// TODO: discard transaction
}
func (s *ClientTransactionSegmentedRequestState) HandleSegmentAckPdu(header *SegmentAckHeader) {
	tr := s.transaction
	if tr.InWindow(header.SequenceNumber, tr.InitialSequenceNumber) {
		if len(tr.segments) > 0 { // TODO this is not correct, check the amount of segments remaining to send
			// NewACK_Received
			tr.InitialSequenceNumber = (header.SequenceNumber + 1) % 256
			tr.ActualWindowSize = header.ActualWindowSize
			tr.SegmentRetryCount = 0
			// TODO: call FillWindows to send segmentsm
			// TODO: restart SegmentTimer
		} else {
			// FinalACK_Received
			// TODO: Stop SegmentTimer
			tr.state = &ClientTransactionAwaitConfirmationState{tr}
		}
	} else {
		// TODO: restart SegmentTimer
	}
}
func (s *ClientTransactionSegmentedRequestState) HandleAbortPdu(int) {
	// AbortPDU_Received
	// TODO: stop SegmentTimer
	// TODO: issue ABORT.indication to local application
	// TODO: discard transaction
}

////////////////////////////////////////////////////////////////////////////////
// SEGMENTED_CONF State
////////////////////////////////////////////////////////////////////////////////

type ClientTransactionSegmentedConfState struct {
	transaction *ClientTransaction
}

func (s *ClientTransactionSegmentedConfState) HandleSimpleAckPdu(*SimpleACKHeader) {
	// UnexpectedPDU_Received
	// TODO: stop SegmentTimer
	// TODO: issue a N-UNITDATA.request to send Abort PDU
	// TODO: discard transaction
}

func (s *ClientTransactionSegmentedConfState) HandleComplexAckPdu(
	indication *NPDUIndication,
	header *ComplexAckHeader,
	serviceAck []byte,
) {
	tr := s.transaction
	if header.Flags.SegmentedRequest {
		// TODO: support NewSegmentReceived_InconsistentAttributes
		if header.SequenceNumber == (tr.LastSequenceNumber+1)%256 {
			// TODO: support NewSegmentReceived_NoSpace
			if header.Flags.MoreSegments {
				if header.SequenceNumber == (tr.InitialSequenceNumber+tr.ActualWindowSize)%256 {
					// LastSegmentOfGroup_Received
					// TODO: save PDU segment
					tr.LastSequenceNumber = (tr.LastSequenceNumber + 1) % 256
					tr.InitialSequenceNumber = tr.LastSequenceNumber
					tr.DuplicateCount = 0
					// TODO issue N-UNITDATA.request to send SegmentACK PDU
					// TODO: Restart SegmentTimer
				} else {
					// NewSegmentReceived
					// TODO: save PDU segment
					tr.LastSequenceNumber = (tr.LastSequenceNumber + 1) % 256
					// TODO: restart SegmentTimer
					// TODO: issue N-RELEASE.request to inform datalink layer
				}
			} else {
				// LastSegmentOfComplexACK_Received
				// TODO: save PDU segment
				// TODO: stop SegmentTimer
				// TODO: issue N-UNITDATA.request to send SegmentACK PDU
				// TODO: issue CONF_SERV.confirm to the local application
				// TODO: discard transaction
			}
		} else {
			if tr.DuplicateInWindow(header.SequenceNumber) {
				if tr.DuplicateCount < Ndup {
					// DuplicateSegmentReceived
					// Drop message
					// TODO: restart SegmentTimer
					tr.DuplicateCount++
				} else {
					// TooManyDuplicateSegmentsReceived
					// Drop message
					// TODO: issue a N-UNITDATA.request to send SegmentACK PDU
					// TODO: restart SegmentTimer
					tr.DuplicateCount = 0
				}
			} else {
				// SegmentReceivedOutOfOrder
				// Drop message
				// TODO: issue N-UNITDATA.request to send SegmentACK PDU
				// TODO: restart SegmentTimer
				tr.InitialSequenceNumber = tr.LastSequenceNumber
				tr.DuplicateCount = 0
			}
		}
	} else {
		// UnexpectedPDU_Received
		// TODO: stop SegmentTimer
		// TODO: issue a N-UNITDATA.request to send Abort PDU
		// TODO: discard transaction
	}
}
func (s *ClientTransactionSegmentedConfState) HandleErrorPdu(*ErrorHeader, []byte) {
	// UnexpectedPDU_Received
	// TODO: stop SegmentTimer
	// TODO: issue a N-UNITDATA.request to send Abort PDU
	// TODO: discard transaction
}

func (s *ClientTransactionSegmentedConfState) HandleRejectPdu(int) {
	// UnexpectedPDU_Received
	// TODO: stop SegmentTimer
	// TODO: issue a N-UNITDATA.request to send Abort PDU
	// TODO: discard transaction
}

func (s *ClientTransactionSegmentedConfState) HandleSegmentAckPdu(*SegmentAckHeader) {
	// UnexpectedPDU_Received
	// TODO: stop SegmentTimer
	// TODO: issue a N-UNITDATA.request to send Abort PDU
	// TODO: discard transaction
}

func (s *ClientTransactionSegmentedConfState) HandleAbortPdu(int) {
	// AbortPDU_Received
	// TODO: stop SegmentTimer
	// TODO: issue ABORT.indication to local application
	// TODO: discard transaction
}
