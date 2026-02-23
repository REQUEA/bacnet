package bacip

import "github.com/REQUEA/bacnet"

type ServerTransactionState interface {
	HandleUnconfirmedServiceRequestPdu()
	HandleConfirmedServiceRequestPdu(*NPDUIndication, *ConfirmedServiceRequestHeader, []byte)
	HandleSegmentAckPdu(*SegmentAckHeader)
	HandleAbortPdu(int)
	OnSegmentTimerFired()
	OnRequestTimerFired()
}

type ServerTransaction struct {
	Transaction
	state ServerTransactionState
}

func NewServerTransaction(id *TransactionId) ServerTransaction {
	result := ServerTransaction{
		Transaction: Transaction{
			Id: id,
		},
	}
	result.state = &ServerTransactionIdleState{
		transaction: &result,
	}
	return result
}

func (t *ServerTransaction) HandleConfirmedServiceRequestPdu(
	indication *NPDUIndication,
	header *ConfirmedServiceRequestHeader,
	serverRequest []byte,
) {
	if t.state != nil {
		t.state.HandleConfirmedServiceRequestPdu(indication, header, serverRequest)
	}
}

func (t *ServerTransaction) HandleAbortPdu(reason int) {
	if t.state != nil {
		t.state.HandleAbortPdu(reason)
	}
}

func (t *ServerTransaction) HandleSegmentAckPdu(header *SegmentAckHeader) {
	if t.state != nil {
		t.state.HandleSegmentAckPdu(header)
	}
}

type ServerSegmentTimerEvent struct {
	transaction *ServerTransaction
}

func (e *ServerSegmentTimerEvent) Exec() {
	if e.transaction.state != nil {
		e.transaction.state.OnSegmentTimerFired()
	}
}

func (t *ServerTransaction) OnSegmentTimerFired() {
	t.events <- &ServerSegmentTimerEvent{
		transaction: t,
	}
}

func (t *ServerTransaction) OnRequestTimerFired() {
	if t.state != nil {
		t.state.OnRequestTimerFired()
	}
}

////////////////////////////////////////////////////////////////////////////////
// IDLE State
////////////////////////////////////////////////////////////////////////////////

type ServerTransactionIdleState struct {
	transaction *ServerTransaction
}

func (s *ServerTransactionIdleState) HandleUnconfirmedServiceRequestPdu() {}

func (s *ServerTransactionIdleState) HandleSegmentAckPdu(*SegmentAckHeader) {}

func (s *ServerTransactionIdleState) HandleAbortPdu(int) {}

func (s *ServerTransactionIdleState) HandleConfirmedServiceRequestPdu(
	indication *NPDUIndication,
	header *ConfirmedServiceRequestHeader,
	serviceRequest []byte,
) {
	tr := s.transaction
	if !header.Flags.SegmentedRequest {
		// ConfirmedUnsegmentedReceived
		// TODO send CONF_SERV.indication to application
		tr.RequestTimer.Start()
		tr.state = &ServerTransactionAwaitResponseState{
			transaction: tr,
		}
		return
	}
	// TODO add case where segmentation is not supported
	if header.SequenceNumber == 0 {
		if header.ProposedWindowSize > 0 && header.ProposedWindowSize < 128 {
			tr.ActualWindowSize = min(tr.MaxSegmentsAccepted, header.ProposedWindowSize)
			tr.ProposedWindowSize = header.ProposedWindowSize

			// send N-UNITDATA.request to transmit SegmentAck-PDU
			segmentAckPdu := SegmentAckHeader{
				Flags:            SegmentAckFlags{NegativeAck: false, SentByServer: true},
				InvokeId:         header.InvokeId,
				SequenceNumber:   header.SequenceNumber,
				ActualWindowSize: tr.ActualWindowSize,
			}
			segmentAckBytes, err := segmentAckPdu.Marshal()
			if err != nil {
				logger.Error("could not marshal segment-ack-pdu: ", err)
				tr.applicationEntity.removeServerTransaction(tr.Id)
				return
			}
			tr.applicationEntity.networkEntity.NUnitDataRequest(
				tr.Source, false, NormalPriority,
				segmentAckBytes,
			)

			tr.SegmentTimer.Start()

			segment := &Segment{
				SequenceNumber: header.SequenceNumber,
				data:           serviceRequest,
			}
			tr.segments = append(tr.segments, segment)
			tr.InitialSequenceNumber = 0
			tr.LastSequenceNumber = 0
			tr.DuplicateCount = 0
			tr.state = &ServerTransactionSegmentedRequestState{
				transaction: tr,
			}
		} else {
			// ConfirmedSegmentedReceivedWindowSizeOutOfRange
			abortHeader := AbortHeader{
				SentByServer: true,
				InvokeId:     uint8(header.InvokeId),
				AbortReason:  uint8(bacnet.AbortWindowSizeOutOfRange),
			}
			abortBytes, err := abortHeader.Marshal()
			if err != nil {
				logger.Error("could not marshal abort header: ", err)
			} else {
				err = tr.applicationEntity.networkEntity.NUnitDataRequest(indication.source, false, 0, abortBytes)
				if err != nil {
					logger.Error("could not send Abort PDU: ", err)
				}
			}
			tr.applicationEntity.removeServerTransaction(tr.Id)
		}
	} else {
		// UnexpectedPDU_Received
		err := tr.applicationEntity.networkEntity.NReleaseRequest(indication.source)
		if err != nil {
			logger.Error("N-RELEASE.request failed: ", err)
		}
		abortHeader := AbortHeader{
			SentByServer: true,
			InvokeId:     uint8(header.InvokeId),
			AbortReason:  uint8(bacnet.AbortOther),
		}
		abortBytes, err := abortHeader.Marshal()
		if err != nil {
			logger.Error("could not marshal abort header: ", err)
		}
		err = tr.applicationEntity.networkEntity.NUnitDataRequest(indication.source, false, 0, abortBytes)
		if err != nil {
			logger.Error("could not send Abort PDU: ", err)
		}
	}
}

func (s *ServerTransactionIdleState) OnSegmentTimerFired() {
	// nothing to do
}

func (s *ServerTransactionIdleState) OnRequestTimerFired() {
	// nothing to do
}

////////////////////////////////////////////////////////////////////////////////
// SEGMENTED_REQUEST State
////////////////////////////////////////////////////////////////////////////////

type ServerTransactionSegmentedRequestState struct {
	transaction *ServerTransaction
}

func (s *ServerTransactionSegmentedRequestState) HandleUnconfirmedServiceRequestPdu() {}

func (s *ServerTransactionSegmentedRequestState) HandleSegmentAckPdu(*SegmentAckHeader) {}

func (s *ServerTransactionSegmentedRequestState) HandleAbortPdu(int) {}

func (s *ServerTransactionSegmentedRequestState) HandleConfirmedServiceRequestPdu(
	indication *NPDUIndication,
	header *ConfirmedServiceRequestHeader,
	data []byte,
) {
	tr := s.transaction
	// TODO: check that the data_attributes are the same than the first segment
	if header.Flags.SegmentedRequest {
		if header.SequenceNumber == (tr.LastSequenceNumber+1)%256 {
			segment := &Segment{
				SequenceNumber: header.SequenceNumber,
				data:           data,
			}
			tr.segments = append(tr.segments, segment)
			tr.LastSequenceNumber = (tr.LastSequenceNumber + 1) % 256
			if header.Flags.MoreSegments {
				if header.SequenceNumber != (tr.InitialSequenceNumber+tr.ActualWindowSize)%256 {
					// NewSegmentReceived
					alreadyFired := tr.SegmentTimer.Restart(false)
					if alreadyFired {
						logger.Trace("confirm service request PDU handling: segment timer had already fired")
					}
					err := tr.applicationEntity.networkEntity.NReleaseRequest(tr.Source)
					if err != nil {
						logger.Error("could not send N-RELEASE.request to network layer: ", err)
					}
					return
				} else {
					// LastSegmentOfGroupReceived
					header := SegmentAckHeader{
						Flags:            SegmentAckFlags{NegativeAck: false, SentByServer: true},
						InvokeId:         tr.Id.InvokeId,
						SequenceNumber:   tr.LastSequenceNumber,
						ActualWindowSize: tr.ActualWindowSize,
					}
					headerBytes, err := header.Marshal()
					if err != nil {
						logger.Error("could not marshal segment-ack pdu: ", err)
						return
					}
					err = tr.applicationEntity.networkEntity.NUnitDataRequest(
						tr.Source, false, NormalPriority, headerBytes)
					if err != nil {
						logger.Error("error when sending segment-ack-pdu: ", err)
					}
					// TODO: Check return value
					tr.SegmentTimer.Restart(false)
					tr.InitialSequenceNumber = tr.LastSequenceNumber
					tr.DuplicateCount = 0
					return
				}
			} else {
				// LastSegmentOfMessageReceived
				tr.SegmentTimer.Stop()
				header := SegmentAckHeader{
					Flags:            SegmentAckFlags{NegativeAck: false, SentByServer: true},
					InvokeId:         tr.Id.InvokeId,
					SequenceNumber:   tr.LastSequenceNumber,
					ActualWindowSize: tr.ActualWindowSize,
				}
				headerBytes, err := header.Marshal()
				if err != nil {
					logger.Error("could not marshal segment-ack pdu: ", err)
					return
				}
				err = tr.applicationEntity.networkEntity.NUnitDataRequest(
					tr.Source, false, NormalPriority, headerBytes)
				if err != nil {
					logger.Error("error when sending segment-ack-pdu: ", err)
					return
				}
				tr.InitialSequenceNumber = tr.LastSequenceNumber
				// TODO: send CONF_SERV.indication
				tr.RequestTimer.Start()
				tr.state = &ServerTransactionAwaitResponseState{
					transaction: tr,
				}
			}
		} else {
			// Check DuplicateWindow()
			if tr.DuplicateInWindow(header.SequenceNumber) {
				if tr.DuplicateCount < Ndup {
					// DuplicateSegmentReceived
					// TODO: check returned value
					tr.SegmentTimer.Restart(false)
					tr.DuplicateCount++
				} else {
					// TooManyDuplicateSegmentsReceived
					header := SegmentAckHeader{
						Flags:            SegmentAckFlags{NegativeAck: true, SentByServer: true},
						InvokeId:         tr.Id.InvokeId,
						SequenceNumber:   tr.LastSequenceNumber,
						ActualWindowSize: tr.ActualWindowSize,
					}
					headerBytes, err := header.Marshal()
					if err != nil {
						logger.Error("could not marshal segment-ack pdu: ", err)
						return
					}
					err = tr.applicationEntity.networkEntity.NUnitDataRequest(
						tr.Source, false, NormalPriority, headerBytes)
					if err != nil {
						logger.Error("error when sending segment-ack-pdu: ", err)
						return
					}
					// TODO: check returned value
					tr.SegmentTimer.Restart(false)
					tr.InitialSequenceNumber = tr.LastSequenceNumber
					tr.DuplicateCount = 0
				}
			} else {
				// SegmentReceivedOutOfOrder
				// Drop segment
				header := SegmentAckHeader{
					Flags:            SegmentAckFlags{NegativeAck: true, SentByServer: true},
					InvokeId:         tr.Id.InvokeId,
					SequenceNumber:   tr.LastSequenceNumber,
					ActualWindowSize: tr.ActualWindowSize,
				}
				headerBytes, err := header.Marshal()
				if err != nil {
					logger.Error("could not marshal segment-ack pdu: ", err)
					return
				}
				err = tr.applicationEntity.networkEntity.NUnitDataRequest(
					tr.Source, false, NormalPriority, headerBytes)
				if err != nil {
					logger.Error("error when sending segment-ack-pdu: ", err)
					return
				}
				// TODO: check returned value
				tr.SegmentTimer.Restart(false)
				tr.InitialSequenceNumber = tr.LastSequenceNumber
				tr.DuplicateCount = 0
			}
		}
	} else {
		// TODO check security parameters
		// UnexpectedPDU_Received
		tr.SegmentTimer.Stop()
		header := &AbortHeader{
			SentByServer: true,
			InvokeId:     uint8(tr.Id.InvokeId),
			AbortReason:  uint8(bacnet.AbortInvalidApduInThisState),
		}
		headerBytes, err := header.Marshal()
		if err != nil {
			logger.Error("could not marshal abort-pdu: ", err)
			return
		}
		err = tr.applicationEntity.networkEntity.NUnitDataRequest(
			tr.Source, false, NormalPriority, headerBytes)
		if err != nil {
			logger.Error("error when sending abort-pdu: ", err)
			return
		}
		tr.applicationEntity.removeServerTransaction(tr.Id)
	}
}

func (s *ServerTransactionSegmentedRequestState) OnSegmentTimerFired() {
	// Timeout
	tr := s.transaction
	tr.applicationEntity.removeServerTransaction(tr.Id)
}

func (s *ServerTransactionSegmentedRequestState) OnRequestTimerFired() {
	// nothing to do
}

////////////////////////////////////////////////////////////////////////////////
// AWAIT_RESPONSE State
////////////////////////////////////////////////////////////////////////////////

type ServerTransactionAwaitResponseState struct {
	transaction *ServerTransaction
}

func (s *ServerTransactionAwaitResponseState) HandleUnconfirmedServiceRequestPdu() {}

func (s *ServerTransactionAwaitResponseState) HandleSegmentAckPdu(*SegmentAckHeader) {}

func (s *ServerTransactionAwaitResponseState) HandleAbortPdu(int) {}

func (s *ServerTransactionAwaitResponseState) HandleConfirmedServiceRequestPdu(
	indication *NPDUIndication,
	header *ConfirmedServiceRequestHeader,
	serviceRequest []byte,
) {
	if header.Flags.SegmentedRequest {
		// DuplicateSegmentReceived
		tr := s.transaction
		segmentAckPdu := SegmentAckHeader{
			Flags:            SegmentAckFlags{NegativeAck: false, SentByServer: true},
			SequenceNumber:   tr.LastSequenceNumber,
			ActualWindowSize: tr.ActualWindowSize,
		}
		segmentAckBytes, err := segmentAckPdu.Marshal()
		if err != nil {
			logger.Error("could not marshal segment-ack-pdu: ", err)
			tr.applicationEntity.removeServerTransaction(tr.Id)
			return
		}
		tr.applicationEntity.networkEntity.NUnitDataRequest(
			tr.Source, false, NormalPriority, segmentAckBytes,
		)
	}
}

func (s *ServerTransactionAwaitResponseState) OnSegmentTimerFired() {
	// nothing to do
}

func (s *ServerTransactionAwaitResponseState) OnRequestTimerFired() {
	// Timeout
	tr := s.transaction

	abortPdu := AbortHeader{
		SentByServer: true,
		InvokeId:     uint8(tr.Id.InvokeId),
		AbortReason:  uint8(bacnet.AbortApplicationExceededReplyTime),
	}
	abortBytes, err := abortPdu.Marshal()
	if err != nil {
		logger.Error("could not marshal abort-pdu: ", err)
		tr.applicationEntity.removeServerTransaction(tr.Id)
		return
	}
	tr.applicationEntity.networkEntity.NUnitDataRequest(
		tr.Source, false, NormalPriority,
		abortBytes,
	)

	// TODO send ABORT.indication to the user layer
}

////////////////////////////////////////////////////////////////////////////////
// SEGMENTED_RESPONSE State
////////////////////////////////////////////////////////////////////////////////

type ServerTransactionSegmentedResponseState struct {
	transaction *ServerTransaction
}

func (s *ServerTransactionSegmentedResponseState) HandleUnconfirmedServiceRequest() {}

func (s *ServerTransactionSegmentedResponseState) HandleSegmentAckPdu(*SegmentAckHeader) {}

func (s *ServerTransactionSegmentedResponseState) HandleAbort() {}

func (s *ServerTransactionSegmentedResponseState) HandleConfirmedServiceRequest(
	indication *NPDUIndication,
	header *ConfirmedServiceRequestHeader,
	serviceRequest []byte,
) {
	// UnexpectedPDU_Received
	// TODO: check security parameters identical to initial PDU
	tr := s.transaction
	abortPdu := &AbortHeader{
		SentByServer: true,
		InvokeId:     uint8(tr.Id.InvokeId),
		AbortReason:  uint8(bacnet.Other),
	}
	abortBytes, err := abortPdu.Marshal()
	if err != nil {
		logger.Error("could not marshal abort-pdu: ", err)
		return
	}
	err = tr.applicationEntity.networkEntity.NUnitDataRequest(
		tr.Source, false, NormalPriority, abortBytes)
	if err != nil {
		logger.Error("error when sending abort-pdu: ", err)
		return
	}
	tr.applicationEntity.removeServerTransaction(tr.Id)
	tr.SegmentTimer.Stop()
}

func (s *ServerTransactionSegmentedResponseState) OnSegmentTimerFired() {
	tr := s.transaction
	if tr.SegmentRetryCount < tr.NumberOfApduRetries {
		// Timeout
		// TODO: send multiple complex ack to fill send window
		// TODO: check returned value
		tr.SegmentTimer.Restart(false)
	} else {
		// FinalTimeout
		tr.SegmentTimer.Stop()
		tr.applicationEntity.removeServerTransaction(tr.Id)
	}
}

func (s *ServerTransactionSegmentedResponseState) OnRequestTimerFired() {
	// nothing to do
}
