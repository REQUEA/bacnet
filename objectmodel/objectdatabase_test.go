package objectmodel

import (
	"testing"

	"github.com/REQUEA/bacnet"
)

func TestObjectDatabase_AddDevice_Duplicate(t *testing.T) {
	cache := &RemoteDeviceCache{}
	db := NewObjectDatabase(cache)

	devObj := NewDeviceObject(
		bacnet.BACnetObjectIdentifier(uint32(bacnet.BacnetDevice)<<22|100),
		"TestDevice", bacnet.DeviceStatusOperational,
		"Vendor", 1, "Model", "1.0", "1.0",
		nil, nil, 1476, bacnet.SegmentationSupportNone,
		3000, 3, 1, 1,
	)
	dev := NewDevice(devObj)

	if err := db.AddDevice(dev); err != nil {
		t.Fatalf("first AddDevice failed: %v", err)
	}

	// Adding the same device again (same instance) must fail.
	dev2 := NewDevice(devObj)
	if err := db.AddDevice(dev2); err == nil {
		t.Fatal("expected error when adding duplicate device, got nil")
	}

	// Only one device should be registered.
	if got := len(db.GetDevices()); got != 1 {
		t.Errorf("expected 1 device, got %d", got)
	}
}

func TestObjectDatabase_AddObject_Duplicate(t *testing.T) {
	cache := &RemoteDeviceCache{}
	db := NewObjectDatabase(cache)

	devObj := NewDeviceObject(
		bacnet.BACnetObjectIdentifier(uint32(bacnet.BacnetDevice)<<22|200),
		"TestDevice2", bacnet.DeviceStatusOperational,
		"Vendor", 1, "Model", "1.0", "1.0",
		nil, nil, 1476, bacnet.SegmentationSupportNone,
		3000, 3, 1, 1,
	)
	dev := NewDevice(devObj)
	if err := db.AddDevice(dev); err != nil {
		t.Fatalf("AddDevice: %v", err)
	}

	// Add object A — should succeed.
	ai1 := NewAnalogInputObject(1, "AI1", bacnet.NoUnits)
	if err := db.AddObject(dev, ai1); err != nil {
		t.Fatalf("first AddObject failed: %v", err)
	}

	// Add object B with same (type, instance) — must fail.
	ai2 := NewAnalogInputObject(1, "AI1-dup", bacnet.NoUnits)
	if err := db.AddObject(dev, ai2); err == nil {
		t.Fatal("expected error when adding duplicate object, got nil")
	}

	// Object count must be unchanged (1 non-device object + the device itself in ObjectList).
	obj := dev.GetObject(bacnet.AnalogInput, 1)
	if obj == nil {
		t.Fatal("original object should still be present")
	}
	if obj.GetOwner() != dev {
		t.Error("owner should be dev")
	}
}

func TestObjectDatabase_AddObject_CrossDeviceConflict(t *testing.T) {
	cache := &RemoteDeviceCache{}
	db := NewObjectDatabase(cache)

	makeDevice := func(instance uint32) *Device {
		devObj := NewDeviceObject(
			bacnet.BACnetObjectIdentifier(uint32(bacnet.BacnetDevice)<<22|instance),
			"Dev", bacnet.DeviceStatusOperational,
			"Vendor", 1, "Model", "1.0", "1.0",
			nil, nil, 1476, bacnet.SegmentationSupportNone,
			3000, 3, 1, 1,
		)
		dev := NewDevice(devObj)
		if err := db.AddDevice(dev); err != nil {
			t.Fatalf("AddDevice(%d): %v", instance, err)
		}
		return dev
	}

	dev1 := makeDevice(300)
	dev2 := makeDevice(301)

	// Register AnalogValue:1 on device1 — must succeed.
	av1 := NewAnalogValueObject(1, "AV1", bacnet.NoUnits)
	if err := db.AddObject(dev1, av1); err != nil {
		t.Fatalf("first AddObject failed: %v", err)
	}

	// Register AnalogValue:1 on device2 — must fail (same key in global index).
	av2 := NewAnalogValueObject(1, "AV1-dev2", bacnet.NoUnits)
	if err := db.AddObject(dev2, av2); err == nil {
		t.Fatal("expected error when adding same key to different device, got nil")
	}

	// dev2 must have no non-device objects.
	if got := dev2.GetObject(bacnet.AnalogValue, 1); got != nil {
		t.Error("object should not have been registered on dev2")
	}
}
