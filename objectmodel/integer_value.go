package objectmodel

import (
	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/internal/encoding"
)

// IntegerValueObject implements the BACnet Integer Value object type (0x2d).
type IntegerValueObject struct {
	owner      *Device
	properties map[bacnet.PropertyIdentifier]Property
}

func NewIntegerValueObject(
	instanceID uint32,
	name string,
	initialValue int32,
	units bacnet.BACnetEngineeringUnits,
) *IntegerValueObject {
	properties := make(map[bacnet.PropertyIdentifier]Property)

	properties[bacnet.ObjectIdentifier] = NewObjectIdentifierProperty(
		true, uint16(bacnet.IntegerValue), instanceID,
	)
	properties[bacnet.ObjectName] = NewCharacterStringProperty(false, name)
	properties[bacnet.ObjectTypeProp] = NewEnumeratedProperty(true, uint32(bacnet.IntegerValue))
	properties[bacnet.PresentValue] = NewIntegerProperty(false, initialValue)
	properties[bacnet.StatusFlags] = newStatusFlagsProperty()
	properties[bacnet.EventState] = NewEnumeratedProperty(true, uint32(bacnet.EventStateNormal))
	properties[bacnet.OutOfService] = NewBooleanProperty(false, false)
	properties[bacnet.Units] = NewEnumeratedProperty(false, uint32(units))

	propertyList := NewBACnetArrayProperty[*encoding.Enumerated](true)
	propertyList.value.Set(
		encoding.NewEnumerated(uint32(bacnet.PresentValue)),
		encoding.NewEnumerated(uint32(bacnet.StatusFlags)),
		encoding.NewEnumerated(uint32(bacnet.EventState)),
		encoding.NewEnumerated(uint32(bacnet.OutOfService)),
		encoding.NewEnumerated(uint32(bacnet.Units)),
	)
	properties[bacnet.PropertyList] = propertyList

	return &IntegerValueObject{properties: properties}
}

// Object interface implementation.

func (iv *IntegerValueObject) GetOwner() *Device { return iv.owner }
func (iv *IntegerValueObject) setOwner(d *Device) { iv.owner = d }

func (iv *IntegerValueObject) GetProperty(id bacnet.PropertyIdentifier) Property {
	return iv.properties[id]
}

func (iv *IntegerValueObject) AllPropertyIdentifiers() []bacnet.PropertyIdentifier {
	ids := make([]bacnet.PropertyIdentifier, 0, len(iv.properties))
	for id := range iv.properties {
		ids = append(ids, id)
	}
	return ids
}

// COVProperties returns PresentValue and StatusFlags as the COV-triggering properties.
func (iv *IntegerValueObject) COVProperties() []bacnet.PropertyIdentifier {
	return []bacnet.PropertyIdentifier{bacnet.PresentValue, bacnet.StatusFlags}
}

// Typed accessors.

func (iv *IntegerValueObject) GetPresentValue() int32 {
	return iv.properties[bacnet.PresentValue].GetValue().(*encoding.Integer).Value()
}

func (iv *IntegerValueObject) SetPresentValue(v int32) {
	i := &encoding.Integer{}
	i.SetValue(v)
	_ = iv.properties[bacnet.PresentValue].SetValue(i)
}

// SetStatusFlag sets the given StatusFlags bit. Clearing bits is not yet supported.
func (iv *IntegerValueObject) SetStatusFlag(flag bacnet.BACnetStatusFlag, set bool) {
	bs := iv.properties[bacnet.StatusFlags].GetValue().(*encoding.BitString)
	if set {
		bs.SetBit(uint(flag))
	}
}
