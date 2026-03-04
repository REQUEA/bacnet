package encoding

import (
	"errors"
	"fmt"
	"math"
	"reflect"
)

// nolint: deadcode, varcheck
const (
	applicationTagNull            byte = 0x00
	applicationTagBoolean         byte = 0x01
	applicationTagUnsignedInt     byte = 0x02
	applicationTagSignedInt       byte = 0x03
	applicationTagReal            byte = 0x04
	applicationTagDouble          byte = 0x05
	applicationTagOctetString     byte = 0x06
	applicationTagCharacterString byte = 0x07
	applicationTagBitString       byte = 0x08
	applicationTagEnumerated      byte = 0x09
	applicationTagDate            byte = 0x0A
	applicationTagTime            byte = 0x0B
	applicationTagObjectID        byte = 0x0C
)

const (
	tagNumberMask  uint8 = 0xf0
	tagNumberShift       = 4
	classMask      uint8 = 0x08
	classShift           = 3
	lvtMask        uint8 = 0x07
)

type tagClass bool

const (
	applicationClass = false
	contextClass     = true
)

type Unmarshalable interface {
	Unmarshal([]byte) ([]byte, error)
}

// Marshalable is implemented by types that can be both serialised and
// deserialised as BACnet application-tagged primitives.
type Marshalable interface {
	MarshalPrimitive() ([]byte, error)
	Unmarshal([]byte) ([]byte, error)
}

type Optional[T Unmarshalable] struct {
	present bool
	value   T
}

func (o *Optional[T]) Set(value T) {
	o.present = true
	o.value = value
}

func (o *Optional[T]) Present() bool {
	return o.present
}

func (o *Optional[T]) Get() T {
	return o.value
}

type TagValue struct {
	Tag   uint8
	Value Unmarshalable
}

func ReadContextValues(buf []byte, expected []TagValue) error {
	return errors.New("not implemented")
}

type LengthValueType interface {
	getClass() tagClass
}

type Unsigned8 struct {
	value uint8
}

func (u *Unsigned8) Value() uint8 {
	return u.value
}

func (u *Unsigned8) SetValue(v uint8) {
	u.value = v
}

func (u *Unsigned8) Unmarshal(buf []byte) ([]byte, error) {
	tag := (buf[0] & tagNumberMask) >> tagNumberShift
	data, remaining, err := parseVarLen(uint(tag), buf)
	if err != nil {
		return remaining, err
	}
	if len(data) > 1 {
		return remaining, fmt.Errorf("data too long for %T", u.value)
	}
	u.value = data[0]
	return remaining, nil
}

func (u *Unsigned8) MarshalPrimitive() ([]byte, error) {
	return u.MarshalTagged(uint(applicationTagUnsignedInt))
}

func (u *Unsigned8) MarshalTagged(tag uint) ([]byte, error) {
	data := make([]byte, 0)
	result := make([]byte, 0)
	tagLen := createTagLen(byte(tag), uint(len(data)))
	result = append(result, tagLen...)
	result = append(result, u.value)
	return result, nil
}

type UnsignedBase[T uint16 | uint32 | uint64] struct {
	value T
}

func (u *UnsignedBase[T]) Value() T {
	return u.value
}

func (u *UnsignedBase[T]) SetValue(v T) {
	u.value = v
}

func (u *UnsignedBase[T]) Unmarshal(buf []byte) ([]byte, error) {
	tag := (buf[0] & tagNumberMask) >> tagNumberShift
	data, remaining, err := parseVarLen(uint(tag), buf)
	if err != nil {
		return remaining, err
	}
	if len(data) > int(reflect.TypeOf(u.value).Size()) {
		return remaining, fmt.Errorf("data too long for %T", u.value)
	}
	u.value = 0
	for _, v := range data {
		u.value = (u.value << 8) | T(v)
	}
	return remaining, nil
}

func (u *UnsignedBase[T]) MarshalPrimitive() ([]byte, error) {
	return u.MarshalTagged(uint(applicationTagUnsignedInt))
}

func (u *UnsignedBase[T]) MarshalTagged(tag uint) ([]byte, error) {
	data := make([]byte, 0)
	typeOfValue := reflect.TypeOf(u.value)
	shift := typeOfValue.Bits() - 8
	for range typeOfValue.Size() {
		mask := T(0xff) << shift
		masked := u.value & mask
		value := masked >> shift
		if value != 0 {
			data = append(data, uint8(value))
		}
		shift -= 8
	}
	if len(data) == 0 {
		data = append(data, 0)
	}
	result := make([]byte, 0)
	tagLen := createTagLen(byte(tag), uint(len(data)))
	result = append(result, tagLen...)
	result = append(result, data...)
	return result, nil
}

func createTagLen(tag byte, length uint) []byte {
	result := make([]byte, 0)
	if length < 5 {
		if tag > 14 {
			// extended tag
			result = append(result, (0xf<<tagNumberShift)|(uint8(length)&lvtMask))
			result = append(result, tag)
		} else {
			result = append(result, (tag<<tagNumberShift)|(uint8(length)&lvtMask))
		}
		return result
	}
	if tag > 14 {
		// extended tag
		result = append(result, (0xf<<tagNumberShift)|0b101)
		result = append(result, tag)
	} else {
		result = append(result, (tag<<tagNumberShift)|0b101)
	}
	if length < 254 {
		result = append(result, byte(length))
	} else if length < 65536 {
		result = append(result, 254, byte((length&0xff00)>>8), byte(length&0xff))
	} else {
		result = append(result, 255,
			byte((length&0xff000000)>>24),
			byte((length&0xff0000)>>16),
			byte((length&0xff00)>>8),
			byte((length & 0xff)),
		)
	}
	return result
}

type Unsigned = UnsignedBase[uint64]

type Integer16 struct {
	value int16
}

func (i *Integer16) Unmarshal(buf []byte) ([]byte, error) {
	tag := (buf[0] & tagNumberMask) >> tagNumberShift
	data, remaining, err := parseVarLen(uint(tag), buf)
	if err != nil {
		return remaining, err
	}
	if len(data) > 2 {
		return remaining, fmt.Errorf("data to long for Integer16")
	}
	i.value = 0
	for _, v := range data {
		i.value = (i.value << 8) | int16(v)
	}
	return remaining, nil
}

type Unsigned16 = UnsignedBase[uint16]
type Unsigned32 = UnsignedBase[uint16]
type Unsigned64 = UnsignedBase[uint64]

// Real represents a BACnet REAL value (IEEE 754 float32, application tag 4).
type Real struct{ value float32 }

func (r *Real) Value() float32     { return r.value }
func (r *Real) SetValue(v float32) { r.value = v }

func (r *Real) MarshalPrimitive() ([]byte, error) {
	bits := math.Float32bits(r.value)
	return []byte{
		(applicationTagReal << tagNumberShift) | 4,
		byte(bits >> 24), byte(bits >> 16), byte(bits >> 8), byte(bits),
	}, nil
}

func (r *Real) MarshalTagged(tag uint) ([]byte, error) {
	bits := math.Float32bits(r.value)
	result := make([]byte, 0, 6)
	if tag < 15 {
		result = append(result, byte(tag<<4)|classMask|4)
	} else {
		result = append(result, byte(0xf<<4)|classMask|4, byte(tag))
	}
	return append(result, byte(bits>>24), byte(bits>>16), byte(bits>>8), byte(bits)), nil
}

func (r *Real) Unmarshal(buf []byte) ([]byte, error) {
	if len(buf) < 5 {
		return buf, fmt.Errorf("buffer too short for Real")
	}
	tagByte := buf[0]
	tag := (tagByte & tagNumberMask) >> tagNumberShift
	if tagByte&classMask == 0 && tag != applicationTagReal {
		return buf, fmt.Errorf("expected real tag %d, got %d", applicationTagReal, tag)
	}
	length := tagByte & lvtMask
	if length != 4 {
		return buf, fmt.Errorf("expected length 4 for Real, got %d", length)
	}
	bits := uint32(buf[1])<<24 | uint32(buf[2])<<16 | uint32(buf[3])<<8 | uint32(buf[4])
	r.value = math.Float32frombits(bits)
	return buf[5:], nil
}

// Boolean represents a BACnet BOOLEAN value (application tag 1).
// The value is encoded inside the tag byte's LVT field (0=false, 1=true).
type Boolean struct{ value bool }

func (b *Boolean) Value() bool     { return b.value }
func (b *Boolean) SetValue(v bool) { b.value = v }

func (b *Boolean) lvt() byte {
	if b.value {
		return 1
	}
	return 0
}

func (b *Boolean) MarshalPrimitive() ([]byte, error) {
	return []byte{(applicationTagBoolean << tagNumberShift) | b.lvt()}, nil
}

func (b *Boolean) MarshalTagged(tag uint) ([]byte, error) {
	return []byte{byte(tag<<4) | classMask | b.lvt()}, nil
}

func (b *Boolean) Unmarshal(buf []byte) ([]byte, error) {
	if len(buf) < 1 {
		return buf, fmt.Errorf("buffer too short for Boolean")
	}
	tagByte := buf[0]
	tag := (tagByte & tagNumberMask) >> tagNumberShift
	if tagByte&classMask == 0 && tag != applicationTagBoolean {
		return buf, fmt.Errorf("expected boolean tag %d, got %d", applicationTagBoolean, tag)
	}
	switch tagByte & lvtMask {
	case 0:
		b.value = false
	case 1:
		b.value = true
	default:
		return buf, fmt.Errorf("invalid LVT %d for Boolean", tagByte&lvtMask)
	}
	return buf[1:], nil
}

type BACnetObjectIdentifier struct {
	objType  uint16
	instance uint32
}

func (u *BACnetObjectIdentifier) Unmarshal(buf []byte) ([]byte, error) {
	tag := (buf[0] & tagNumberMask) >> tagNumberShift
	data, remaining, err := parseVarLen(uint(tag), buf)
	if err != nil {
		return remaining, err
	}
	if len(data) != 4 {
		return remaining, fmt.Errorf("wrong data size for object identifier")
	}
	u.objType = (uint16(data[0])<<8 | uint16(data[1])) >> 6
	u.instance = ((uint32(data[1]) << 16) & 0x3f0000) | ((uint32(data[2]) << 8) & 0xff00) | (uint32(data[3]) & 0xff)
	return remaining, nil
}

func (i *BACnetObjectIdentifier) ObjType() uint16  { return i.objType }
func (i *BACnetObjectIdentifier) Instance() uint32 { return i.instance }
func (i *BACnetObjectIdentifier) SetFromValues(objType uint16, instance uint32) {
	i.objType = objType
	i.instance = instance
}

func (i *BACnetObjectIdentifier) MarshalPrimitive() ([]byte, error) {
	result := make([]byte, 0)
	value := uint32(i.objType)<<22 | (i.instance & 0x3fffff)
	result = append(result,
		(applicationTagObjectID<<tagNumberShift)|0b100, // tag + len(4)
		byte(value>>24),
		byte(value>>16),
		byte(value>>8),
		byte(value&0xff),
	)
	return result, nil
}

func (i *BACnetObjectIdentifier) MarshalTagged(tag uint8) ([]byte, error) {
	value := uint32(i.objType)<<22 | (i.instance & 0x3fffff)
	var header []byte
	if tag < 15 {
		// context-class tag: (tagNum<<4) | classMask | len(4)
		header = []byte{(tag << tagNumberShift) | classMask | 4}
	} else {
		// extended context-class tag
		header = []byte{(0xf << tagNumberShift) | classMask | 4, tag}
	}
	result := append(header,
		byte(value>>24),
		byte(value>>16),
		byte(value>>8),
		byte(value&0xff),
	)
	return result, nil
}

type Enumerated struct {
	UnsignedBase[uint32]
}

// Unmarshal reads an Enumerated value from buf. For application-tagged data the tag
// number is validated (must be 9); context-tagged data is accepted without tag validation.
func (e *Enumerated) Unmarshal(buf []byte) ([]byte, error) {
	if len(buf) == 0 {
		return buf, fmt.Errorf("empty buffer")
	}
	if buf[0]&classMask == 0 {
		tag := (buf[0] & tagNumberMask) >> tagNumberShift
		if tag != applicationTagEnumerated {
			return buf, fmt.Errorf("expected enumerated tag %d, got %d", applicationTagEnumerated, tag)
		}
	}
	return e.UnsignedBase.Unmarshal(buf)
}

// MarshalPrimitive encodes the value with the BACnet Enumerated application tag (9).
func (e *Enumerated) MarshalPrimitive() ([]byte, error) {
	return e.UnsignedBase.MarshalTagged(uint(applicationTagEnumerated))
}

// MarshalTagged encodes the value with a BACnet context tag, setting the context class bit.
func (e *Enumerated) MarshalTagged(tag uint) ([]byte, error) {
	result, err := e.UnsignedBase.MarshalTagged(tag)
	if err != nil {
		return nil, err
	}
	result[0] |= classMask
	return result, nil
}

type BACnetPropertyIdentifier = Enumerated
type BACnetSegmentation = Enumerated

// NewEnumerated creates an Enumerated with the given initial value.
func NewEnumerated(v uint32) *Enumerated {
	e := &Enumerated{}
	e.SetValue(v)
	return e
}

// CharacterString represents a BACnet application-tagged character string (tag 7).
type CharacterString struct {
	value string
}

func NewCharacterString(s string) *CharacterString { return &CharacterString{value: s} }
func (cs *CharacterString) Value() string          { return cs.value }
func (cs *CharacterString) SetValue(s string)      { cs.value = s }

func (cs *CharacterString) MarshalPrimitive() ([]byte, error) {
	encoded := append([]byte{0x00}, []byte(cs.value)...) // 0x00 = UTF-8 encoding indicator
	result := createTagLen(applicationTagCharacterString, uint(len(encoded)))
	return append(result, encoded...), nil
}

func (cs *CharacterString) MarshalTagged(tag uint) ([]byte, error) {
	encoded := append([]byte{0x00}, []byte(cs.value)...)
	result := createTagLen(byte(tag), uint(len(encoded)))
	result[0] |= classMask
	return append(result, encoded...), nil
}

func (cs *CharacterString) Unmarshal(buf []byte) ([]byte, error) {
	if len(buf) == 0 {
		return buf, fmt.Errorf("empty buffer")
	}
	tag := (buf[0] & tagNumberMask) >> tagNumberShift
	if buf[0]&classMask == 0 && tag != applicationTagCharacterString {
		return buf, fmt.Errorf("expected charstring tag %d, got %d", applicationTagCharacterString, tag)
	}
	data, remaining, err := parseVarLen(uint(tag), buf)
	if err != nil {
		return remaining, err
	}
	if len(data) < 1 {
		return remaining, fmt.Errorf("charstring too short")
	}
	cs.value = string(data[1:]) // skip encoding indicator byte
	return remaining, nil
}

// BitString represents a BACnet application-tagged bit string (tag 8).
type BitString struct {
	unusedBits uint8
	octets     []byte
}

func NewBitString() *BitString { return &BitString{octets: make([]byte, 0)} }

func (bs *BitString) SetBit(n uint) *BitString {
	octetIndex := n / 8
	bitIndex := 7 - (n % 8)
	if octetIndex >= uint(len(bs.octets)) {
		fill := make([]byte, int(octetIndex)-len(bs.octets)+1)
		bs.octets = append(bs.octets, fill...)
		bs.unusedBits = 8
	}
	bs.octets[len(bs.octets)-1] |= (1 << bitIndex)
	if bs.unusedBits > uint8(bitIndex) {
		bs.unusedBits = uint8(bitIndex)
	}
	return bs
}

func (bs *BitString) GetBit(n uint) bool {
	octetIndex := n / 8
	if octetIndex >= uint(len(bs.octets)) {
		return false
	}
	bitIndex := 7 - (n % 8)
	return bs.octets[octetIndex]&(1<<bitIndex) != 0
}

func (bs *BitString) UnusedBits() uint8 { return bs.unusedBits }
func (bs *BitString) Octets() []byte    { return bs.octets }

func (bs *BitString) MarshalPrimitive() ([]byte, error) {
	data := append([]byte{bs.unusedBits}, bs.octets...)
	result := createTagLen(applicationTagBitString, uint(len(data)))
	return append(result, data...), nil
}

func (bs *BitString) MarshalTagged(tag uint) ([]byte, error) {
	data := append([]byte{bs.unusedBits}, bs.octets...)
	result := createTagLen(byte(tag), uint(len(data)))
	result[0] |= classMask
	return append(result, data...), nil
}

func (bs *BitString) Unmarshal(buf []byte) ([]byte, error) {
	if len(buf) == 0 {
		return buf, fmt.Errorf("empty buffer")
	}
	tag := (buf[0] & tagNumberMask) >> tagNumberShift
	if buf[0]&classMask == 0 && tag != applicationTagBitString {
		return buf, fmt.Errorf("expected bitstring tag %d, got %d", applicationTagBitString, tag)
	}
	data, remaining, err := parseVarLen(uint(tag), buf)
	if err != nil {
		return remaining, err
	}
	if len(data) < 1 {
		return remaining, fmt.Errorf("bitstring too short")
	}
	bs.unusedBits = data[0]
	bs.octets = make([]byte, len(data)-1)
	copy(bs.octets, data[1:])
	return remaining, nil
}

// BACnetArray is an ordered, resizable array of Marshalable elements.
// BACnet array indices are 1-based; index 0 is used by the protocol to
// query the element count.
type BACnetArray[T Marshalable] struct {
	elements []T
}

func NewBACnetArray[T Marshalable]() *BACnetArray[T] {
	return &BACnetArray[T]{elements: make([]T, 0)}
}

func (a *BACnetArray[T]) Len() uint { return uint(len(a.elements)) }

// Get returns the element at the given 1-based index.
func (a *BACnetArray[T]) Get(index uint) (T, error) {
	if index == 0 || int(index) > len(a.elements) {
		var zero T
		return zero, fmt.Errorf("index %d out of bounds (len=%d)", index, len(a.elements))
	}
	return a.elements[index-1], nil
}

// Set replaces all elements.
func (a *BACnetArray[T]) Set(values ...T) {
	a.elements = make([]T, len(values))
	copy(a.elements, values)
}

// Append adds one element to the end of the array.
func (a *BACnetArray[T]) Append(v T) {
	a.elements = append(a.elements, v)
}

// MarshalPrimitive encodes all elements concatenated (no wrapping tag).
func (a *BACnetArray[T]) MarshalPrimitive() ([]byte, error) {
	var result []byte
	for _, elem := range a.elements {
		b, err := elem.MarshalPrimitive()
		if err != nil {
			return nil, err
		}
		result = append(result, b...)
	}
	return result, nil
}

// MarshalAt encodes the single element at the given 1-based index.
func (a *BACnetArray[T]) MarshalAt(index uint) ([]byte, error) {
	elem, err := a.Get(index)
	if err != nil {
		return nil, err
	}
	return elem.MarshalPrimitive()
}

// Unmarshal is not required for read-only array properties.
func (a *BACnetArray[T]) Unmarshal(buf []byte) ([]byte, error) {
	return buf, fmt.Errorf("BACnetArray.Unmarshal not implemented")
}

type Date struct {
	value uint32
}

func (d *Date) Unmarshal(buf []byte) ([]byte, error) {
	tag := (buf[0] & tagNumberMask) >> tagNumberShift
	data, remaining, err := parseVarLen(uint(tag), buf)
	if err != nil {
		return remaining, err
	}
	if len(data) > 4 {
		return remaining, fmt.Errorf("date size above 4 bytes")
	}
	d.value = 0
	for _, v := range data {
		d.value = (d.value << 8) | uint32(v)
	}
	return remaining, nil
}

type Time struct {
	Hour     uint8
	Minute   uint8
	Second   uint8
	Hundreth uint8
}

func (t *Time) Unmarshal(buf []byte) ([]byte, error) {
	tag := (buf[0] & tagNumberMask) >> tagNumberShift
	data, remaining, err := parseVarLen(uint(tag), buf)
	if err != nil {
		return remaining, err
	}
	if len(data) > 4 {
		return remaining, fmt.Errorf("date size above 4 bytes")
	}
	t.Hour = data[0]
	t.Minute = data[1]
	t.Second = data[2]
	t.Hundreth = data[3]
	return remaining, nil
}

type BACnetDateTime struct {
	date Date
	time Time
}

func (t *BACnetDateTime) Unmarshal(buf []byte) ([]byte, error) {
	remaining, err := t.date.Unmarshal(buf)
	if err != nil {
		return remaining, fmt.Errorf("could not unmarshal date: %v", err)
	}
	remaining, err = t.time.Unmarshal(remaining)
	if err != nil {
		return remaining, fmt.Errorf("could not unmarshal time: %v", err)
	}
	return remaining, nil
}

type Abstract struct {
	value []byte
}

// NewAbstract creates an Abstract with pre-set raw application-tagged bytes.
func NewAbstract(data []byte) Abstract {
	return Abstract{value: data}
}

// Value returns the raw inner bytes of the Abstract (without context tag wrapper).
func (a *Abstract) Value() []byte {
	return a.value
}

func (a *Abstract) Unmarshal(buf []byte) ([]byte, error) {
	// extract original tag
	if buf[0]&classMask == 0 {
		return buf, fmt.Errorf("expected constructed data")
	}
	if buf[0]&lvtMask != 0b110 {
		return buf, fmt.Errorf("buffer not starting with opening tag")
	}
	tag := (buf[0] & tagNumberMask) >> tagNumberShift
	startOffset := uint(1)
	if tag == 15 {
		tag = buf[1]
		startOffset++
	}
	tagStack := []byte{tag}
	endOffset := startOffset
	valueEnd := startOffset
	for endOffset < uint(len(buf)) {
		if buf[endOffset]&classMask == 0 {
			currentTag := (buf[endOffset] & tagNumberMask) >> tagNumberShift
			if currentTag == 0 || currentTag == 1 {
				endOffset++
			} else {
				offset := uint(1)
				if currentTag == 15 {
					offset++ // we don't care about the tag value here, just skip over the byte
				}
				length := uint(buf[endOffset] & lvtMask)
				if length < 5 {
					offset += length
				} else if length == 5 {
					length = uint(buf[endOffset+offset])
					offset += 1 // now pointing right after length byte
					if length < 254 {
						offset += length
					} else if length < 65536 {
						length = uint(buf[endOffset+offset])<<8 | uint(buf[endOffset+offset+1])
						offset += 2 + length
					} else {
						length = uint(0)
						for i := range 4 {
							length = (length << 8) | uint(buf[endOffset+offset+uint(i)])
						}
						offset += 4 + length
					}
				} else {
					return buf, fmt.Errorf("unexpected length value (%d) in application class tag", length)
				}
				endOffset += offset
			}
		} else {
			// need to check if it is the closing of the outer tag
			currentTag := (buf[endOffset] & tagNumberMask) >> tagNumberShift
			offset := uint(1)
			if currentTag == 15 {
				currentTag = buf[endOffset+1]
				offset++
			}
			length := uint(buf[endOffset] & lvtMask)
			if length == 7 {
				if tagStack[len(tagStack)-1] == currentTag {
					tagStack = tagStack[:len(tagStack)-1]
				} else {
					return buf, fmt.Errorf("tag mismatch at offset %d", endOffset)
				}
				if len(tagStack) == 0 {
					valueEnd = endOffset // value ends before the closing tag
					endOffset += offset  // advance past closing tag
					break
				}
			} else if length == 6 {
				// another opening tag
				tagStack = append(tagStack, currentTag)
			} else if length == 5 {
				length = uint(buf[endOffset+offset])
				offset += 1 // now pointing right after length byte
				if length < 254 {
					offset += length
				} else if length < 65536 {
					length = uint(buf[endOffset+offset])<<8 | uint(buf[endOffset+offset+1])
					offset += 2 + length
				} else {
					length = uint(0)
					for i := range 4 {
						length = (length << 8) | uint(buf[endOffset+offset+uint(i)])
					}
					offset += 4 + length
				}
			} else {
				offset += length
			}
			endOffset += offset
		}
	}
	if len(tagStack) != 0 {
		return buf, fmt.Errorf("end of buffer before closing tag")
	}
	a.value = buf[startOffset:valueEnd]
	// return the remaining data of buf
	return buf[endOffset:], nil
}

func (a *Abstract) MarshalTagged(tag uint8) ([]byte, error) {
	result := make([]byte, 0)
	if tag < 15 {
		result = append(result, uint8(tag)<<tagNumberShift|0b1110)
	} else {
		result = append(result, uint8(0xf)<<tagNumberShift|0b1110, uint8(tag))
	}
	result = append(result, a.value...)
	if tag < 15 {
		result = append(result, uint8(tag)<<tagNumberShift|0b1111)
	} else {
		result = append(result, uint8(0xf)<<tagNumberShift|0b1111, uint8(tag))
	}
	return result, nil
}

type PrimitiveData struct {
	tag   uint
	value uint
	data  []byte
}

type ConstructedData struct {
	tag             uint
	applicationTags []PrimitiveData
	constructedTags map[uint]ConstructedData
}

func (c *ConstructedData) Marshal() ([]byte, error) {
	result := make([]byte, 0)
	err := c.marshalHelper(result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *ConstructedData) marshalHelper(result []byte) error {
	if c.tag < 15 {
		result = append(result, uint8(c.tag)<<tagNumberShift|0b1110)
	} else {
		result = append(result, 0b11111110, uint8(c.tag))
	}
	for _, p := range c.applicationTags {
		p.value = 0
	}

	if c.tag < 15 {
		result = append(result, uint8(c.tag)<<tagNumberShift|0b1111)
	} else {
		result = append(result, 0b11111111, uint8(c.tag))
	}
	return nil
}

func parsePrimitive(buf []byte) (*PrimitiveData, []byte, error) {
	tag := ((buf[0] & tagNumberMask) >> tagNumberShift)
	switch tag {
	case applicationTagNull:
		return &PrimitiveData{tag: uint(applicationTagNull)}, buf[1:], nil
	case applicationTagBoolean:
		lenValTyp := (buf[0] & lvtMask)
		if lenValTyp != 0 && lenValTyp != 1 {
			return nil, buf, fmt.Errorf("wrong value for boolean tag")
		}
		result := &PrimitiveData{
			tag:   uint(applicationTagBoolean),
			value: uint(lenValTyp),
		}
		return result, buf[1:], nil
	case applicationTagUnsignedInt,
		applicationTagSignedInt,
		applicationTagReal,
		applicationTagDouble,
		applicationTagOctetString,
		applicationTagCharacterString,
		applicationTagBitString,
		applicationTagEnumerated,
		applicationTagDate,
		applicationTagTime,
		applicationTagObjectID:
		data, remaining, err := parseVarLen(uint(tag), buf)
		if err != nil {
			return nil, nil, err
		}
		result := &PrimitiveData{
			tag:  uint(tag),
			data: data,
		}
		return result, remaining, nil
	default:
		// tag field == 15, this is not allowed by the standard
		return nil, buf, fmt.Errorf("tag value 15 is not valid for a primitive type")
	}
}

func parseConstructed(buf []byte) (*ConstructedData, []byte, error) {
	result := ConstructedData{}
	tag := uint((buf[0] & tagNumberMask) >> tagNumberShift)
	if tag < 15 {
		result.tag = tag
	} else {
		result.tag = uint(buf[1])
		_, _, err := parseVarLen(tag, buf)
		if err != nil {
			return nil, nil, err
		}
	}
	return nil, nil, errors.New("not implemented")
}

func ReadTag(buf []byte) (uint, error) {
	if len(buf) < 1 {
		return 0, fmt.Errorf("empty buffer")
	}
	tag := uint((buf[0] & tagNumberMask) >> tagNumberShift)
	if tag < 15 {
		return uint(tag), nil
	}
	if len(buf) < 2 {
		return 0, fmt.Errorf("cannot read extended tag")
	}
	return uint(buf[1]), nil
}

func parseVarLen(tag uint, buf []byte) ([]byte, []byte, error) {
	length := uint(buf[0] & lvtMask)
	offset := uint(1)
	if tag == 15 {
		offset++
	}
	if length > 5 {
		return nil, nil, fmt.Errorf("wrong value for length/value/type field (%d)", length)
	} else if length == 5 {
		if buf[offset] < 254 {
			length = uint(buf[offset])
			offset++
		} else if buf[offset] == 254 {
			offset++
			length = uint(buf[offset])
			offset++
			length = (length << 8) | uint(buf[offset])
			offset++
		} else {
			length = 0
			offset++
			for range 4 {
				length = (length << 8) | uint(buf[offset])
				offset++
			}
		}
	}
	if len(buf)-int(offset) < int(length) {
		return nil, buf, fmt.Errorf("buffer too short")
	}
	return buf[offset : offset+length], buf[offset+length:], nil
}

/*

Input: APDU payload []byte
Output: {
	applicationTags : []PrimitiveData
	contextTags : map[uint]ConstrutedData
}
parsePayload(buf []byte) {
    remaining := buf
	result := parsePayloadResult{
		applicationTags: make([]TLV, 0)
		contextTags: make(map[uint]TLV)
	}
	while not finished {
		TLV, remaining, err = parse(remaining)
	}
}

*/
