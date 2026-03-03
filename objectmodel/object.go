package objectmodel

import (
	"fmt"

	"github.com/REQUEA/bacnet"
)

const (
	ProtocolVersion  = 1
	ProtocolRevision = 30
)

type Object interface {
	GetOwner() *Device
	GetProperty(bacnet.PropertyIdentifier) *Property
}

type Property interface {
	GetValue() any
	SetValue(any) error
	IsWritable() bool
}

type PropertyBase[T any] struct {
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

type ObjectIdentifierProperty = PropertyBase[bacnet.BACnetObjectIdentifier]

func NewObjectIdentifierProperty(readOnly bool, id bacnet.BACnetObjectIdentifier) *ObjectIdentifierProperty {
	result := &ObjectIdentifierProperty{value: id, readOnly: readOnly}
	return result
}

type CharacterStringProperty = PropertyBase[string]

func NewCharacterStringProperty(readOnly bool, s string) *CharacterStringProperty {
	return &CharacterStringProperty{value: s, readOnly: readOnly}
}

type ObjectTypeProperty = PropertyBase[bacnet.ObjectType]

func NewObjectTypeProperty(readOnly bool, t bacnet.ObjectType) *ObjectTypeProperty {
	return &ObjectTypeProperty{value: t, readOnly: readOnly}
}

type DeviceStatusProperty = PropertyBase[bacnet.BACnetDeviceStatus]

func NewDeviceStatusProperty(readOnly bool, s bacnet.BACnetDeviceStatus) *DeviceStatusProperty {
	return &DeviceStatusProperty{value: s, readOnly: readOnly}
}

type Unsigned16Property = PropertyBase[uint16]

func NewUnsigned16Property(readOnly bool, u uint16) *Unsigned16Property {
	return &Unsigned16Property{value: u, readOnly: readOnly}
}

type UnsignedProperty = PropertyBase[uint]

func NewUnsignedProperty(readOnly bool, u uint) *UnsignedProperty {
	return &UnsignedProperty{value: u, readOnly: readOnly}
}

type ServicesSupportedProperty = PropertyBase[bacnet.BitString]

func NewServiceSupportedProperty(
	readOnly bool,
	services ...bacnet.BACnetServicesSupported,
) *ServicesSupportedProperty {
	result := &ServicesSupportedProperty{
		value:    *bacnet.NewBitString(),
		readOnly: readOnly,
	}
	for _, s := range services {
		result.value.SetBit(uint(s))
	}
	return result
}

type ObjectTypesSupportedProperty = PropertyBase[bacnet.BitString]

func NewObjectTypesSupportedProperty(
	readOnly bool,
	types ...bacnet.BACnetObjectTypesSupported,
) *ObjectTypesSupportedProperty {
	result := &ObjectTypesSupportedProperty{
		value:    *bacnet.NewBitString(),
		readOnly: readOnly,
	}
	for _, s := range types {
		result.value.SetBit(uint(s))
	}
	return result
}

type SegmentationProperty = PropertyBase[bacnet.SegmentationSupport]

func NewSegmentationProperty(readOnly bool, s bacnet.SegmentationSupport) *SegmentationProperty {
	return &SegmentationProperty{value: s, readOnly: readOnly}
}

type BACnetArrayProperty[T any] = PropertyBase[bacnet.BACnetArray[T]]

func NewBACnetArrayProperty[T any](readOnly bool, size int, writable bool) *BACnetArrayProperty[T] {
	return &BACnetArrayProperty[T]{
		value:    bacnet.NewBACnetArray[T](size, writable),
		readOnly: readOnly,
	}
}

type BACnetListProperty[T any] = PropertyBase[bacnet.BACnetList[T]]

func NewBACnetListProperty[T any](readOnly bool) *BACnetListProperty[T] {
	return &BACnetListProperty[T]{
		value:    bacnet.NewBACnetList[T](),
		readOnly: readOnly,
	}
}
