package objectmodel

import (
	"time"

	"github.com/REQUEA/bacnet"
)

type RemoteDevice struct {
	deviceObjectId        bacnet.BACnetObjectIdentifier
	address               *bacnet.BACnetAddress
	maxAPDULength         uint
	segmentationSupported bool
	vendorId              uint16
	announcementTime      time.Time
}

func (d *RemoteDevice) DeviceObjectId() bacnet.BACnetObjectIdentifier {
	return d.deviceObjectId
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

func (d *RemoteDevice) VendorId() uint16 {
	return d.vendorId
}

func (d *RemoteDevice) AnnouncementTime() time.Time {
	return d.announcementTime
}

type RemoteDeviceCache struct {
	remoteDevices []*RemoteDevice
}

func (c *RemoteDeviceCache) Add(device RemoteDevice) {
	for i, d := range c.remoteDevices {
		if d.address.Equal(device.address) {
			c.remoteDevices[i] = &device
			return
		}
	}
	c.remoteDevices = append(c.remoteDevices, &device)
}

func (c *RemoteDeviceCache) Get(addr *bacnet.BACnetAddress) *RemoteDevice {
	for _, d := range c.remoteDevices {
		if d.address.Equal(addr) {
			return d
		}
	}
	return nil
}
