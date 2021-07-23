package bacnet

import (
	"bytes"
	"fmt"
)

const MaxInstance = 0x3FFFFF

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
			return nil, fmt.Errorf("Invalid WhoIs range: [%d, %d]: max value is %d", *w.Low, *w.High, MaxInstance)
		}
		if *w.Low > *w.High {
			return nil, fmt.Errorf("Invalid WhoIs range: [%d, %d]: low limit is higher than high limit", *w.Low, *w.High)
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
	var expectedTagID byte = 0
	_, tag, err := decodeTag(buf)
	if err != nil {
		return fmt.Errorf("decode 1st WhoIs tag: %w", err)
	}
	if tag.ID != expectedTagID {
		return fmt.Errorf("decode 1st WhoIs tag: %w", ErrorIncorrectTag{Expected: expectedTagID, Given: tag.ID})
	}
	//here the value of the tag is the length of the data
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
