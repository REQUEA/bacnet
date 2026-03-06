package objectmodel

import (
	"fmt"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/internal/encoding"
)

const (
	ProtocolVersion  = 1
	ProtocolRevision = 30
)

type Object interface {
	GetOwner() *Device
	GetProperty(bacnet.PropertyIdentifier) Property
	AllPropertyIdentifiers() []bacnet.PropertyIdentifier
	// COVProperties returns the property identifiers that trigger COV notifications.
	// Returns nil if the object does not support COV.
	COVProperties() []bacnet.PropertyIdentifier
	setOwner(*Device)
}

// PresentValueSetter is implemented by BACnet objects that expose a writable
// float32 present value (e.g. AnalogInput, AnalogValue, AnalogOutput, …).
type PresentValueSetter interface {
	SetPresentValue(float32)
}

type Property interface {
	GetValue() any
	SetValue(any) error
	IsWritable() bool
	// MarshalValue serialises the property's current value as application-tagged bytes.
	MarshalValue() ([]byte, error)
	// UnmarshalValue parses application-tagged bytes and updates the property value in place.
	UnmarshalValue([]byte) error
}

// PropertyBase is the generic base for all scalar property types.
// T must be a pointer to an encoding type that implements encoding.Marshalable.
type PropertyBase[T encoding.Marshalable] struct {
	value    T
	readOnly bool
}

func (p *PropertyBase[T]) GetValue() any {
	return p.value
}

func (p *PropertyBase[T]) IsWritable() bool {
	return !p.readOnly
}

func (p *PropertyBase[T]) SetValue(v any) error {
	if p.readOnly {
		return fmt.Errorf("trying to set a read only property")
	}
	newValue, ok := v.(T)
	if !ok {
		return fmt.Errorf("wrong property value type")
	}
	p.value = newValue
	return nil
}

func (p *PropertyBase[T]) MarshalValue() ([]byte, error) {
	return p.value.MarshalPrimitive()
}

func (p *PropertyBase[T]) UnmarshalValue(data []byte) error {
	if p.readOnly {
		return fmt.Errorf("trying to set a read only property")
	}
	_, err := p.value.Unmarshal(data)
	return err
}

// --- Scalar property type aliases ---

type ObjectIdentifierProperty = PropertyBase[*encoding.BACnetObjectIdentifier]

func NewObjectIdentifierProperty(readOnly bool, objType uint16, instance uint32) *ObjectIdentifierProperty {
	v := &encoding.BACnetObjectIdentifier{}
	v.SetFromValues(objType, instance)
	return &ObjectIdentifierProperty{value: v, readOnly: readOnly}
}

type CharacterStringProperty = PropertyBase[*encoding.CharacterString]

func NewCharacterStringProperty(readOnly bool, s string) *CharacterStringProperty {
	return &CharacterStringProperty{value: encoding.NewCharacterString(s), readOnly: readOnly}
}

// EnumeratedProperty stores any BACnet enumerated or unsigned-integer value
// (ObjectType, DeviceStatus, SegmentationSupport, PropertyIdentifier, …).
type EnumeratedProperty = PropertyBase[*encoding.Enumerated]

func NewEnumeratedProperty(readOnly bool, v uint32) *EnumeratedProperty {
	return &EnumeratedProperty{value: encoding.NewEnumerated(v), readOnly: readOnly}
}

type Unsigned16Property = PropertyBase[*encoding.Unsigned16]

func NewUnsigned16Property(readOnly bool, u uint16) *Unsigned16Property {
	v := &encoding.Unsigned16{}
	v.SetValue(u)
	return &Unsigned16Property{value: v, readOnly: readOnly}
}

type UnsignedProperty = PropertyBase[*encoding.Unsigned]

func NewUnsignedProperty(readOnly bool, u uint) *UnsignedProperty {
	v := &encoding.Unsigned{}
	v.SetValue(uint64(u))
	return &UnsignedProperty{value: v, readOnly: readOnly}
}

type BitStringProperty = PropertyBase[*encoding.BitString]

type RealProperty = PropertyBase[*encoding.Real]

func NewRealProperty(readOnly bool, v float32) *RealProperty {
	r := &encoding.Real{}
	r.SetValue(v)
	return &RealProperty{value: r, readOnly: readOnly}
}

type BooleanProperty = PropertyBase[*encoding.Boolean]

func NewBooleanProperty(readOnly bool, v bool) *BooleanProperty {
	b := &encoding.Boolean{}
	b.SetValue(v)
	return &BooleanProperty{value: b, readOnly: readOnly}
}

type IntegerProperty = PropertyBase[*encoding.Integer]

func NewIntegerProperty(readOnly bool, v int32) *IntegerProperty {
	i := &encoding.Integer{}
	i.SetValue(v)
	return &IntegerProperty{value: i, readOnly: readOnly}
}

// NewServiceSupportedProperty builds a BitString property for ProtocolServicesSupported.
func NewServiceSupportedProperty(
	readOnly bool,
	services ...bacnet.BACnetServicesSupported,
) *BitStringProperty {
	bs := encoding.NewBitString()
	for _, s := range services {
		bs.SetBit(uint(s))
	}
	return &BitStringProperty{value: bs, readOnly: readOnly}
}

// NewObjectTypesSupportedProperty builds a BitString property for ProtocolObjectTypesSupported.
func NewObjectTypesSupportedProperty(
	readOnly bool,
	types ...bacnet.BACnetObjectTypesSupported,
) *BitStringProperty {
	bs := encoding.NewBitString()
	for _, s := range types {
		bs.SetBit(uint(s))
	}
	return &BitStringProperty{value: bs, readOnly: readOnly}
}

// --- Array property ---

// ArrayProperty is implemented by BACnetArrayProperty.
// GetAt(0) returns the element count as *encoding.Unsigned.
// GetAt(n) for n≥1 returns the n-th element (1-based).
type ArrayProperty interface {
	GetAt(position uint) (encoding.Marshalable, error)
	SetAt(position uint, value encoding.Marshalable) error
}

type BACnetArrayProperty[T encoding.Marshalable] struct {
	PropertyBase[*encoding.BACnetArray[T]]
}

func NewBACnetArrayProperty[T encoding.Marshalable](readOnly bool) *BACnetArrayProperty[T] {
	return &BACnetArrayProperty[T]{
		PropertyBase: PropertyBase[*encoding.BACnetArray[T]]{
			value:    encoding.NewBACnetArray[T](),
			readOnly: readOnly,
		},
	}
}

func (p *BACnetArrayProperty[T]) GetAt(position uint) (encoding.Marshalable, error) {
	if position == 0 {
		count := &encoding.Unsigned{}
		count.SetValue(uint64(p.value.Len()))
		return count, nil
	}
	return p.value.Get(position)
}

func (p *BACnetArrayProperty[T]) SetAt(_ uint, value encoding.Marshalable) error {
	if p.readOnly {
		return fmt.Errorf("trying to set a read only property")
	}
	v, ok := value.(T)
	if !ok {
		return fmt.Errorf("wrong element type for array property")
	}
	_ = v
	return fmt.Errorf("SetAt not fully implemented for BACnetArrayProperty")
}

// --- Range property ---

// RangeReadResult holds the result of a ReadRange operation.
type RangeReadResult struct {
	Items               [][]byte // each item as application-tagged bytes
	FirstItem           bool
	LastItem            bool
	MoreItems           bool
	FirstSequenceNumber *uint32 // non-nil for BySequenceNumber/ByTime reads
}

// RangeProperty is implemented by properties that support ReadRange.
type RangeProperty interface {
	ReadByPosition(referenceIndex uint32, count int32) (*RangeReadResult, error)
	ReadBySequenceNumber(seqNum uint32, count int32) (*RangeReadResult, error)
	ReadByTime(refTime encoding.BACnetDateTime, count int32) (*RangeReadResult, error)
}

// --- List property (DeviceAddressBinding only; marshal not supported) ---

type BACnetListProperty[T any] struct {
	value    bacnet.BACnetList[T]
	readOnly bool
}

func NewBACnetListProperty[T any](readOnly bool) *BACnetListProperty[T] {
	return &BACnetListProperty[T]{
		value:    bacnet.NewBACnetList[T](),
		readOnly: readOnly,
	}
}

func (p *BACnetListProperty[T]) GetValue() any    { return p.value }
func (p *BACnetListProperty[T]) IsWritable() bool { return !p.readOnly }

func (p *BACnetListProperty[T]) SetValue(v any) error {
	if p.readOnly {
		return fmt.Errorf("trying to set a read only property")
	}
	newValue, ok := v.(bacnet.BACnetList[T])
	if !ok {
		return fmt.Errorf("wrong property value type")
	}
	p.value = newValue
	return nil
}

func (p *BACnetListProperty[T]) MarshalValue() ([]byte, error) {
	return []byte{}, nil
}

func (p *BACnetListProperty[T]) UnmarshalValue([]byte) error {
	return fmt.Errorf("unmarshal not supported for BACnetList properties")
}
