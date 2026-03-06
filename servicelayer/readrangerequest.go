package servicelayer

import (
	"fmt"

	"github.com/REQUEA/bacnet/internal/encoding"
)

func (r *ReadRangeRequest) Marshal() ([]byte, error) {
	var result []byte

	b, err := r.objectIdentifier.MarshalTagged(0)
	if err != nil {
		return nil, fmt.Errorf("could not marshal objectIdentifier: %w", err)
	}
	result = append(result, b...)

	b, err = r.propertyIdentifier.MarshalTagged(1)
	if err != nil {
		return nil, fmt.Errorf("could not marshal propertyIdentifier: %w", err)
	}
	result = append(result, b...)

	if r.propertyArrayIndex.Present() {
		b, err = r.propertyArrayIndex.Get().MarshalTagged(2)
		if err != nil {
			return nil, fmt.Errorf("could not marshal propertyArrayIndex: %w", err)
		}
		result = append(result, b...)
	}

	if r.rangeByPosition.Present() {
		p := r.rangeByPosition.Get()
		result = append(result, openingTag(3))
		b, err = p.referenceIndex.MarshalPrimitive()
		if err != nil {
			return nil, fmt.Errorf("could not marshal byPosition.referenceIndex: %w", err)
		}
		result = append(result, b...)
		b, err = p.count.MarshalPrimitive()
		if err != nil {
			return nil, fmt.Errorf("could not marshal byPosition.count: %w", err)
		}
		result = append(result, b...)
		result = append(result, closingTag(3))
	} else if r.rangeBySequenceNumber.Present() {
		p := r.rangeBySequenceNumber.Get()
		result = append(result, openingTag(6))
		b, err = p.referenceSequenceNumber.MarshalPrimitive()
		if err != nil {
			return nil, fmt.Errorf("could not marshal bySequenceNumber.referenceSequenceNumber: %w", err)
		}
		result = append(result, b...)
		b, err = p.count.MarshalPrimitive()
		if err != nil {
			return nil, fmt.Errorf("could not marshal bySequenceNumber.count: %w", err)
		}
		result = append(result, b...)
		result = append(result, closingTag(6))
	} else if r.rangeByTime.Present() {
		p := r.rangeByTime.Get()
		result = append(result, openingTag(7))
		b, err = p.referenceTime.MarshalPrimitive()
		if err != nil {
			return nil, fmt.Errorf("could not marshal byTime.referenceTime: %w", err)
		}
		result = append(result, b...)
		b, err = p.count.MarshalPrimitive()
		if err != nil {
			return nil, fmt.Errorf("could not marshal byTime.count: %w", err)
		}
		result = append(result, b...)
		result = append(result, closingTag(7))
	}

	return result, nil
}

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
	objIDFound := false
	propIDFound := false
	rangeEltCount := 0
	for len(remaining) > 0 {
		tag, err := encoding.ReadTag(remaining)
		if err != nil {
			return remaining, fmt.Errorf("failed to read tag: %w", err)
		}
		switch tag {
		case 0:
			remaining, err = r.objectIdentifier.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading objectIdentifier: %w", err)
			}
			objIDFound = true
		case 1:
			remaining, err = r.propertyIdentifier.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading propertyIdentifier: %w", err)
			}
			propIDFound = true
		case 2:
			var propertyArrayIndex encoding.Unsigned
			remaining, err = propertyArrayIndex.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading propertyArrayIndex: %w", err)
			}
			r.propertyArrayIndex.Set(&propertyArrayIndex)
		case 3:
			if len(remaining) < 1 || remaining[0] != openingTag(3) {
				return remaining, fmt.Errorf("expected opening tag [3] for byPosition")
			}
			remaining = remaining[1:]
			var rangeByPosition byPosition
			remaining, err = rangeByPosition.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading range.byPosition field: %w", err)
			}
			if len(remaining) < 1 || remaining[0] != closingTag(3) {
				return remaining, fmt.Errorf("expected closing tag [3] for byPosition")
			}
			remaining = remaining[1:]
			r.rangeByPosition.Set(&rangeByPosition)
			rangeEltCount++
		case 6:
			if len(remaining) < 1 || remaining[0] != openingTag(6) {
				return remaining, fmt.Errorf("expected opening tag [6] for bySequenceNumber")
			}
			remaining = remaining[1:]
			var rangeBySequenceNumber bySequenceNumber
			remaining, err = rangeBySequenceNumber.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading range.bySequenceNumber field: %w", err)
			}
			if len(remaining) < 1 || remaining[0] != closingTag(6) {
				return remaining, fmt.Errorf("expected closing tag [6] for bySequenceNumber")
			}
			remaining = remaining[1:]
			r.rangeBySequenceNumber.Set(&rangeBySequenceNumber)
			rangeEltCount++
		case 7:
			if len(remaining) < 1 || remaining[0] != openingTag(7) {
				return remaining, fmt.Errorf("expected opening tag [7] for byTime")
			}
			remaining = remaining[1:]
			var rangeTime byTime
			remaining, err = rangeTime.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading range.byTime field: %w", err)
			}
			if len(remaining) < 1 || remaining[0] != closingTag(7) {
				return remaining, fmt.Errorf("expected closing tag [7] for byTime")
			}
			remaining = remaining[1:]
			r.rangeByTime.Set(&rangeTime)
			rangeEltCount++
		default:
			return remaining, fmt.Errorf("unexpected tag %v", tag)
		}
	}
	if !objIDFound {
		return remaining, fmt.Errorf("missing objectIdentifier")
	}
	if !propIDFound {
		return remaining, fmt.Errorf("missing propertyIdentifier")
	}
	if rangeEltCount > 1 {
		return remaining, fmt.Errorf("more than one element present for the range field")
	}
	return remaining, nil
}
