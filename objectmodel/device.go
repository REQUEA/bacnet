package objectmodel

import "github.com/REQUEA/bacnet"

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

	properties[bacnet.ObjectIdentifier] = NewObjectIdentifierProperty(true, id)
	properties[bacnet.ObjectName] = NewCharacterStringProperty(false, objectName)
	properties[bacnet.ObjectTypeProp] = NewObjectTypeProperty(true, bacnet.BacnetDevice)

	properties[bacnet.SystemStatus] = NewDeviceStatusProperty(false, deviceStatus)
	properties[bacnet.VendorName] = NewCharacterStringProperty(false, vendorName)
	properties[bacnet.VendorIdentifier] = NewUnsigned16Property(true, vendorId)
	properties[bacnet.ModelName] = NewCharacterStringProperty(true, modelName)
	properties[bacnet.FirmwareRevision] = NewCharacterStringProperty(false, firmwareRevision)
	properties[bacnet.ApplicationSoftwareVersion] = NewCharacterStringProperty(true, applicationSoftwareVersion)
	properties[bacnet.ProtocolVersion] = NewUnsignedProperty(true, ProtocolVersion)
	properties[bacnet.ProtocolRevision] = NewUnsignedProperty(true, ProtocolRevision)
	properties[bacnet.ProtocolServicesSupported] = NewServiceSupportedProperty(true, servicesSupported...)
	properties[bacnet.ProtocolObjectTypesSupported] = NewObjectTypesSupportedProperty(true, objectTypesSupported...)
	properties[bacnet.ObjectList] = NewBACnetArrayProperty[bacnet.BACnetObjectIdentifier](true, 0, true)
	properties[bacnet.MaxApduLengthAccepted] = NewUnsignedProperty(true, maxApduLenAccepted)
	properties[bacnet.SegmentationSupported] = NewSegmentationProperty(true, segmentationSupported)
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
	properties[bacnet.ApduSegmentTimeout] = NewUnsignedProperty(false, 5000) // 5 seconds default

	propertyList := NewBACnetArrayProperty[bacnet.PropertyIdentifier](true, 19, true)
	properties[bacnet.PropertyList] = propertyList

	propertyArray := propertyList.GetValue().(bacnet.BACnetArray[bacnet.PropertyIdentifier])
	propertyArray.Set(
		bacnet.SystemStatus,
		bacnet.VendorName,
		bacnet.VendorIdentifier,
		bacnet.ModelName,
		bacnet.FirmwareRevision,
		bacnet.ApplicationSoftwareVersion,
		bacnet.ProtocolVersion,
		bacnet.ProtocolRevision,
		bacnet.ProtocolServicesSupported,
		bacnet.ProtocolObjectTypesSupported,
		bacnet.ObjectList,
		bacnet.MaxApduLengthAccepted,
		bacnet.SegmentationSupported,
		bacnet.ApduTimeout,
		bacnet.NumberOfApduRetries,
		bacnet.DeviceAddressBinding,
		bacnet.DatabaseRevision,
		bacnet.MaxSegmentsAccepted,
		bacnet.ApduSegmentTimeout,
	)

	result := &DeviceObject{
		properties: properties,
	}
	return result
}

func (o *DeviceObject) GetProperty(id bacnet.PropertyIdentifier) Property {
	p, ok := o.properties[id]
	if !ok {
		return nil
	}
	return p
}
