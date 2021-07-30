package bacnet

import (
	"context"
	"sync"
)

type Tx struct {
	Bvlc chan<- BVLC
	Ctx  context.Context
}
type Transactions struct {
	sync.Mutex
	//TODO: maybe chan of apdu ?
	currents     map[byte]Tx
	freeInvokeID chan byte
}

func NewTransactions() *Transactions {
	t := Transactions{
		currents:     map[byte]Tx{},
		freeInvokeID: make(chan byte, 256), //The chan should be able to handle all possible values
	}
	for x := 0; x < 256; x++ {
		t.freeInvokeID <- byte(x)
	}
	return &t
}

//GetID returns a free InvokeID to use fr a Confirmed service
//request. Blocks until such ID is available
func (t *Transactions) GetID() byte {
	return <-t.freeInvokeID
}

//FreeID puts back the id in the pool of available invoke ID
func (t *Transactions) FreeID(id byte) {
	t.freeInvokeID <- id
}

//nolint: revive
//SetTransaction set up the channel passed as parameter as a callback for the bacnet response.
//All call to SetTransaction must be followed by a StopTransaction to prevent leaks
func (t *Transactions) SetTransaction(id byte, bvlc chan<- BVLC, ctx context.Context) {
	t.Lock()
	defer t.Unlock()
	t.currents[id] = Tx{
		Bvlc: bvlc,
		Ctx:  ctx,
	}
}

func (t *Transactions) StopTransaction(id byte) {
	t.Lock()
	defer t.Unlock()
	delete(t.currents, id)
}

func (t *Transactions) GetTransaction(id byte) (Tx, bool) {
	t.Lock()
	defer t.Unlock()
	c, ok := t.currents[id]
	return c, ok
}
