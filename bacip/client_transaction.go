package bacip

import "github.com/REQUEA/bacnet"

type ClientTransactionState interface {
	HandleSimpleAckPdu(*NPDUIndication, *SimpleAckHeader)
	HandleComplexAckPdu(*NPDUIndication, *ComplexAckHeader, []byte)
	HandleErrorPdu(*NPDUIndication, *ErrorHeader, []byte)
	HandleRejectPdu(*NPDUIndication, int)
	HandleSegmentAckPdu(*NPDUIndication, *SegmentAckHeader)
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

func (t *ClientTransaction) HandleSimpleAckPdu(indication *NPDUIndication, header *SimpleAckHeader) {
	if t.state != nil {
		t.state.HandleSimpleAckPdu(indication, header)
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

func (t *ClientTransaction) HandleErrorPdu(indication *NPDUIndication, header *ErrorHeader, errorData []byte) {
	if t.state != nil {
		t.state.HandleErrorPdu(indication, header, errorData)
	}
}

func (t *ClientTransaction) HandleRejectPdu(indication *NPDUIndication, reason int) {
	if t.state != nil {
		t.state.HandleRejectPdu(indication, reason)
	}
}

func (t *ClientTransaction) HandleSegmentAckPdu(indication *NPDUIndication, header *SegmentAckHeader) {
	if t.state != nil {
		t.state.HandleSegmentAckPdu(indication, header)
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

func (s *ClientTransactionIdleState) HandleSimpleAckPdu(*NPDUIndication, *SimpleAckHeader) {
	// never called
}

func (s *ClientTransactionIdleState) HandleComplexAckPdu(
	indication *NPDUIndication,
	header *ComplexAckHeader,
	serviceRequest []byte,
) {
	if header.Flags.SegmentedRequest {
		tr := s.transaction
		// UnexpectedSegmentInfoReceived
		abortHeader := &AbortHeader{
			SentByServer: false,
			InvokeId:     uint8(header.InvokeId),
			AbortReason:  uint8(bacnet.AbortInvalidApduInThisState),
		}
		abortBytes, err := abortHeader.Marshal()
		if err != nil {
			logger.Error("could not marshal abort header: ", err)
			tr.applicationEntity.removeClientTransaction(tr.Id)
			return
		}
		err = tr.applicationEntity.networkEntity.NUnitDataRequest(
			indication.source,
			false,
			NormalPriority,
			abortBytes,
		)
		if err != nil {
			logger.Error("could not send Abort PDU: ", err)
		}
		return
	}
}

func (s *ClientTransactionIdleState) HandleErrorPdu(*NPDUIndication, *ErrorHeader, []byte) {
	// never called
}

func (s *ClientTransactionIdleState) HandleRejectPdu(*NPDUIndication, int) {
	// never called
}

func (s *ClientTransactionIdleState) HandleSegmentAckPdu(indication *NPDUIndication, header *SegmentAckHeader) {
	if header.Flags.SentByServer {
		// client state machine IDLE state UnexpectedSegmentInfoReceived
		// TODO issue a N-UNITDATA.request to transmit a AbortPDU
	}
}
func (s *ClientTransactionIdleState) HandleAbortPdu(int) {
	// never called
}

////////////////////////////////////////////////////////////////////////////////
// AWAIT_CONFIRMATION State
////////////////////////////////////////////////////////////////////////////////

type ClientTransactionAwaitConfirmationState struct {
	transaction *ClientTransaction
}

func (s *ClientTransactionAwaitConfirmationState) HandleSimpleAckPdu(*NPDUIndication, *SimpleAckHeader) {
	// SimpleACK_Received
	tr := s.transaction
	tr.RequestTimer.Stop()
	// TODO: issue a CONF_SERV.confirm to the local application
	tr.applicationEntity.removeClientTransaction(tr.Id)
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
			tr.RequestTimer.Stop()
			// TODO: refine the calculation ActualWindowSize ('based [...] and on local conditions')
			tr.ActualWindowSize = min(tr.MaxSegmentsAccepted, header.ProposedWindowSize)
			segmentAckHeader := &SegmentAckHeader{
				Flags:            SegmentAckFlags{NegativeAck: false, SentByServer: false},
				InvokeId:         tr.Id.InvokeId,
				SequenceNumber:   header.SequenceNumber,
				ActualWindowSize: tr.ActualWindowSize,
			}
			segmentAckBytes, err := segmentAckHeader.Marshal()
			if err != nil {
				logger.Error("could not marshal segment-ack header: ", err)
			} else {
				err = tr.applicationEntity.networkEntity.NUnitDataRequest(
					indication.source, false, NormalPriority, segmentAckBytes)
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
		// TODO: issue CONF_SERV.confirm to the local application
		tr.applicationEntity.removeClientTransaction(tr.Id)
	}
}

func (s *ClientTransactionAwaitConfirmationState) HandleErrorPdu(indication *NPDUIndication, header *ErrorHeader, errorData []byte) {
	// ErrorPDU_Received
	// Stop RequestTimer
	tr := s.transaction
	tr.RequestTimer.Stop()
	// TODO: send CONF_SERV.confirm(-) to user layer
	// discard transaction
	tr.applicationEntity.removeClientTransaction(tr.Id)
}

func (s *ClientTransactionAwaitConfirmationState) HandleRejectPdu(*NPDUIndication, int) {
	// RejectPDU_Received
	tr := s.transaction
	tr.RequestTimer.Stop()
	// TODO: send REJECT.indication to user layer
	// discard transaction
	tr.applicationEntity.removeClientTransaction(tr.Id)
}

func (s *ClientTransactionAwaitConfirmationState) HandleSegmentAckPdu(*NPDUIndication, *SegmentAckHeader) {
	// SegmentACK_Received
	// Drop PDU
}

func (s *ClientTransactionAwaitConfirmationState) HandleAbortPdu(reason int) {
	// AbortPDU_Received
	tr := s.transaction
	tr.RequestTimer.Stop()
	// TODO: send ABORT.indication to user layer
	// discard transaction
	tr.applicationEntity.removeClientTransaction(tr.Id)
}

////////////////////////////////////////////////////////////////////////////////
// SEGMENTED_REQUEST State
////////////////////////////////////////////////////////////////////////////////

type ClientTransactionSegmentedRequestState struct {
	transaction *ClientTransaction
}

func (s *ClientTransactionSegmentedRequestState) HandleSimpleAckPdu(
	indication *NPDUIndication,
	header *SimpleAckHeader,
) {
	tr := s.transaction
	if tr.SentAllSegments {
		// SimpleACK_Received
		// Stop SegmentTimer
		tr.SegmentTimer.Stop()
		// TODO: issue CONF_SERV.confirm to local application
		// Discard transaction
		tr.applicationEntity.removeClientTransaction(tr.Id)
	} else {
		// UnexpectedPDU_Received
		// Stop SegmentTimer
		tr.SegmentTimer.Stop()
		// Issue N-UNITDATA.request to send a AbortPDU
		header := &AbortHeader{
			SentByServer: false,
			InvokeId:     uint8(tr.Id.InvokeId),
			AbortReason:  uint8(bacnet.AbortInvalidApduInThisState),
		}
		abortBytes, err := header.Marshal()
		if err != nil {
			logger.Error("could not marshal abort header: ", err)
		} else {
			err = tr.applicationEntity.networkEntity.NUnitDataRequest(
				indication.source, false, NormalPriority, abortBytes)
			if err != nil {
				logger.Error("could not send Abort PDU: ", err)
			}
		}
		// TODO: issue a ABORT.indication to the local application program
		// Discard transaction
		tr.applicationEntity.removeClientTransaction(tr.Id)
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
					InvokeId:         tr.Id.InvokeId,
					SequenceNumber:   header.SequenceNumber,
					ActualWindowSize: tr.ActualWindowSize,
				}
				segmentAckBytes, err := segmentAckHeader.Marshal()
				if err != nil {
					logger.Error("could not marshal segment ack header: ", err)
				} else {
					err = tr.applicationEntity.networkEntity.NUnitDataRequest(
						indication.source, false, NormalPriority, segmentAckBytes)
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
				InvokeId:     uint8(tr.Id.InvokeId),
				AbortReason:  uint8(bacnet.AbortInvalidApduInThisState),
			}
			abortBytes, err := abortHeader.Marshal()
			if err != nil {
				logger.Error("could not marshal abort header: ", err)
			} else {
				err = tr.applicationEntity.networkEntity.NUnitDataRequest(
					indication.source, false, NormalPriority, abortBytes)
				if err != nil {
					logger.Error("could not send Abort PDU: ", err)
				}
			}
			// TODO: issue a ABORT.indication to the local application program
			// Discard transaction
			tr.applicationEntity.removeClientTransaction(tr.Id)
		}
	} else {
		if tr.SentAllSegments {
			// UnsegmentedComplexACK_Received
			tr.SegmentTimer.Stop()
			// TODO: issue CONF_SERV.confirm to local application
			tr.applicationEntity.removeClientTransaction(tr.Id)
		} else {
			// UnexpectedPDU_Received
			tr.SegmentTimer.Stop()
			// Issue N-UNITDATA.request to send a AbortPDU
			abortHeader := &AbortHeader{
				SentByServer: false,
				InvokeId:     uint8(tr.Id.InvokeId),
				AbortReason:  uint8(bacnet.AbortInvalidApduInThisState),
			}
			abortBytes, err := abortHeader.Marshal()
			if err != nil {
				logger.Error("could not marshal abort header: ", err)
			} else {
				err = tr.applicationEntity.networkEntity.NUnitDataRequest(
					indication.source, false, NormalPriority, abortBytes)
				if err != nil {
					logger.Error("could not send Abort PDU: ", err)
				}
			}
			// TODO: issue a ABORT.indication to the local application program
			tr.applicationEntity.removeClientTransaction(tr.Id)
		}
	}
}
func (s *ClientTransactionSegmentedRequestState) HandleErrorPdu(indication *NPDUIndication, header *ErrorHeader, errorData []byte) {
	tr := s.transaction
	if tr.SentAllSegments {
		// ErrorPDU_Received
		tr.SegmentTimer.Stop()
		// TODO: issue CONF_SERV.confirm(-) to the local application
		// Discard transaction
		tr.applicationEntity.removeClientTransaction(tr.Id)
	} else {
		// UnexpectedPDU_Received
		tr.SegmentTimer.Stop()
		// Issue N-UNITDATA.request to send a AbortPDU
		abortHeader := &AbortHeader{
			SentByServer: false,
			InvokeId:     uint8(tr.Id.InvokeId),
			AbortReason:  uint8(bacnet.AbortInvalidApduInThisState),
		}
		abortBytes, err := abortHeader.Marshal()
		if err != nil {
			logger.Error("could not marshal abort header: ", err)
		} else {
			err = tr.applicationEntity.networkEntity.NUnitDataRequest(
				indication.source, false, NormalPriority, abortBytes)
			if err != nil {
				logger.Error("could not send Abort PDU: ", err)
			}
		}
		// TODO: issue a ABORT.indication to the local application program
		// Discard transaction
		tr.applicationEntity.removeClientTransaction(tr.Id)
	}
}
func (s *ClientTransactionSegmentedRequestState) HandleRejectPdu(*NPDUIndication, int) {
	// RejectPDU_Received
	tr := s.transaction
	tr.SegmentTimer.Stop()
	// TODO: issue a REJECT.indication to the local application
	// Discard transaction
	tr.applicationEntity.removeClientTransaction(tr.Id)
}
func (s *ClientTransactionSegmentedRequestState) HandleSegmentAckPdu(indication *NPDUIndication, header *SegmentAckHeader) {
	tr := s.transaction
	if tr.InWindow(header.SequenceNumber, tr.InitialSequenceNumber) {
		if len(tr.segments) > 0 { // TODO this is not correct, check the amount of segments remaining to send
			// NewACK_Received
			tr.InitialSequenceNumber = (header.SequenceNumber + 1) % 256
			tr.ActualWindowSize = header.ActualWindowSize
			tr.SegmentRetryCount = 0
			// TODO: call FillWindow to send segments
			tr.SegmentTimer.Restart(false)
		} else {
			// FinalACK_Received
			tr.SegmentTimer.Stop()
			tr.state = &ClientTransactionAwaitConfirmationState{tr}
		}
	} else {
		// DuplicateACK_Received
		tr.SegmentTimer.Restart(false)
	}
}
func (s *ClientTransactionSegmentedRequestState) HandleAbortPdu(int) {
	// AbortPDU_Received
	tr := s.transaction
	tr.SegmentTimer.Stop()
	// TODO: issue ABORT.indication to local application
	// Discard transaction
	tr.applicationEntity.removeClientTransaction(tr.Id)
}

////////////////////////////////////////////////////////////////////////////////
// SEGMENTED_CONF State
////////////////////////////////////////////////////////////////////////////////

type ClientTransactionSegmentedConfState struct {
	transaction *ClientTransaction
}

func (s *ClientTransactionSegmentedConfState) HandleSimpleAckPdu(
	indication *NPDUIndication,
	header *SimpleAckHeader,
) {
	// UnexpectedPDU_Received
	tr := s.transaction
	tr.SegmentTimer.Stop()
	// Issue a N-UNITDATA.request to send Abort PDU
	abortHeader := &AbortHeader{
		SentByServer: false,
		InvokeId:     uint8(tr.Id.InvokeId),
		AbortReason:  uint8(bacnet.AbortInvalidApduInThisState),
	}
	abortBytes, err := abortHeader.Marshal()
	if err != nil {
		logger.Error("could not marshal abort header: ", err)
	} else {
		err = tr.applicationEntity.networkEntity.NUnitDataRequest(
			indication.source, false, NormalPriority, abortBytes)
		if err != nil {
			logger.Error("could not send Abort PDU: ", err)
		}
	}
	// TODO: send ABORT.indication to user layer (InvalidApduInThisState)
	// Discard transaction
	tr.applicationEntity.removeClientTransaction(tr.Id)
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
					// Issue N-UNITDATA.request to send SegmentACK PDU
					segmentAckHeader := &SegmentAckHeader{
						Flags:            SegmentAckFlags{NegativeAck: false, SentByServer: false},
						InvokeId:         tr.Id.InvokeId,
						SequenceNumber:   header.SequenceNumber,
						ActualWindowSize: tr.ActualWindowSize,
					}
					segmentAckBytes, err := segmentAckHeader.Marshal()
					if err != nil {
						logger.Error("could not marshal segment ack header: ", err)
					} else {
						err = tr.applicationEntity.networkEntity.NUnitDataRequest(
							indication.source, false, NormalPriority, segmentAckBytes)
						if err != nil {
							logger.Error("could not send SegmentACK PDU: ", err)
						}
					}
					tr.SegmentTimer.Restart(false)
				} else {
					// NewSegmentReceived
					// TODO: save PDU segment
					tr.LastSequenceNumber = (tr.LastSequenceNumber + 1) % 256
					tr.SegmentTimer.Restart(false)
					// Issue N-RELEASE.request to inform datalink layer
					err := tr.applicationEntity.networkEntity.NReleaseRequest(indication.source)
					if err != nil {
						logger.Error("could not send N-RELEASE.request: ", err)
					}
				}
			} else {
				// LastSegmentOfComplexACK_Received
				// TODO: save PDU segment
				tr.SegmentTimer.Stop()
				// Issue N-UNITDATA.request to send SegmentACK PDU
				segmentAckHeader := &SegmentAckHeader{
					Flags:            SegmentAckFlags{NegativeAck: false, SentByServer: false},
					InvokeId:         tr.Id.InvokeId,
					SequenceNumber:   header.SequenceNumber,
					ActualWindowSize: tr.ActualWindowSize,
				}
				segmentAckBytes, err := segmentAckHeader.Marshal()
				if err != nil {
					logger.Error("could not marshal segment ack header: ", err)
				} else {
					err = tr.applicationEntity.networkEntity.NUnitDataRequest(
						indication.source, false, NormalPriority, segmentAckBytes)
					if err != nil {
						logger.Error("could not send SegmentACK PDU: ", err)
					}
				}
				// TODO: issue CONF_SERV.confirm to the local application
				// Discard transaction
				tr.applicationEntity.removeClientTransaction(tr.Id)
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
						InvokeId:         tr.Id.InvokeId,
						SequenceNumber:   tr.LastSequenceNumber,
						ActualWindowSize: tr.ActualWindowSize,
					}
					segmentAckBytes, err := segmentAckHeader.Marshal()
					if err != nil {
						logger.Error("could not marshal segment ack header: ", err)
					} else {
						err = tr.applicationEntity.networkEntity.NUnitDataRequest(
							indication.source, false, NormalPriority, segmentAckBytes)
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
					InvokeId:         tr.Id.InvokeId,
					SequenceNumber:   tr.LastSequenceNumber,
					ActualWindowSize: tr.ActualWindowSize,
				}
				segmentAckBytes, err := segmentAckHeader.Marshal()
				if err != nil {
					logger.Error("could not marshal segment ack header: ", err)
				} else {
					err = tr.applicationEntity.networkEntity.NUnitDataRequest(
						indication.source, false, NormalPriority, segmentAckBytes)
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
			InvokeId:     uint8(tr.Id.InvokeId),
			AbortReason:  uint8(bacnet.AbortInvalidApduInThisState),
		}
		abortBytes, err := abortHeader.Marshal()
		if err != nil {
			logger.Error("could not marshal abort header: ", err)
		} else {
			err = tr.applicationEntity.networkEntity.NUnitDataRequest(
				indication.source, false, NormalPriority, abortBytes)
			if err != nil {
				logger.Error("could not send Abort PDU: ", err)
			}
		}
		// TODO: send ABORT.indication to the user layer
		// Discard transaction
		tr.applicationEntity.removeClientTransaction(tr.Id)
	}
}

func (s *ClientTransactionSegmentedConfState) HandleErrorPdu(
	indication *NPDUIndication,
	header *ErrorHeader,
	payload []byte,
) {
	// UnexpectedPDU_Received
	tr := s.transaction
	tr.SegmentTimer.Stop()
	// Issue a N-UNITDATA.request to send Abort PDU
	abortHeader := &AbortHeader{
		SentByServer: false,
		InvokeId:     uint8(tr.Id.InvokeId),
		AbortReason:  uint8(bacnet.AbortInvalidApduInThisState),
	}
	abortBytes, err := abortHeader.Marshal()
	if err != nil {
		logger.Error("could not marshal abort header: ", err)
	} else {
		err = tr.applicationEntity.networkEntity.NUnitDataRequest(
			indication.source, false, NormalPriority, abortBytes)
		if err != nil {
			logger.Error("could not send Abort PDU: ", err)
		}
	}
	// TODO: send ABORT.indication to the user layer
	// Discard transaction
	tr.applicationEntity.removeClientTransaction(tr.Id)
}

func (s *ClientTransactionSegmentedConfState) HandleRejectPdu(
	indication *NPDUIndication,
	reason int,
) {
	// UnexpectedPDU_Received
	tr := s.transaction
	tr.SegmentTimer.Stop()
	// Issue a N-UNITDATA.request to send Abort PDU
	abortHeader := &AbortHeader{
		SentByServer: false,
		InvokeId:     uint8(tr.Id.InvokeId),
		AbortReason:  uint8(bacnet.AbortInvalidApduInThisState),
	}
	abortBytes, err := abortHeader.Marshal()
	if err != nil {
		logger.Error("could not marshal abort header: ", err)
	} else {
		err = tr.applicationEntity.networkEntity.NUnitDataRequest(
			indication.source, false, NormalPriority, abortBytes)
		if err != nil {
			logger.Error("could not send Abort PDU: ", err)
		}
	}
	// TODO: send ABORT.indication to the user layer
	// Discard transaction
	tr.applicationEntity.removeClientTransaction(tr.Id)
}

func (s *ClientTransactionSegmentedConfState) HandleSegmentAckPdu(indication *NPDUIndication, header *SegmentAckHeader) {
	// UnexpectedPDU_Received
	tr := s.transaction
	tr.SegmentTimer.Stop()
	// Issue a N-UNITDATA.request to send Abort PDU
	abortHeader := &AbortHeader{
		SentByServer: false,
		InvokeId:     uint8(tr.Id.InvokeId),
		AbortReason:  uint8(bacnet.AbortInvalidApduInThisState),
	}
	abortBytes, err := abortHeader.Marshal()
	if err != nil {
		logger.Error("could not marshal abort header: ", err)
	} else {
		err = tr.applicationEntity.networkEntity.NUnitDataRequest(
			indication.source, false, NormalPriority, abortBytes)
		if err != nil {
			logger.Error("could not send Abort PDU: ", err)
		}
	}
	// TODO: send ABORT.indication to the user layer
	// Discard transaction
	tr.applicationEntity.removeClientTransaction(tr.Id)
}

func (s *ClientTransactionSegmentedConfState) HandleAbortPdu(reason int) {
	// AbortPDU_Received
	tr := s.transaction
	tr.SegmentTimer.Stop()
	// TODO: issue ABORT.indication to local application
	// Discard transaction
	tr.applicationEntity.removeClientTransaction(tr.Id)
}
