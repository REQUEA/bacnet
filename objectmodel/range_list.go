package objectmodel

import (
	"fmt"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/internal/encoding"
)

// RangeListObject is a BACnet object whose PresentValue property stores an ordered
// list of raw application-tagged bytes and supports ReadRange operations.
// It is useful for trending and logging scenarios.
type RangeListObject struct {
	owner      *Device
	properties map[bacnet.PropertyIdentifier]Property
	listProp   *RangeListProperty
}

func NewRangeListObject(objType uint16, instance uint32, name string) *RangeListObject {
	properties := make(map[bacnet.PropertyIdentifier]Property)

	properties[bacnet.ObjectIdentifier] = NewObjectIdentifierProperty(true, objType, instance)
	properties[bacnet.ObjectName] = NewCharacterStringProperty(false, name)
	properties[bacnet.ObjectTypeProp] = NewEnumeratedProperty(true, uint32(objType))

	lp := &RangeListProperty{}
	properties[bacnet.LogBuffer] = lp

	propertyList := NewBACnetArrayProperty[*encoding.Enumerated](true)
	propertyList.value.Set(encoding.NewEnumerated(uint32(bacnet.LogBuffer)))
	properties[bacnet.PropertyList] = propertyList

	return &RangeListObject{
		properties: properties,
		listProp:   lp,
	}
}

// AppendItem appends a raw application-tagged byte slice to the list.
func (o *RangeListObject) AppendItem(item []byte) {
	o.listProp.items = append(o.listProp.items, item)
}

func (o *RangeListObject) GetOwner() *Device                      { return o.owner }
func (o *RangeListObject) setOwner(d *Device)                     { o.owner = d }
func (o *RangeListObject) COVProperties() []bacnet.PropertyIdentifier { return nil }

func (o *RangeListObject) GetProperty(id bacnet.PropertyIdentifier) Property {
	return o.properties[id]
}

func (o *RangeListObject) AllPropertyIdentifiers() []bacnet.PropertyIdentifier {
	ids := make([]bacnet.PropertyIdentifier, 0, len(o.properties))
	for id := range o.properties {
		ids = append(ids, id)
	}
	return ids
}

// RangeListProperty is a Property that implements RangeProperty.
type RangeListProperty struct {
	items [][]byte
}

func (p *RangeListProperty) GetValue() any     { return p.items }
func (p *RangeListProperty) IsWritable() bool  { return false }
func (p *RangeListProperty) SetValue(any) error { return fmt.Errorf("read-only") }

func (p *RangeListProperty) MarshalValue() ([]byte, error) {
	var result []byte
	for _, item := range p.items {
		result = append(result, item...)
	}
	return result, nil
}

func (p *RangeListProperty) UnmarshalValue([]byte) error {
	return fmt.Errorf("RangeListProperty does not support UnmarshalValue")
}

func (p *RangeListProperty) ReadByPosition(ref uint32, count int32) (*RangeReadResult, error) {
	if ref < 1 {
		ref = 1
	}
	start := int(ref) - 1
	if start >= len(p.items) {
		return &RangeReadResult{FirstItem: true, LastItem: true}, nil
	}
	end := start + int(count)
	if count <= 0 || end > len(p.items) {
		end = len(p.items)
	}
	return &RangeReadResult{
		Items:     p.items[start:end],
		FirstItem: start == 0,
		LastItem:  end == len(p.items),
		MoreItems: end < len(p.items),
	}, nil
}

func (p *RangeListProperty) ReadBySequenceNumber(seqNum uint32, count int32) (*RangeReadResult, error) {
	return p.ReadByPosition(seqNum, count)
}

func (p *RangeListProperty) ReadByTime(refTime encoding.BACnetDateTime, count int32) (*RangeReadResult, error) {
	return p.ReadByPosition(1, count)
}
