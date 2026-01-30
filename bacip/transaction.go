package bacip

import (
	"fmt"
	"sync"
	"time"

	"github.com/REQUEA/bacnet"
)

type TransactionId struct {
	Address  *bacnet.BACnetAddress
	InvokeId uint
}

func NewTransactionId(addr *bacnet.BACnetAddress, invokeId uint) TransactionId {
	return TransactionId{
		Address:  addr,
		InvokeId: invokeId,
	}
}

func (i *TransactionId) Equal(o *TransactionId) bool {
	if i.Address != nil && o.Address != nil {
		return i.InvokeId == o.InvokeId && i.Address.Equal(o.Address)
	}
	return i.InvokeId == o.InvokeId && i.Address != o.Address
}

func (i *TransactionId) String() string {
	return fmt.Sprintf("{ Address: %s, InvokeId: %d }", i.Address.String(), i.InvokeId)
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
	timer      *time.Timer
	value      time.Duration
	genCounter int
	stopped    bool
	restarted  bool
	onTimeout  func()
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
	t.timer.Stop()
	t.stopped = true
}

func (t *TransactionTimer) Restart(force bool) bool {
	t.Lock()
	defer t.Unlock()
	result := true
	if t.stopped {
		// the timer has already fired.
		result = false
	}
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
	Id                    *TransactionId
	Device                *Device
	Source                *bacnet.BACnetAddress
	Dest                  *bacnet.BACnetAddress
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
	serviceLayer        *ServiceLayer
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
		logger.Trace("end of transaction ", t.Id.String())
	}()
}

func (t *Transaction) PushEvent(e TransactionEvent) {
	t.events <- e
}
