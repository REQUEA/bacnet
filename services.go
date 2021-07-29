package bacnet

import (
	"bacnet/internal/encoding"
	"bacnet/internal/types"
	"errors"
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
	encoder.AppData(iam.ObjectID)
	encoder.AppData(iam.MaxApduLength)
	encoder.AppData(iam.SegmentationSupport)
	encoder.AppData(iam.VendorID)
	return encoder.Bytes(), encoder.Error()
}

func (iam *Iam) UnmarshalBinary(data []byte) error {
	decoder := encoding.NewDecoder(data)
	decoder.AppData(&iam.ObjectID)
	decoder.AppData(&iam.MaxApduLength)
	decoder.AppData(&iam.SegmentationSupport)
	decoder.AppData(&iam.VendorID)
	return decoder.Error()
}

type ReadProperty struct {
	ObjectID types.ObjectID
	Property types.PropertyIdentifier
	//Data is here to contains the response
	Data interface{}
}

func (rp ReadProperty) MarshalBinary() ([]byte, error) {
	encoder := encoding.NewEncoder()
	encoder.ContextObjectID(0, rp.ObjectID)
	encoder.ContextUnsigned(1, rp.Property.Type)
	if rp.Property.ArrayIndex != nil {
		encoder.ContextUnsigned(2, *rp.Property.ArrayIndex)
	}
	return encoder.Bytes(), encoder.Error()
}

func (rp *ReadProperty) UnmarshalBinary(data []byte) error {
	if len(data) < 7 {
		return fmt.Errorf("unmarshall readPropertyData: payload too short: %d bytes", len(data))
	}
	decoder := encoding.NewDecoder(data)
	decoder.ContextObjectID(0, &rp.ObjectID)
	decoder.ContextValue(1, &rp.Property.Type)
	rp.Property.ArrayIndex = new(uint32)
	decoder.ContextValue(2, rp.Property.ArrayIndex)
	err := decoder.Error()
	var e encoding.ErrorIncorrectTagID
	//This tag is optional, maybe it doesn't exist
	if err != nil && errors.As(err, &e) {
		rp.Property.ArrayIndex = nil
		decoder.ResetError()
	}
	decoder.ContextAbstractType(3, &rp.Data)
	return decoder.Error()
}
