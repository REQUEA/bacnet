package bacnet

import "sync"

type Transactions struct {
	sync.Mutex
	//TODO: maybe chan of apdu ?
	current      map[byte]chan<- BVLC
	freeInvokeID chan byte
}

func NewTransactions() *Transactions {
	t := Transactions{
		current:      map[byte]chan<- BVLC{},
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

func (t *Transactions) SetTransaction(id byte, channel chan<- BVLC) {
	t.Lock()
	defer t.Unlock()
	t.current[id] = channel
}

func (t *Transactions) StopTransaction(id byte) {
	t.Lock()
	defer t.Unlock()
	delete(t.current, id)
}

func (t *Transactions) GetTransaction(id byte) (chan<- BVLC, bool) {
	t.Lock()
	defer t.Unlock()
	c, ok := t.current[id]
	return c, ok
}
