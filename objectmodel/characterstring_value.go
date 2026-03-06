package objectmodel

import (
	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/internal/encoding"
)

// CharacterStringValueObject implements the BACnet CharacterString Value object type (0x28).
type CharacterStringValueObject struct {
	owner      *Device
	properties map[bacnet.PropertyIdentifier]Property
}

func NewCharacterStringValueObject(
	instanceID uint32,
	name string,
	initialValue string,
) *CharacterStringValueObject {
	properties := make(map[bacnet.PropertyIdentifier]Property)

	properties[bacnet.ObjectIdentifier] = NewObjectIdentifierProperty(
		true, uint16(bacnet.CharacterstringValue), instanceID,
	)
	properties[bacnet.ObjectName] = NewCharacterStringProperty(false, name)
	properties[bacnet.ObjectTypeProp] = NewEnumeratedProperty(true, uint32(bacnet.CharacterstringValue))
	properties[bacnet.PresentValue] = NewCharacterStringProperty(false, initialValue)
	properties[bacnet.StatusFlags] = newStatusFlagsProperty()
	properties[bacnet.EventState] = NewEnumeratedProperty(true, uint32(bacnet.EventStateNormal))
	properties[bacnet.OutOfService] = NewBooleanProperty(false, false)

	propertyList := NewBACnetArrayProperty[*encoding.Enumerated](true)
	propertyList.value.Set(
		encoding.NewEnumerated(uint32(bacnet.PresentValue)),
		encoding.NewEnumerated(uint32(bacnet.StatusFlags)),
		encoding.NewEnumerated(uint32(bacnet.EventState)),
		encoding.NewEnumerated(uint32(bacnet.OutOfService)),
	)
	properties[bacnet.PropertyList] = propertyList

	return &CharacterStringValueObject{properties: properties}
}

// Object interface implementation.

func (csv *CharacterStringValueObject) GetOwner() *Device  { return csv.owner }
func (csv *CharacterStringValueObject) setOwner(d *Device) { csv.owner = d }

func (csv *CharacterStringValueObject) GetProperty(id bacnet.PropertyIdentifier) Property {
	return csv.properties[id]
}

func (csv *CharacterStringValueObject) AllPropertyIdentifiers() []bacnet.PropertyIdentifier {
	ids := make([]bacnet.PropertyIdentifier, 0, len(csv.properties))
	for id := range csv.properties {
		ids = append(ids, id)
	}
	return ids
}

// COVProperties returns PresentValue and StatusFlags as the COV-triggering properties.
func (csv *CharacterStringValueObject) COVProperties() []bacnet.PropertyIdentifier {
	return []bacnet.PropertyIdentifier{bacnet.PresentValue, bacnet.StatusFlags}
}

// Typed accessors.

func (csv *CharacterStringValueObject) GetPresentValue() string {
	return csv.properties[bacnet.PresentValue].GetValue().(*encoding.CharacterString).Value()
}

func (csv *CharacterStringValueObject) SetPresentValue(v string) {
	_ = csv.properties[bacnet.PresentValue].SetValue(encoding.NewCharacterString(v))
}

// SetStatusFlag sets the given StatusFlags bit. Clearing bits is not yet supported.
func (csv *CharacterStringValueObject) SetStatusFlag(flag bacnet.BACnetStatusFlag, set bool) {
	bs := csv.properties[bacnet.StatusFlags].GetValue().(*encoding.BitString)
	if set {
		bs.SetBit(uint(flag))
	}
}
