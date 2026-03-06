package applicationlayer

import (
	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/logger"
	"github.com/REQUEA/bacnet/networklayer"
)

type ServerTransactionState interface {
	HandleUnconfirmedServiceRequestPdu()
	HandleConfirmedServiceRequestPdu(*networklayer.NPDUIndication, *ConfirmedServiceRequestHeader, []byte)
	HandleSegmentAckPdu(*SegmentAckHeader)
	HandleAbortPdu(int)
	OnSegmentTimerFired()
	OnRequestTimerFired()
}

type ServerTransaction struct {
	Transaction
	state          ServerTransactionState
	responsePdu    []byte
	requestOffset  int
	maxPduLength   int
	serviceChoice  bacnet.BACnetConfirmedServiceChoice
	lastSentSeqNum uint
}

func NewServerTransaction(id *TransactionID) *ServerTransaction {
	result := &ServerTransaction{
		Transaction: Transaction{
			ID:     id,
			events: make(chan TransactionEvent, 16),
		},
	}
	result.state = &ServerTransactionIdleState{
		transaction: result,
	}
	return result
}

func (t *ServerTransaction) HandleConfirmedServiceRequestPdu(
	indication *networklayer.NPDUIndication,
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
	indication *networklayer.NPDUIndication,
	header *ConfirmedServiceRequestHeader,
	serviceRequest []byte,
) {
	tr := s.transaction
	if !header.Flags.SegmentedRequest {
		// ConfirmedUnsegmentedReceived: transition to AwaitResponse BEFORE calling the service
		// layer so that SendConfServResponse (called from within HandleConfServIndication) can
		// change the state further without it being overwritten on return.
		apduInd := APDUIndication{
			Source:        indication.Source,
			ExpectedReply: indication.ExpectedReply,
			InvokeID:      tr.ID.InvokeID,
			Data:          serviceRequest,
		}
		tr.RequestTimer.Start()
		tr.state = &ServerTransactionAwaitResponseState{transaction: tr}
		tr.serviceLayer.HandleConfServIndication(&apduInd, header.ServiceChoice)
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
				InvokeID:         header.InvokeID,
				SequenceNumber:   header.SequenceNumber,
				ActualWindowSize: tr.ActualWindowSize,
			}
			segmentAckBytes, err := segmentAckPdu.Marshal()
			if err != nil {
				logger.Error("could not marshal segment-ack-pdu: ", err)
				tr.applicationEntity.removeServerTransaction(tr.ID)
				return
			}
			if err := tr.applicationEntity.networkEntity.NUnitDataRequest(
				tr.Source, false, networklayer.NormalPriority,
				segmentAckBytes,
			); err != nil {
				logger.Error("could not send segment-ack: ", err)
			}

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
				InvokeID:     uint8(header.InvokeID), //nolint:gosec
				AbortReason:  uint8(bacnet.AbortWindowSizeOutOfRange),
			}
			abortBytes, err := abortHeader.Marshal()
			if err != nil {
				logger.Error("could not marshal abort header: ", err)
			} else {
				err = tr.applicationEntity.networkEntity.NUnitDataRequest(indication.Source, false, 0, abortBytes)
				if err != nil {
					logger.Error("could not send Abort PDU: ", err)
				}
			}
			tr.applicationEntity.removeServerTransaction(tr.ID)
		}
	} else {
		// UnexpectedPDU_Received
		err := tr.applicationEntity.networkEntity.NReleaseRequest(indication.Source)
		if err != nil {
			logger.Error("N-RELEASE.request failed: ", err)
		}
		abortHeader := AbortHeader{
			SentByServer: true,
			InvokeID:     uint8(header.InvokeID), //nolint:gosec
			AbortReason:  uint8(bacnet.AbortOther),
		}
		abortBytes, err := abortHeader.Marshal()
		if err != nil {
			logger.Error("could not marshal abort header: ", err)
		}
		err = tr.applicationEntity.networkEntity.NUnitDataRequest(indication.Source, false, 0, abortBytes)
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
	indication *networklayer.NPDUIndication,
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
				}
				// LastSegmentOfGroupReceived
				header := SegmentAckHeader{
					Flags:            SegmentAckFlags{NegativeAck: false, SentByServer: true},
					InvokeID:         tr.ID.InvokeID,
					SequenceNumber:   tr.LastSequenceNumber,
					ActualWindowSize: tr.ActualWindowSize,
				}
				headerBytes, err := header.Marshal()
				if err != nil {
					logger.Error("could not marshal segment-ack pdu: ", err)
					return
				}
				err = tr.applicationEntity.networkEntity.NUnitDataRequest(
					tr.Source, false, networklayer.NormalPriority, headerBytes)
				if err != nil {
					logger.Error("error when sending segment-ack-pdu: ", err)
				}
				// TODO: Check return value
				tr.SegmentTimer.Restart(false)
				tr.InitialSequenceNumber = tr.LastSequenceNumber
				tr.DuplicateCount = 0
				return
			}
			// LastSegmentOfMessageReceived
			tr.SegmentTimer.Stop()
			segmentAckHeader := SegmentAckHeader{
				Flags:            SegmentAckFlags{NegativeAck: false, SentByServer: true},
				InvokeID:         tr.ID.InvokeID,
				SequenceNumber:   tr.LastSequenceNumber,
				ActualWindowSize: tr.ActualWindowSize,
			}
			headerBytes, err := segmentAckHeader.Marshal()
			if err != nil {
				logger.Error("could not marshal segment-ack pdu: ", err)
				return
			}
			err = tr.applicationEntity.networkEntity.NUnitDataRequest(
				tr.Source, false, networklayer.NormalPriority, headerBytes)
			if err != nil {
				logger.Error("error when sending segment-ack-pdu: ", err)
				return
			}
			tr.InitialSequenceNumber = tr.LastSequenceNumber
			// Reassemble all segments for CONF_SERV.indication
			var fullData []byte
			for _, seg := range tr.segments {
				fullData = append(fullData, seg.data...)
			}
			apduInd := APDUIndication{
				Source:        indication.Source,
				ExpectedReply: indication.ExpectedReply,
				InvokeID:      tr.ID.InvokeID,
				Data:          fullData,
			}
			// Transition to AwaitResponse BEFORE calling the service layer so that
			// SendConfServResponse (called from within HandleConfServIndication) can
			// change the state further without being overwritten on return.
			tr.RequestTimer.Start()
			tr.state = &ServerTransactionAwaitResponseState{transaction: tr}
			tr.serviceLayer.HandleConfServIndication(&apduInd, header.ServiceChoice)
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
						InvokeID:         tr.ID.InvokeID,
						SequenceNumber:   tr.LastSequenceNumber,
						ActualWindowSize: tr.ActualWindowSize,
					}
					headerBytes, err := header.Marshal()
					if err != nil {
						logger.Error("could not marshal segment-ack pdu: ", err)
						return
					}
					err = tr.applicationEntity.networkEntity.NUnitDataRequest(
						tr.Source, false, networklayer.NormalPriority, headerBytes)
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
					InvokeID:         tr.ID.InvokeID,
					SequenceNumber:   tr.LastSequenceNumber,
					ActualWindowSize: tr.ActualWindowSize,
				}
				headerBytes, err := header.Marshal()
				if err != nil {
					logger.Error("could not marshal segment-ack pdu: ", err)
					return
				}
				err = tr.applicationEntity.networkEntity.NUnitDataRequest(
					tr.Source, false, networklayer.NormalPriority, headerBytes)
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
			InvokeID:     uint8(tr.ID.InvokeID), //nolint:gosec
			AbortReason:  uint8(bacnet.AbortInvalidApduInThisState),
		}
		headerBytes, err := header.Marshal()
		if err != nil {
			logger.Error("could not marshal abort-pdu: ", err)
			return
		}
		err = tr.applicationEntity.networkEntity.NUnitDataRequest(
			tr.Source, false, networklayer.NormalPriority, headerBytes)
		if err != nil {
			logger.Error("error when sending abort-pdu: ", err)
			return
		}
		tr.applicationEntity.removeServerTransaction(tr.ID)
	}
}

func (s *ServerTransactionSegmentedRequestState) OnSegmentTimerFired() {
	// Timeout
	tr := s.transaction
	tr.applicationEntity.removeServerTransaction(tr.ID)
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
	_ *networklayer.NPDUIndication,
	header *ConfirmedServiceRequestHeader,
	_ []byte,
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
			tr.applicationEntity.removeServerTransaction(tr.ID)
			return
		}
		if err := tr.applicationEntity.networkEntity.NUnitDataRequest(
			tr.Source, false, networklayer.NormalPriority, segmentAckBytes,
		); err != nil {
			logger.Error("could not send segment-ack: ", err)
		}
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
		InvokeID:     uint8(tr.ID.InvokeID), //nolint:gosec
		AbortReason:  uint8(bacnet.AbortApplicationExceededReplyTime),
	}
	abortBytes, err := abortPdu.Marshal()
	if err != nil {
		logger.Error("could not marshal abort-pdu: ", err)
		tr.applicationEntity.removeServerTransaction(tr.ID)
		return
	}
	if err := tr.applicationEntity.networkEntity.NUnitDataRequest(
		tr.Source, false, networklayer.NormalPriority,
		abortBytes,
	); err != nil {
		logger.Error("could not send abort: ", err)
	}

	// TODO send ABORT.indication to the user layer
	indication := APDUIndication{
		Source:        tr.Source,
		ExpectedReply: false,
	}
	tr.serviceLayer.HandleAbortIndication(&indication, 0, uint8(bacnet.AbortApplicationExceededReplyTime))
	// TODO: discard transaction?
}

////////////////////////////////////////////////////////////////////////////////
// SEGMENTED_RESPONSE State
////////////////////////////////////////////////////////////////////////////////

type ServerTransactionSegmentedResponseState struct {
	transaction *ServerTransaction
}

func (s *ServerTransactionSegmentedResponseState) HandleUnconfirmedServiceRequestPdu() {}

// sendNextSegments sends up to ActualWindowSize ComplexAck segments starting from
// tr.requestOffset / tr.InitialSequenceNumber. It does NOT advance requestOffset or ISN
// (those are updated only when a positive SegmentAck is received), so retransmitting the
// current window is achieved simply by calling sendNextSegments again.
func (s *ServerTransactionSegmentedResponseState) sendNextSegments() {
	tr := s.transaction
	seqNum := tr.InitialSequenceNumber
	offset := tr.requestOffset

	for i := uint(0); i < tr.ActualWindowSize; i++ {
		end := offset + tr.maxPduLength
		if end > len(tr.responsePdu) {
			end = len(tr.responsePdu)
		}
		moreSegments := end < len(tr.responsePdu)
		segData := tr.responsePdu[offset:end]

		header := ComplexAckHeader{
			Flags: ComplexAckFlags{
				SegmentedRequest: true,
				MoreSegments:     moreSegments,
			},
			InvokeID:           tr.ID.InvokeID,
			SequenceNumber:     seqNum,
			ProposedWindowSize: tr.ActualWindowSize,
			ServiceAckChoice:   int(tr.serviceChoice), //nolint:gosec
		}
		headerBytes, err := header.Marshal()
		if err != nil {
			logger.Error("could not marshal ComplexAck segment header: ", err)
			return
		}
		pdu := append(headerBytes, segData...)
		err = tr.applicationEntity.networkEntity.NUnitDataRequest(
			tr.Source, false, networklayer.NormalPriority, pdu)
		if err != nil {
			logger.Error("could not send ComplexAck segment: ", err)
		}

		if !moreSegments {
			tr.SentAllSegments = true
			tr.lastSentSeqNum = seqNum
			break
		}
		seqNum = (seqNum + 1) % 256
		offset = end
	}

	tr.SegmentTimer.Reset()
	tr.SegmentTimer.Start()
}

func (s *ServerTransactionSegmentedResponseState) HandleSegmentAckPdu(header *SegmentAckHeader) {
	tr := s.transaction
	if !header.Flags.NegativeAck {
		// Positive ack
		if tr.SentAllSegments && header.SequenceNumber == tr.lastSentSeqNum {
			// Final ack: all segments delivered
			tr.SegmentTimer.Stop()
			tr.applicationEntity.removeServerTransaction(tr.ID)
			return
		}
		// Advance window: compute how many segments were acked
		nackedSegs := int((header.SequenceNumber-tr.InitialSequenceNumber+256)%256) + 1 //nolint:gosec
		tr.requestOffset += nackedSegs * tr.maxPduLength
		if tr.requestOffset > len(tr.responsePdu) {
			tr.requestOffset = len(tr.responsePdu)
		}
		tr.InitialSequenceNumber = (header.SequenceNumber + 1) % 256
		tr.SegmentRetryCount = 0
		s.sendNextSegments()
	} else {
		// Negative ack: retransmit current window from ISN
		s.sendNextSegments()
	}
}

func (s *ServerTransactionSegmentedResponseState) HandleAbortPdu(int) {}

func (s *ServerTransactionSegmentedResponseState) HandleConfirmedServiceRequestPdu(
	_ *networklayer.NPDUIndication,
	_ *ConfirmedServiceRequestHeader,
	_ []byte,
) {
	// UnexpectedPDU_Received
	// TODO: check security parameters identical to initial PDU
	tr := s.transaction
	abortPdu := &AbortHeader{
		SentByServer: true,
		InvokeID:     uint8(tr.ID.InvokeID), //nolint:gosec
		AbortReason:  uint8(bacnet.Other),
	}
	abortBytes, err := abortPdu.Marshal()
	if err != nil {
		logger.Error("could not marshal abort-pdu: ", err)
		return
	}
	err = tr.applicationEntity.networkEntity.NUnitDataRequest(
		tr.Source, false, networklayer.NormalPriority, abortBytes)
	if err != nil {
		logger.Error("error when sending abort-pdu: ", err)
		return
	}
	tr.applicationEntity.removeServerTransaction(tr.ID)
	tr.SegmentTimer.Stop()
}

func (s *ServerTransactionSegmentedResponseState) OnSegmentTimerFired() {
	tr := s.transaction
	if tr.SegmentRetryCount < tr.NumberOfApduRetries {
		// Timeout: retransmit current window
		tr.SegmentRetryCount++
		s.sendNextSegments()
	} else {
		// FinalTimeout
		tr.SegmentTimer.Stop()
		tr.applicationEntity.removeServerTransaction(tr.ID)
	}
}

func (s *ServerTransactionSegmentedResponseState) OnRequestTimerFired() {
	// nothing to do
}
