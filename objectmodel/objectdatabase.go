package objectmodel

import (
	"fmt"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/internal/encoding"
)

// ObjectModel is the interface for accessing local and remote devices.
type ObjectModel interface {
	AddDevice(device *Device) error
	AddObject(device *Device, obj Object) error
	GetDevices() []*Device
	FindDeviceByObjectID(objType bacnet.ObjectType, instance uint32) *Device
	GetObject(objType bacnet.ObjectType, instance uint32) Object
	AddRemoteDevice(device RemoteDevice)
	GetRemoteDevice(addr *bacnet.BACnetAddress) *RemoteDevice
	GetRemoteDeviceByInstance(instance uint32) *RemoteDevice
	GetAllRemoteDevices() []*RemoteDevice
}

// ObjectDatabase is the central object model for a BACnet stack.
// It holds local devices and delegates remote-device lookups to a RemoteDeviceCache.
type ObjectDatabase struct {
	devices      []*Device
	objectsByKey map[uint32]Object // global index: object identifier uint32 → Object
	remoteCache  *RemoteDeviceCache
}

// compile-time check
var _ ObjectModel = (*ObjectDatabase)(nil)

// NewObjectDatabase creates a new ObjectDatabase backed by the given RemoteDeviceCache.
func NewObjectDatabase(remoteCache *RemoteDeviceCache) *ObjectDatabase {
	return &ObjectDatabase{
		devices:      make([]*Device, 0),
		objectsByKey: make(map[uint32]Object),
		remoteCache:  remoteCache,
	}
}

// AddObject registers obj with device in this database.
// Returns an error if an object with the same (type, instance) is already registered
// on any device in this database.
func (db *ObjectDatabase) AddObject(device *Device, obj Object) error {
	p := obj.GetProperty(bacnet.ObjectIdentifier)
	if p == nil {
		return fmt.Errorf("object has no ObjectIdentifier property")
	}
	id, ok := p.GetValue().(*encoding.BACnetObjectIdentifier)
	if !ok {
		return fmt.Errorf("ObjectIdentifier property has wrong type")
	}
	key := uint32(id.ObjType())<<22 | id.Instance()&0x3fffff
	if _, exists := db.objectsByKey[key]; exists {
		return fmt.Errorf("object %d:%d already registered in database", id.ObjType(), id.Instance())
	}
	device.addObject(obj)
	db.objectsByKey[key] = obj
	return nil
}

// AddDevice registers a local device. Returns an error if a device with the same
// device-object instance is already registered.
func (db *ObjectDatabase) AddDevice(device *Device) error {
	devObj := device.DeviceObject()
	if devObj == nil {
		return fmt.Errorf("device has no DeviceObject")
	}
	idProp := devObj.GetProperty(bacnet.ObjectIdentifier)
	if idProp == nil {
		return fmt.Errorf("DeviceObject has no ObjectIdentifier property")
	}
	oid, ok := idProp.GetValue().(*encoding.BACnetObjectIdentifier)
	if !ok {
		return fmt.Errorf("ObjectIdentifier property has wrong type")
	}
	key := uint32(oid.ObjType())<<22 | oid.Instance()&0x3fffff
	if _, exists := db.objectsByKey[key]; exists {
		return fmt.Errorf("device with object identifier %d:%d already registered", oid.ObjType(), oid.Instance())
	}
	devObj.setOwner(device)
	db.objectsByKey[key] = devObj
	db.devices = append(db.devices, device)
	return nil
}

// GetDevices returns all registered local devices.
func (db *ObjectDatabase) GetDevices() []*Device {
	return db.devices
}

// FindDeviceByObjectID returns the local device whose DeviceObject has the given type and instance.
func (db *ObjectDatabase) FindDeviceByObjectID(objType bacnet.ObjectType, instance uint32) *Device {
	key := uint32(objType)<<22 | instance&0x3fffff
	if obj, ok := db.objectsByKey[key]; ok {
		return obj.GetOwner()
	}
	return nil
}

// GetObject returns the object matching the given type and instance, or nil.
func (db *ObjectDatabase) GetObject(objType bacnet.ObjectType, instance uint32) Object {
	return db.objectsByKey[uint32(objType)<<22|instance&0x3fffff]
}

// AddRemoteDevice adds or updates a remote device in the cache.
func (db *ObjectDatabase) AddRemoteDevice(device RemoteDevice) {
	db.remoteCache.Add(device)
}

// GetRemoteDevice returns the remote device at the given address, or nil.
func (db *ObjectDatabase) GetRemoteDevice(addr *bacnet.BACnetAddress) *RemoteDevice {
	return db.remoteCache.Get(addr)
}

// GetRemoteDeviceByInstance returns the first remote device with the given instance number, or nil.
func (db *ObjectDatabase) GetRemoteDeviceByInstance(instance uint32) *RemoteDevice {
	return db.remoteCache.GetByInstance(instance)
}

// GetAllRemoteDevices returns a snapshot of all known remote devices.
func (db *ObjectDatabase) GetAllRemoteDevices() []*RemoteDevice {
	return db.remoteCache.GetAll()
}
