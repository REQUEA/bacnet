package bacnet

import (
	"bacnet/internal/encoding"
	"bacnet/internal/types"
	"bytes"
	"fmt"
)

type WhoIs struct {
	Low, High *uint32 //may be null if we want to check all range
}

func (w WhoIs) MarshalBinary() ([]byte, error) {
	buf := &bytes.Buffer{}
	if w.Low != nil && w.High != nil {
		if *w.Low > types.MaxInstance || *w.High > types.MaxInstance {
			return nil, fmt.Errorf("invalid WhoIs range: [%d, %d]: max value is %d", *w.Low, *w.High, types.MaxInstance)
		}
		if *w.Low > *w.High {
			return nil, fmt.Errorf("invalid WhoIs range: [%d, %d]: low limit is higher than high limit", *w.Low, *w.High)
		}
		encoding.ContextUnsigned(buf, 0, *w.Low)
		encoding.ContextUnsigned(buf, 1, *w.High)
	}
	return buf.Bytes(), nil
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
	buf := &bytes.Buffer{}
	err := encoding.EncodeAppData(buf, iam.ObjectID)
	if err != nil {
		return nil, err
	}
	err = encoding.EncodeAppData(buf, iam.MaxApduLength)
	if err != nil {
		return nil, err
	}
	err = encoding.EncodeAppData(buf, iam.SegmentationSupport)
	if err != nil {
		return nil, err
	}
	err = encoding.EncodeAppData(buf, iam.VendorID)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (iam *Iam) UnmarshalBinary(data []byte) error {
	buf := bytes.NewBuffer(data)
	err := encoding.DecodeAppData(buf, &iam.ObjectID)
	if err != nil {
		return fmt.Errorf("decode iam objectID: %w", err)
	}
	err = encoding.DecodeAppData(buf, &iam.MaxApduLength)
	if err != nil {
		return fmt.Errorf("decode iam MaxAPDU: %w", err)
	}
	err = encoding.DecodeAppData(buf, &iam.SegmentationSupport)
	if err != nil {
		return fmt.Errorf("decode iam SegmentationSupport: %w", err)
	}
	err = encoding.DecodeAppData(buf, &iam.VendorID)
	if err != nil {
		return fmt.Errorf("decode iam VendorID: %w", err)
	}

	return nil
}
