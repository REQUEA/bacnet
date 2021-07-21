package main

import (
	"bytes"
	"fmt"
)

const MaxInstance = 0x3FFFFF

type WhoIs struct {
	low, high *uint //may be null if we want to check all range
}

func (w WhoIs) MarshalBinary() ([]byte, error) {
	buf := &bytes.Buffer{}
	if w.low != nil && w.high != nil {
		if *w.low > MaxInstance || *w.high > MaxInstance {
			return nil, fmt.Errorf("Invalid WhoIs range: [%d, %d]: max value is %d", *w.low, *w.high, MaxInstance)
		}
		if *w.low > *w.high {
			return nil, fmt.Errorf("Invalid WhoIs range: [%d, %d]: low limit is higher than high limit", *w.low, *w.high)
		}
		contextUnsigned(buf, 0, uint32(*w.low))
		contextUnsigned(buf, 1, uint32(*w.high))
	}
	return buf.Bytes(), nil
}

func (w *WhoIs) UnmarshalBinary([]byte) error {
	panic("not iplemented")
}
