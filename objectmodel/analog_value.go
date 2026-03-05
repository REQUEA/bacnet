package objectmodel

import (
	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/internal/encoding"
)

// AnalogValueObject implements the BACnet Analog Value object type.
type AnalogValueObject struct {
	owner      *Device
	properties map[bacnet.PropertyIdentifier]Property
}

func NewAnalogValueObject(
	instanceID uint32,
	name string,
	units bacnet.BACnetEngineeringUnits,
) *AnalogValueObject {
	properties := make(map[bacnet.PropertyIdentifier]Property)

	properties[bacnet.ObjectIdentifier] = NewObjectIdentifierProperty(
		true, uint16(bacnet.AnalogValue), instanceID,
	)
	properties[bacnet.ObjectName] = NewCharacterStringProperty(false, name)
	properties[bacnet.ObjectTypeProp] = NewEnumeratedProperty(true, uint32(bacnet.AnalogValue))
	properties[bacnet.PresentValue] = NewRealProperty(false, 0.0)
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

	return &AnalogValueObject{properties: properties}
}

func (av *AnalogValueObject) GetOwner() *Device  { return av.owner }
func (av *AnalogValueObject) setOwner(d *Device) { av.owner = d }

func (av *AnalogValueObject) GetProperty(id bacnet.PropertyIdentifier) Property {
	return av.properties[id]
}

func (av *AnalogValueObject) AllPropertyIdentifiers() []bacnet.PropertyIdentifier {
	ids := make([]bacnet.PropertyIdentifier, 0, len(av.properties))
	for id := range av.properties {
		ids = append(ids, id)
	}
	return ids
}

func (av *AnalogValueObject) COVProperties() []bacnet.PropertyIdentifier {
	return []bacnet.PropertyIdentifier{bacnet.PresentValue, bacnet.StatusFlags}
}

func (av *AnalogValueObject) GetPresentValue() float32 {
	return av.properties[bacnet.PresentValue].GetValue().(*encoding.Real).Value()
}

func (av *AnalogValueObject) SetPresentValue(v float32) {
	r := &encoding.Real{}
	r.SetValue(v)
	_ = av.properties[bacnet.PresentValue].SetValue(r)
}
