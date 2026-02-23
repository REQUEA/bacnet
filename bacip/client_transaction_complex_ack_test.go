package bacip

import (
    "testing"
    "time"

    "github.com/REQUEA/bacnet"
)

type mockNetworkEntity struct {
    called      bool
    lastDadr    *bacnet.BACnetAddress
    lastPayload []byte
}

func (m *mockNetworkEntity) NUnitDataIndication(source *Port, dadr bacnet.MAC, sadr bacnet.MAC, buf []byte) error {
    return nil
}
func (m *mockNetworkEntity) NUnitDataRequest(dadr *bacnet.BACnetAddress, der bool, priority NPDUPriority, payload []byte) error {
    m.called = true
    m.lastDadr = dadr
    m.lastPayload = make([]byte, len(payload))
    copy(m.lastPayload, payload)
    return nil
}
func (m *mockNetworkEntity) NReleaseRequest(dadr *bacnet.BACnetAddress) error { return nil }

func TestHandleComplexAckPdu_SegmentedRequest_SendsSegmentAckAndTransitions(t *testing.T) {
    id := &TransactionId{Address: &bacnet.BACnetAddress{}, InvokeId: 7}
    tr := NewClientTransaction(id)

    // timers: start them so Stop() won't panic
    tr.RequestTimer = NewTransactionTimer(time.Hour, func() {})
    tr.RequestTimer.Start()
    tr.SegmentTimer = NewTransactionTimer(time.Hour, func() {})

    tr.MaxSegmentsAccepted = 8
    tr.Id = id

    // application entity with mock network
    mockNet := &mockNetworkEntity{}
    ae := NewApplicationEntity()
    ae.networkEntity = mockNet
    tr.applicationEntity = ae

    // set state to AwaitConfirmation
    tr.state = &ClientTransactionAwaitConfirmationState{transaction: &tr}

    header := &ComplexAckHeader{
        Flags:              ComplexAckFlags{SegmentedRequest: true},
        InvokeId:           tr.Id.InvokeId,
        SequenceNumber:     0,
        ProposedWindowSize: 4,
    }
    indication := &NPDUIndication{source: &bacnet.BACnetAddress{}}

    s := &ClientTransactionAwaitConfirmationState{transaction: &tr}
    s.HandleComplexAckPdu(indication, header, nil)

    if !mockNet.called {
        t.Fatalf("expected network NUnitDataRequest to be called")
    }
    if _, ok := tr.state.(*ClientTransactionSegmentedConfState); !ok {
        t.Fatalf("expected state ClientTransactionSegmentedConfState, got %T", tr.state)
    }
    if tr.LastSequenceNumber != 0 || tr.InitialSequenceNumber != 0 || tr.DuplicateCount != 0 {
        t.Fatalf("expected sequence counters reset to 0 (got Last=%d Initial=%d Dup=%d)", tr.LastSequenceNumber, tr.InitialSequenceNumber, tr.DuplicateCount)
    }
    expected := min(tr.MaxSegmentsAccepted, header.ProposedWindowSize)
    if tr.ActualWindowSize != expected {
        t.Fatalf("expected ActualWindowSize %d, got %d", expected, tr.ActualWindowSize)
    }
}

func TestHandleComplexAckPdu_Unsegmented_RemovesClientTransaction(t *testing.T) {
    id := &TransactionId{Address: &bacnet.BACnetAddress{}, InvokeId: 9}
    tr := NewClientTransaction(id)

    // timers
    tr.RequestTimer = NewTransactionTimer(time.Hour, func() {})
    tr.RequestTimer.Start()

    ae := NewApplicationEntity()
    // register the transaction so removeClientTransaction will find it
    ae.ClientTransactions = append(ae.ClientTransactions, &tr)
    tr.applicationEntity = ae

    // set state
    tr.state = &ClientTransactionAwaitConfirmationState{transaction: &tr}

    header := &ComplexAckHeader{
        Flags:            ComplexAckFlags{SegmentedRequest: false},
        InvokeId:         tr.Id.InvokeId,
        SequenceNumber:   0,
        ProposedWindowSize: 0,
    }
    indication := &NPDUIndication{source: &bacnet.BACnetAddress{}}

    s := &ClientTransactionAwaitConfirmationState{transaction: &tr}
    s.HandleComplexAckPdu(indication, header, nil)

    if len(ae.ClientTransactions) == 0 || ae.ClientTransactions[0] != nil {
        t.Fatalf("expected transaction to be removed (entry set to nil) by removeClientTransaction")
    }
}
