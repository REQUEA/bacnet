package servicelayer

import (
	"fmt"

	"github.com/REQUEA/bacnet/internal/encoding"
)

type byPosition struct {
	referenceIndex encoding.Unsigned
	count          encoding.Integer16
}

func (p *byPosition) Unmarshal(buf []byte) ([]byte, error) {
	remaining, err := p.referenceIndex.Unmarshal(buf)
	if err != nil {
		return remaining, err
	}
	remaining, err = p.count.Unmarshal(remaining)
	if err != nil {
		return remaining, err
	}
	return remaining, nil
}

type bySequenceNumber struct {
	referenceSequenceNumber encoding.Unsigned
	count                   encoding.Integer16
}

func (p *bySequenceNumber) Unmarshal(buf []byte) ([]byte, error) {
	remaining, err := p.referenceSequenceNumber.Unmarshal(buf)
	if err != nil {
		return remaining, err
	}
	remaining, err = p.count.Unmarshal(remaining)
	if err != nil {
		return remaining, err
	}
	return remaining, nil
}

type byTime struct {
	referenceTime encoding.BACnetDateTime
	count         encoding.Integer16
}

func (t *byTime) Unmarshal(buf []byte) ([]byte, error) {
	remaining, err := t.referenceTime.Unmarshal(buf)
	if err != nil {
		return remaining, err
	}
	remaining, err = t.count.Unmarshal(remaining)
	if err != nil {
		return remaining, err
	}
	return remaining, nil
}

type ReadRangeRequest struct {
	objectIdentifier      encoding.BACnetObjectIdentifier
	propertyIdentifier    encoding.BACnetPropertyIdentifier
	propertyArrayIndex    encoding.Optional[*encoding.Unsigned]
	rangeByPosition       encoding.Optional[*byPosition]
	rangeBySequenceNumber encoding.Optional[*bySequenceNumber]
	rangeByTime           encoding.Optional[*byTime]
}

func (r *ReadRangeRequest) Unmarshal(buf []byte) ([]byte, error) {
	remaining := buf
	objIdFound := false
	propIdFound := false
	rangeEltCount := 0
	for len(remaining) > 0 {
		tag, err := encoding.ReadTag(remaining)
		if err != nil {
			return remaining, fmt.Errorf("failed to read tag: %v", err)
		}
		switch tag {
		case 0:
			remaining, err = r.objectIdentifier.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading objectIdentifier: %v", err)
			}
			objIdFound = true
		case 1:
			remaining, err = r.propertyIdentifier.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading propertyIdentifier: %v", err)
			}
			propIdFound = true
		case 2:
			var propertyArrayIndex encoding.Unsigned
			remaining, err = propertyArrayIndex.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading propertyArrayIndex: %v", err)
			}
			r.propertyArrayIndex.Set(&propertyArrayIndex)
		case 3:
			var rangeByPosition byPosition
			remaining, err = rangeByPosition.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading range.byPosition field: %v", err)
			}
			r.rangeByPosition.Set(&rangeByPosition)
			rangeEltCount++
		case 6:
			var rangeBySequenceNumber bySequenceNumber
			remaining, err = rangeBySequenceNumber.Unmarshal(buf)
			if err != nil {
				return remaining, fmt.Errorf("error reading range.bySequenceNumber field: %v", err)
			}
			r.rangeBySequenceNumber.Set(&rangeBySequenceNumber)
			rangeEltCount++
		case 7:
			var rangeTime byTime
			remaining, err = rangeTime.Unmarshal(buf)
			if err != nil {
				return remaining, fmt.Errorf("error reading range.byTime field: %v", err)
			}
			r.rangeByTime.Set(&rangeTime)
			rangeEltCount++
		default:
			return remaining, fmt.Errorf("unexpected tag %v", tag)
		}
	}
	if !objIdFound {
		return remaining, fmt.Errorf("missing objectIdentifier")
	}
	if !propIdFound {
		return remaining, fmt.Errorf("missing propertyIdentifier")
	}
	if rangeEltCount > 1 {
		return remaining, fmt.Errorf("more than one element present for the range field")
	}
	return remaining, nil
}
