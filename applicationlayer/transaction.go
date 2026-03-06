package applicationlayer

import (
	"fmt"
	"sync"
	"time"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/logger"
	"github.com/REQUEA/bacnet/networklayer"
	"github.com/REQUEA/bacnet/objectmodel"
)

type TransactionID struct {
	Address  *bacnet.BACnetAddress
	InvokeID uint
}

func NewTransactionID(addr *bacnet.BACnetAddress, invokeID uint) TransactionID {
	return TransactionID{
		Address:  addr,
		InvokeID: invokeID,
	}
}

func (i *TransactionID) Equal(o *TransactionID) bool {
	if i.Address != nil && o.Address != nil {
		return i.InvokeID == o.InvokeID && i.Address.Equal(o.Address)
	}
	return i.InvokeID == o.InvokeID && i.Address != o.Address
}

func (i *TransactionID) String() string {
	return fmt.Sprintf("{ Address: %s, InvokeID: %d }", i.Address.String(), i.InvokeID)
}

func (t *Transaction) DuplicateInWindow(seqA uint) bool {
	initialSequenceNumber := t.InitialSequenceNumber % 256
	receivedCount := (t.LastSequenceNumber - initialSequenceNumber) % 256
	if receivedCount == 0 {
		return false
	}
	if receivedCount > ((seqA - initialSequenceNumber) % 256) {
		return true
	}
	return false
}

type Segment struct {
	SequenceNumber uint
	data           []byte
}

type TransactionTimer struct {
	sync.Mutex
	timer     *time.Timer
	value     time.Duration
	stopped   bool
	restarted bool
	onTimeout func()
}

func NewTransactionTimer(value time.Duration, onTimeout func()) *TransactionTimer {
	return &TransactionTimer{
		timer:     nil,
		value:     value,
		stopped:   false,
		restarted: false,
		onTimeout: onTimeout,
	}
}

func (t *TransactionTimer) Start() {
	if t.timer != nil {
		logger.Error("transaction timer already started")
		return
	}
	t.timer = time.AfterFunc(t.value, func() {
		t.Lock()
		defer t.Unlock()
		if t.stopped {
			return
		}
		if t.restarted {
			// Restart was called while this routine was started
			t.restarted = false
			return
		}
		t.onTimeout()
		t.stopped = true
	})
}

func (t *TransactionTimer) Stop() {
	t.Lock()
	defer t.Unlock()
	if t.timer != nil {
		t.timer.Stop()
	}
	t.stopped = true
}

// Reset stops any running timer and clears internal state, allowing Start to be called again.
func (t *TransactionTimer) Reset() {
	t.Lock()
	defer t.Unlock()
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
	t.stopped = false
	t.restarted = false
}

func (t *TransactionTimer) Restart(force bool) bool {
	t.Lock()
	defer t.Unlock()
	result := !t.stopped
	if result || force {
		t.timer.Reset(t.value)
		t.restarted = true
		result = true
	}
	return result
}

type TransactionEvent interface {
	Exec()
}

type Transaction struct {
	ID                    *TransactionID
	Device                *objectmodel.Device
	Source                *bacnet.BACnetAddress
	Dest                  *bacnet.BACnetAddress
	Priority              networklayer.NPDUPriority
	RetryCount            int
	SegmentRetryCount     uint
	DuplicateCount        int
	SentAllSegments       bool
	LastSequenceNumber    uint
	InitialSequenceNumber uint
	ActualWindowSize      uint
	ProposedWindowSize    uint
	SegmentTimer          *TransactionTimer
	RequestTimer          *TransactionTimer
	// transaction constants
	MaxSegmentsAccepted uint
	NumberOfApduRetries uint
	applicationEntity   *ApplicationEntity
	serviceLayer        ServiceHandler
	segments            []*Segment
	events              chan TransactionEvent
}

func (t *Transaction) InWindow(seqA, seqB uint) bool {
	return ((seqA - seqB) % 256) < t.ActualWindowSize
}

func (t *Transaction) Start() {
	go func() {
		for e := range t.events {
			e.Exec()
		}
		logger.Trace("end of transaction ", t.ID.String())
	}()
}

func (t *Transaction) PushEvent(e TransactionEvent) {
	t.events <- e
}
