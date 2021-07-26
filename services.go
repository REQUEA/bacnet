package bacnet

import (
	"bytes"
	"fmt"
)

const MaxInstance = 0x3FFFFF
const InstanceBits = 22

type WhoIs struct {
	Low, High *uint //may be null if we want to check all range
}

type ErrorIncorrectTag struct {
	Expected uint8
	Given    uint8
}

func (e ErrorIncorrectTag) Error() string {
	return fmt.Sprintf("Incorrect tag %d, expected %d.", e.Given, e.Expected)
}

func (w WhoIs) MarshalBinary() ([]byte, error) {
	buf := &bytes.Buffer{}
	if w.Low != nil && w.High != nil {
		if *w.Low > MaxInstance || *w.High > MaxInstance {
			return nil, fmt.Errorf("invalid WhoIs range: [%d, %d]: max value is %d", *w.Low, *w.High, MaxInstance)
		}
		if *w.Low > *w.High {
			return nil, fmt.Errorf("invalid WhoIs range: [%d, %d]: low limit is higher than high limit", *w.Low, *w.High)
		}
		contextUnsigned(buf, 0, uint32(*w.Low))
		contextUnsigned(buf, 1, uint32(*w.High))
	}
	return buf.Bytes(), nil
}

func (w *WhoIs) UnmarshalBinary(data []byte) error {
	if len(data) == 0 {
		// If data is empty, the whoIs request is a full range
		// check. So keep the low and high pointer nil
		return nil
	}
	w.Low = new(uint)
	w.High = new(uint)
	buf := bytes.NewBuffer(data)
	// Tag 0 - Low Value
	expectedTagID := byte(0)
	_, tag, err := decodeTag(buf)
	if err != nil {
		return fmt.Errorf("decode 1st WhoIs tag: %w", err)
	}
	if tag.ID != expectedTagID {
		return fmt.Errorf("decode 1st WhoIs tag: %w", ErrorIncorrectTag{Expected: expectedTagID, Given: tag.ID})
	}
	//The tag value is the length of the next data field
	val, err := decodeUnsignedWithLen(buf, int(tag.Value))
	if err != nil {
		return fmt.Errorf("read 1st WhoIs value: %w", err)
	}
	*w.Low = uint(val)

	// Tag 1 - High Value
	expectedTagID = 1
	_, tag, err = decodeTag(buf)
	if err != nil {
		return fmt.Errorf("decode 2nd WhoIs tag: %w", err)
	}
	if tag.ID != expectedTagID {
		return fmt.Errorf("decode 2nd WhoIs tag: %w", ErrorIncorrectTag{Expected: expectedTagID, Given: tag.ID})
	}
	val, err = decodeUnsignedWithLen(buf, int(tag.Value))
	if err != nil {
		return fmt.Errorf("read 2st WhoIs value: %w", err)
	}
	*w.High = uint(val)
	return nil
}

type Iam struct {
	ObjectID            ObjectID
	MaxApduLength       uint32
	SegmentationSupport SegmentationSupport
	VendorID            uint32
}

func (iam Iam) MarshalBinary() ([]byte, error) {
	buf := &bytes.Buffer{}
	err := encodeAppData(buf, iam.ObjectID)
	if err != nil {
		return nil, err
	}
	err = encodeAppData(buf, iam.MaxApduLength)
	if err != nil {
		return nil, err
	}
	err = encodeAppData(buf, iam.SegmentationSupport)
	if err != nil {
		return nil, err
	}
	err = encodeAppData(buf, iam.VendorID)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (iam *Iam) UnmarshalBinary(data []byte) error {
	buf := bytes.NewBuffer(data)
	err := decodeAppData(buf, &iam.ObjectID)
	if err != nil {
		return fmt.Errorf("decode iam objectID: %w", err)
	}
	err = decodeAppData(buf, &iam.MaxApduLength)
	if err != nil {
		return fmt.Errorf("decode iam MaxAPDU: %w", err)
	}
	err = decodeAppData(buf, &iam.SegmentationSupport)
	if err != nil {
		return fmt.Errorf("decode iam SegmentationSupport: %w", err)
	}
	err = decodeAppData(buf, &iam.VendorID)
	if err != nil {
		return fmt.Errorf("decode iam VendorID: %w", err)
	}

	return nil
}
