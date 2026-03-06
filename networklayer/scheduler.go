package networklayer

import (
	"container/list"
	"fmt"
	"sync"
)

type Scheduler[T any] interface {
	Schedule(priority int, v T) error
	GetNext() (T, bool)
}

type WRRScheduler[T any] struct {
	sync.Mutex
	fifos           []Fifo[T]
	currentQueueIdx int
	currentCount    int
}

func NewWRRScheduler[T any](weights ...int) *WRRScheduler[T] {
	result := &WRRScheduler[T]{
		fifos: make([]Fifo[T], 0),
	}
	for _, weight := range weights {
		result.fifos = append(result.fifos, Fifo[T]{
			weight: weight,
		})
	}
	return result
}

func (w *WRRScheduler[T]) Schedule(priority int, v T) error {
	w.Lock()
	defer w.Unlock()
	if priority >= len(w.fifos) {
		return fmt.Errorf("priority not within acceptable range")
	}
	w.fifos[priority].Push(v)
	return nil
}

func (w *WRRScheduler[T]) GetNext() (T, bool) {
	w.Lock()
	defer w.Unlock()

	for range w.fifos {
		if w.currentCount < w.fifos[w.currentQueueIdx].weight && !w.fifos[w.currentQueueIdx].Empty() {
			w.currentCount++
			result, ok := w.fifos[w.currentQueueIdx].Pop()
			if !ok {
				// this should never happen, log?
				var zero T
				return zero, false
			}
			return result, true
		}
		w.currentCount = 0
		w.currentQueueIdx = (w.currentQueueIdx + 1) % len(w.fifos)
	}
	var zero T
	return zero, false
}

type Fifo[T any] struct {
	sync.Mutex
	backend list.List
	weight  int
}

func (f *Fifo[T]) Push(v T) {
	f.Lock()
	defer f.Unlock()
	f.backend.PushBack(v)
}

func (f *Fifo[T]) Pop() (T, bool) {
	f.Lock()
	defer f.Unlock()
	elt := f.backend.Front()
	if elt != nil {
		v := f.backend.Remove(elt)
		return v.(T), true
	}
	var zero T
	return zero, false
}

func (f *Fifo[T]) Empty() bool {
	f.Lock()
	defer f.Unlock()
	return f.backend.Len() == 0
}
