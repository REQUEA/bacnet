package objectmodel

import (
	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/internal/encoding"
)

// AnalogInputObject implements the BACnet Analog Input object type.
type AnalogInputObject struct {
	owner      *Device
	properties map[bacnet.PropertyIdentifier]Property
}

func NewAnalogInputObject(
	instanceID uint32,
	name string,
	units bacnet.Unit,
) *AnalogInputObject {
	properties := make(map[bacnet.PropertyIdentifier]Property)

	properties[bacnet.ObjectIdentifier] = NewObjectIdentifierProperty(
		true, uint16(bacnet.AnalogInput), instanceID,
	)
	properties[bacnet.ObjectName] = NewCharacterStringProperty(false, name)
	properties[bacnet.ObjectTypeProp] = NewEnumeratedProperty(true, uint32(bacnet.AnalogInput))
	properties[bacnet.PresentValue] = NewRealProperty(false, 0.0)
	properties[bacnet.StatusFlags] = newStatusFlagsProperty()
	properties[bacnet.EventState] = NewEnumeratedProperty(true, uint32(bacnet.EventStateNormal))
	properties[bacnet.OutOfService] = NewBooleanProperty(false, false)
	properties[bacnet.Units] = NewEnumeratedProperty(false, uint32(units))
	properties[bacnet.CovIncrement] = NewRealProperty(false, 0.0)

	propertyList := NewBACnetArrayProperty[*encoding.Enumerated](true)
	propertyList.value.Set(
		encoding.NewEnumerated(uint32(bacnet.PresentValue)),
		encoding.NewEnumerated(uint32(bacnet.StatusFlags)),
		encoding.NewEnumerated(uint32(bacnet.EventState)),
		encoding.NewEnumerated(uint32(bacnet.OutOfService)),
		encoding.NewEnumerated(uint32(bacnet.Units)),
	)
	properties[bacnet.PropertyList] = propertyList

	return &AnalogInputObject{properties: properties}
}

// Object interface implementation.

func (ai *AnalogInputObject) GetOwner() *Device  { return ai.owner }
func (ai *AnalogInputObject) setOwner(d *Device) { ai.owner = d }

func (ai *AnalogInputObject) GetProperty(id bacnet.PropertyIdentifier) Property {
	return ai.properties[id]
}

func (ai *AnalogInputObject) AllPropertyIdentifiers() []bacnet.PropertyIdentifier {
	ids := make([]bacnet.PropertyIdentifier, 0, len(ai.properties))
	for id := range ai.properties {
		ids = append(ids, id)
	}
	return ids
}

// COVProperties returns PresentValue and StatusFlags as the COV-triggering properties.
func (ai *AnalogInputObject) COVProperties() []bacnet.PropertyIdentifier {
	return []bacnet.PropertyIdentifier{bacnet.PresentValue, bacnet.StatusFlags}
}

// Typed accessors.

func (ai *AnalogInputObject) GetPresentValue() float32 {
	return ai.properties[bacnet.PresentValue].GetValue().(*encoding.Real).Value()
}

func (ai *AnalogInputObject) SetPresentValue(v float32) {
	r := &encoding.Real{}
	r.SetValue(v)
	_ = ai.properties[bacnet.PresentValue].SetValue(r)
}

func (ai *AnalogInputObject) IsOutOfService() bool {
	return ai.properties[bacnet.OutOfService].GetValue().(*encoding.Boolean).Value()
}

func (ai *AnalogInputObject) SetOutOfService(v bool) {
	b := &encoding.Boolean{}
	b.SetValue(v)
	_ = ai.properties[bacnet.OutOfService].SetValue(b)
}

// SetStatusFlag sets the given StatusFlags bit. Clearing bits is not yet supported.
func (ai *AnalogInputObject) SetStatusFlag(flag bacnet.BACnetStatusFlag, set bool) {
	bs := ai.properties[bacnet.StatusFlags].GetValue().(*encoding.BitString)
	if set {
		bs.SetBit(uint(flag))
	}
}

func newStatusFlagsProperty() *BitStringProperty {
	bs := encoding.NewBitString()
	return &BitStringProperty{value: bs, readOnly: true}
}
