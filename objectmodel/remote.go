package objectmodel

import (
	"sync"
	"time"

	"github.com/REQUEA/bacnet"
)

type RemoteDevice struct {
	deviceObjectID        bacnet.BACnetObjectIdentifier
	address               *bacnet.BACnetAddress
	maxAPDULength         uint
	segmentationSupported bool
	vendorID              uint16
	announcementTime      time.Time
}

func (d *RemoteDevice) DeviceObjectID() bacnet.BACnetObjectIdentifier {
	return d.deviceObjectID
}

func (d *RemoteDevice) Address() *bacnet.BACnetAddress {
	return d.address
}

func (d *RemoteDevice) MaxAPDULength() uint {
	return d.maxAPDULength
}

func (d *RemoteDevice) SegmentationSupported() bool {
	return d.segmentationSupported
}

func (d *RemoteDevice) VendorID() uint16 {
	return d.vendorID
}

func (d *RemoteDevice) AnnouncementTime() time.Time {
	return d.announcementTime
}

func NewRemoteDevice(
	deviceObjectID bacnet.BACnetObjectIdentifier,
	address *bacnet.BACnetAddress,
	maxAPDULength uint,
	segmentationSupported bool,
	vendorID uint16,
	announcementTime time.Time,
) RemoteDevice {
	return RemoteDevice{
		deviceObjectID:        deviceObjectID,
		address:               address,
		maxAPDULength:         maxAPDULength,
		segmentationSupported: segmentationSupported,
		vendorID:              vendorID,
		announcementTime:      announcementTime,
	}
}

type RemoteDeviceCache struct {
	mu            sync.RWMutex
	remoteDevices []*RemoteDevice
}

func (c *RemoteDeviceCache) Add(device RemoteDevice) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, d := range c.remoteDevices {
		if d.address.Equal(device.address) {
			c.remoteDevices[i] = &device
			return
		}
	}
	c.remoteDevices = append(c.remoteDevices, &device)
}

func (c *RemoteDeviceCache) Get(addr *bacnet.BACnetAddress) *RemoteDevice {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, d := range c.remoteDevices {
		if d.address.Equal(addr) {
			return d
		}
	}
	return nil
}

// GetByInstance returns the first remote device with the given device instance number, or nil.
func (c *RemoteDeviceCache) GetByInstance(instance uint32) *RemoteDevice {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, d := range c.remoteDevices {
		if uint32(d.deviceObjectID)&0x3fffff == instance {
			return d
		}
	}
	return nil
}

// GetAll returns a snapshot of all known remote devices.
func (c *RemoteDeviceCache) GetAll() []*RemoteDevice {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]*RemoteDevice, len(c.remoteDevices))
	copy(result, c.remoteDevices)
	return result
}
