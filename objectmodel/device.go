package objectmodel

import (
	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/internal/encoding"
)

type Device struct {
	deviceObject *DeviceObject
	objects      []Object
}

func NewDevice(deviceObject *DeviceObject) *Device {
	return &Device{
		deviceObject: deviceObject,
		objects:      make([]Object, 0),
	}
}

func (d *Device) DeviceObject() *DeviceObject {
	return d.deviceObject
}

// AddObject registers a non-device object with this device and appends its
// identifier to the device object's ObjectList property.
func (d *Device) AddObject(obj Object) {
	obj.setOwner(d)
	d.objects = append(d.objects, obj)
	if p := obj.GetProperty(bacnet.ObjectIdentifier); p != nil {
		if id, ok := p.GetValue().(*encoding.BACnetObjectIdentifier); ok {
			d.deviceObject.appendToObjectList(id)
		}
	}
}

// GetObject returns the first object matching the given type and instance, or nil.
func (d *Device) GetObject(objType bacnet.ObjectType, instance uint32) Object {
	for _, obj := range d.objects {
		p := obj.GetProperty(bacnet.ObjectIdentifier)
		if p == nil {
			continue
		}
		id, ok := p.GetValue().(*encoding.BACnetObjectIdentifier)
		if !ok {
			continue
		}
		if id.ObjType() == uint16(objType) && id.Instance() == instance {
			return obj
		}
	}
	return nil
}

type DeviceObject struct {
	properties map[bacnet.PropertyIdentifier]Property
}

func NewDeviceObject(
	id bacnet.BACnetObjectIdentifier,
	objectName string,
	deviceStatus bacnet.BACnetDeviceStatus,
	vendorName string,
	vendorId uint16,
	modelName string,
	firmwareRevision string,
	applicationSoftwareVersion string,
	servicesSupported []bacnet.BACnetServicesSupported,
	objectTypesSupported []bacnet.BACnetObjectTypesSupported,
	maxApduLenAccepted uint,
	segmentationSupported bacnet.SegmentationSupport,
	apduTimeout uint,
	numberOfApduRetries uint,
	databaseRevision uint,
	maxSegmentsAccepted uint,
) *DeviceObject {
	properties := make(map[bacnet.PropertyIdentifier]Property)

	properties[bacnet.ObjectIdentifier] = NewObjectIdentifierProperty(
		true, uint16(id>>22), uint32(id)&0x3fffff,
	)
	properties[bacnet.ObjectName] = NewCharacterStringProperty(false, objectName)
	properties[bacnet.ObjectTypeProp] = NewEnumeratedProperty(true, uint32(bacnet.BacnetDevice))

	properties[bacnet.SystemStatus] = NewEnumeratedProperty(false, uint32(deviceStatus))
	properties[bacnet.VendorName] = NewCharacterStringProperty(false, vendorName)
	properties[bacnet.VendorIdentifier] = NewUnsigned16Property(true, vendorId)
	properties[bacnet.ModelName] = NewCharacterStringProperty(true, modelName)
	properties[bacnet.FirmwareRevision] = NewCharacterStringProperty(false, firmwareRevision)
	properties[bacnet.ApplicationSoftwareVersion] = NewCharacterStringProperty(true, applicationSoftwareVersion)
	properties[bacnet.ProtocolVersion] = NewUnsignedProperty(true, ProtocolVersion)
	properties[bacnet.ProtocolRevision] = NewUnsignedProperty(true, ProtocolRevision)
	properties[bacnet.ProtocolServicesSupported] = NewServiceSupportedProperty(true, servicesSupported...)
	properties[bacnet.ProtocolObjectTypesSupported] = NewObjectTypesSupportedProperty(true, objectTypesSupported...)
	objectList := NewBACnetArrayProperty[*encoding.BACnetObjectIdentifier](true)
	selfId := &encoding.BACnetObjectIdentifier{}
	selfId.SetFromValues(uint16(id>>22), uint32(id)&0x3fffff)
	objectList.value.Append(selfId)
	properties[bacnet.ObjectList] = objectList
	properties[bacnet.MaxApduLengthAccepted] = NewUnsignedProperty(true, maxApduLenAccepted)
	properties[bacnet.SegmentationSupported] = NewEnumeratedProperty(true, uint32(segmentationSupported))
	properties[bacnet.ApduTimeout] = NewUnsignedProperty(true, apduTimeout)
	properties[bacnet.NumberOfApduRetries] = NewUnsignedProperty(true, numberOfApduRetries)
	properties[bacnet.DeviceAddressBinding] = NewBACnetListProperty[bacnet.BACnetAddresBinding](true)
	properties[bacnet.DatabaseRevision] = NewUnsignedProperty(true, databaseRevision)
	if segmentationSupported == bacnet.SegmentationSupportNone ||
		segmentationSupported == bacnet.SegmentationSupportTransmit {
		properties[bacnet.MaxSegmentsAccepted] = NewUnsignedProperty(true, 1)
	} else {
		properties[bacnet.MaxSegmentsAccepted] = NewUnsignedProperty(true, maxSegmentsAccepted)
	}
	properties[bacnet.ApduSegmentTimeout] = NewUnsignedProperty(false, 5000)

	propertyList := NewBACnetArrayProperty[*encoding.Enumerated](true)
	properties[bacnet.PropertyList] = propertyList

	propertyList.value.Set(
		encoding.NewEnumerated(uint32(bacnet.SystemStatus)),
		encoding.NewEnumerated(uint32(bacnet.VendorName)),
		encoding.NewEnumerated(uint32(bacnet.VendorIdentifier)),
		encoding.NewEnumerated(uint32(bacnet.ModelName)),
		encoding.NewEnumerated(uint32(bacnet.FirmwareRevision)),
		encoding.NewEnumerated(uint32(bacnet.ApplicationSoftwareVersion)),
		encoding.NewEnumerated(uint32(bacnet.ProtocolVersion)),
		encoding.NewEnumerated(uint32(bacnet.ProtocolRevision)),
		encoding.NewEnumerated(uint32(bacnet.ProtocolServicesSupported)),
		encoding.NewEnumerated(uint32(bacnet.ProtocolObjectTypesSupported)),
		encoding.NewEnumerated(uint32(bacnet.ObjectList)),
		encoding.NewEnumerated(uint32(bacnet.MaxApduLengthAccepted)),
		encoding.NewEnumerated(uint32(bacnet.SegmentationSupported)),
		encoding.NewEnumerated(uint32(bacnet.ApduTimeout)),
		encoding.NewEnumerated(uint32(bacnet.NumberOfApduRetries)),
		encoding.NewEnumerated(uint32(bacnet.DeviceAddressBinding)),
		encoding.NewEnumerated(uint32(bacnet.DatabaseRevision)),
		encoding.NewEnumerated(uint32(bacnet.MaxSegmentsAccepted)),
		encoding.NewEnumerated(uint32(bacnet.ApduSegmentTimeout)),
	)

	return &DeviceObject{
		properties: properties,
	}
}

func (o *DeviceObject) GetProperty(id bacnet.PropertyIdentifier) Property {
	p, ok := o.properties[id]
	if !ok {
		return nil
	}
	return p
}

func (o *DeviceObject) appendToObjectList(id *encoding.BACnetObjectIdentifier) {
	if p, ok := o.properties[bacnet.ObjectList].(*BACnetArrayProperty[*encoding.BACnetObjectIdentifier]); ok {
		p.value.Append(id)
	}
}

// AllPropertyIdentifiers returns all property identifiers present on the device object.
func (o *DeviceObject) AllPropertyIdentifiers() []bacnet.PropertyIdentifier {
	ids := make([]bacnet.PropertyIdentifier, 0, len(o.properties))
	for id := range o.properties {
		ids = append(ids, id)
	}
	return ids
}
