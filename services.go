package bacnet

import (
	"bacnet/internal/encoding"
	"bacnet/internal/types"
	"fmt"
)

type WhoIs struct {
	Low, High *uint32 //may be null if we want to check all range
}

func (w WhoIs) MarshalBinary() ([]byte, error) {
	encoder := encoding.NewEncoder()
	if w.Low != nil && w.High != nil {
		if *w.Low > types.MaxInstance || *w.High > types.MaxInstance {
			return nil, fmt.Errorf("invalid WhoIs range: [%d, %d]: max value is %d", *w.Low, *w.High, types.MaxInstance)
		}
		if *w.Low > *w.High {
			return nil, fmt.Errorf("invalid WhoIs range: [%d, %d]: low limit is higher than high limit", *w.Low, *w.High)
		}
		encoder.ContextUnsigned(0, *w.Low)
		encoder.ContextUnsigned(1, *w.High)
	}
	return encoder.Bytes(), encoder.Error()
}

func (w *WhoIs) UnmarshalBinary(data []byte) error {
	if len(data) == 0 {
		// If data is empty, the whoIs request is a full range
		// check. So keep the low and high pointer nil
		return nil
	}
	w.Low = new(uint32)
	w.High = new(uint32)
	decoder := encoding.NewDecoder(data)
	decoder.ContextValue(byte(0), w.Low)
	decoder.ContextValue(byte(1), w.High)
	return decoder.Error()
}

type Iam struct {
	ObjectID            types.ObjectID
	MaxApduLength       uint32
	SegmentationSupport types.SegmentationSupport
	VendorID            uint32
}

func (iam Iam) MarshalBinary() ([]byte, error) {
	encoder := encoding.NewEncoder()
	encoder.EncodeAppData(iam.ObjectID)
	encoder.EncodeAppData(iam.MaxApduLength)
	encoder.EncodeAppData(iam.SegmentationSupport)
	encoder.EncodeAppData(iam.VendorID)
	return encoder.Bytes(), encoder.Error()
}

func (iam *Iam) UnmarshalBinary(data []byte) error {
	decoder := encoding.NewDecoder(data)
	decoder.DecodeAppData(&iam.ObjectID)
	decoder.DecodeAppData(&iam.MaxApduLength)
	decoder.DecodeAppData(&iam.SegmentationSupport)
	decoder.DecodeAppData(&iam.VendorID)
	return decoder.Error()
}
