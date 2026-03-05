package servicelayer

import (
	"fmt"
	"sync"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/applicationlayer"
	"github.com/REQUEA/bacnet/internal/encoding"
	"github.com/REQUEA/bacnet/logger"
	"github.com/REQUEA/bacnet/networklayer"
	"github.com/REQUEA/bacnet/objectmodel"
)

// Compile-time check: ServiceHandler implements applicationlayer.ServiceHandler.
var _ applicationlayer.ServiceHandler = (*ServiceHandler)(nil)

type ServiceHandler struct {
	devices             []*objectmodel.Device
	applicationEntity   *applicationlayer.ApplicationEntity
	remoteDeviceCache   *objectmodel.RemoteDeviceCache
	confirmedServices   map[bacnet.BACnetConfirmedServiceChoice]ConfirmedService
	unconfirmedServices map[bacnet.BACnetUnconfirmedServiceChoice]UnconfirmedService
	covSubscriptions    []*covSubscription
	covMu               sync.Mutex
	covNotifyCallback   func(COVNotificationRequest)
}

// NewServiceHandler creates a ServiceHandler for the given device and wires it into the ApplicationEntity.
func NewServiceHandler(ae *applicationlayer.ApplicationEntity, device *objectmodel.Device) *ServiceHandler {
	sh := &ServiceHandler{
		devices:             []*objectmodel.Device{device},
		applicationEntity:   ae,
		remoteDeviceCache:   ae.RemoteDeviceCache(),
		confirmedServices:   make(map[bacnet.BACnetConfirmedServiceChoice]ConfirmedService),
		unconfirmedServices: make(map[bacnet.BACnetUnconfirmedServiceChoice]UnconfirmedService),
	}
	sh.RegisterConfirmedService(bacnet.ConfirmedServiceChoiceReadProperty, &ReadPropertyService{sh})
	sh.RegisterConfirmedService(bacnet.ConfirmedServiceChoiceWriteProperty, &WritePropertyService{sh})
	sh.RegisterConfirmedService(bacnet.ConfirmedServiceChoiceReadPropertyMultiple, &ReadPropertyMultipleService{sh})
	sh.RegisterConfirmedService(bacnet.ConfirmedServiceChoiceWritePropertyMultiple, &WritePropertyMultipleService{sh})
	sh.RegisterConfirmedService(bacnet.ConfirmedServiceChoiceReadRange, &ReadRangeService{sh})
	sh.RegisterUnconfirmedService(bacnet.UnconfirmedServiceChoiceWhoIs, &WhoIsService{sh})
	sh.RegisterUnconfirmedService(bacnet.UnconfirmedServiceChoiceIAm, &IAmService{sh})
	ae.SetServiceLayer(sh)
	return sh
}

func (sh *ServiceHandler) GetDevice(
	serviceChoice bacnet.BACnetConfirmedServiceChoice,
	request []byte,
) *objectmodel.Device {
	service, ok := sh.confirmedServices[serviceChoice]
	if !ok {
		// Fall back to first registered device for built-in confirmed services.
		if len(sh.devices) > 0 {
			return sh.devices[0]
		}
		return nil
	}
	return service.GetDevice(request)
}

func (sh *ServiceHandler) RegisterConfirmedService(
	serviceChoice bacnet.BACnetConfirmedServiceChoice,
	service ConfirmedService,
) {
	sh.confirmedServices[serviceChoice] = service
}

func (sh *ServiceHandler) RegisterUnconfirmedService(
	serviceChoice bacnet.BACnetUnconfirmedServiceChoice,
	service UnconfirmedService,
) {
	sh.unconfirmedServices[serviceChoice] = service
}

func (sh *ServiceHandler) HandleConfServIndication(
	indication *applicationlayer.APDUIndication,
	serviceChoice bacnet.BACnetConfirmedServiceChoice,
) {
	srv, ok := sh.confirmedServices[serviceChoice]
	if ok {
		srv.HandleConfServIndication(indication)
		return
	}
	logger.Trace("unhandled confirmed service: ", serviceChoice)
	sh.applicationEntity.SendErrorResponse(
		indication.InvokeId,
		indication.Source,
		serviceChoice,
		bacnet.ServicesError,
		bacnet.ServiceRequestDenied,
	)
}

func (sh *ServiceHandler) HandleConfServConfirm(
	indication *applicationlayer.APDUIndication,
	serviceChoice bacnet.BACnetConfirmedServiceChoice,
) {
	srv, ok := sh.confirmedServices[serviceChoice]
	if ok {
		if h, ok := srv.(ConfirmedServiceConfirmHandler); ok {
			h.HandleConfServConfirm(indication)
			return
		}
	}
	logger.Trace("confirmed service confirm not handled: ", serviceChoice)
}

func (sh *ServiceHandler) HandleUnconfServIndication(
	indication *applicationlayer.APDUIndication,
	serviceChoice bacnet.BACnetUnconfirmedServiceChoice,
) {
	srv, ok := sh.unconfirmedServices[serviceChoice]
	if ok {
		srv.HandleUnconfServIndication(indication)
		return
	}
	logger.Trace("unconfirmed service not handled: ", serviceChoice)
}

func (sh *ServiceHandler) HandleSegmentAckIndication(
	indication *applicationlayer.APDUIndication,
	serviceChoice bacnet.BACnetConfirmedServiceChoice,
) {
	// TODO: implement
}

func (sh *ServiceHandler) HandleRejectIndication(
	indication *applicationlayer.APDUIndication,
	serviceChoice bacnet.BACnetConfirmedServiceChoice,
) {
	// TODO: implement
}

func (sh *ServiceHandler) HandleAbortIndication(
	indication *applicationlayer.APDUIndication,
	serviceChoice bacnet.BACnetConfirmedServiceChoice,
	reason uint8,
) {
	// TODO: implement
}

// findDeviceForRequest decodes the first BACnetObjectIdentifier in the request bytes
// and returns the local device that owns the named object.
// Returns nil if the bytes cannot be decoded or the object is not found.
func (sh *ServiceHandler) findDeviceForRequest(request []byte) *objectmodel.Device {
	var oid encoding.BACnetObjectIdentifier
	if _, err := oid.Unmarshal(request); err != nil {
		return nil
	}
	if d := sh.findDeviceByObjectId(oid.ObjType(), oid.Instance()); d != nil {
		return d
	}
	for _, dev := range sh.devices {
		if obj := dev.GetObject(bacnet.ObjectType(oid.ObjType()), oid.Instance()); obj != nil {
			return obj.GetOwner()
		}
	}
	return nil
}

// findDeviceByObjectId returns the local device whose DeviceObject has the given type+instance.
func (sh *ServiceHandler) findDeviceByObjectId(objType uint16, instance uint32) *objectmodel.Device {
	for _, device := range sh.devices {
		devObj := device.DeviceObject()
		if devObj == nil {
			continue
		}
		idProp := devObj.GetProperty(bacnet.ObjectIdentifier)
		if idProp == nil {
			continue
		}
		oid, ok := idProp.GetValue().(*encoding.BACnetObjectIdentifier)
		if !ok {
			continue
		}
		if oid.Instance() == instance && oid.ObjType() == objType {
			return device
		}
	}
	return nil
}

// propertySource is satisfied by DeviceObject and any Object (AnalogInput, etc.).
type propertySource interface {
	GetProperty(bacnet.PropertyIdentifier) objectmodel.Property
	AllPropertyIdentifiers() []bacnet.PropertyIdentifier
}

// resolveObject finds the property source for the given object identifier across all
// registered devices, checking device objects first then non-device objects.
func (sh *ServiceHandler) resolveObject(objType uint16, instance uint32) (propertySource, *objectmodel.Device) {
	device := sh.findDeviceByObjectId(objType, instance)
	if device != nil {
		return device.DeviceObject(), device
	}
	for _, dev := range sh.devices {
		obj := dev.GetObject(bacnet.ObjectType(objType), instance)
		if obj != nil {
			return obj, obj.GetOwner()
		}
	}
	return nil, nil
}

// sendIAmForDevice broadcasts an IAm for the given local device.
func (sh *ServiceHandler) sendIAmForDevice(device *objectmodel.Device) error {
	devObj := device.DeviceObject()
	idProp := devObj.GetProperty(bacnet.ObjectIdentifier)
	if idProp == nil {
		return fmt.Errorf("device object has no ObjectIdentifier property")
	}
	oid, ok := idProp.GetValue().(*encoding.BACnetObjectIdentifier)
	if !ok {
		return fmt.Errorf("wrong type for ObjectIdentifier property")
	}
	maxApduProp := devObj.GetProperty(bacnet.MaxApduLengthAccepted)
	if maxApduProp == nil {
		return fmt.Errorf("device object has no MaxApduLengthAccepted property")
	}
	maxApduUnsigned, ok := maxApduProp.GetValue().(*encoding.Unsigned)
	if !ok {
		return fmt.Errorf("wrong type for MaxApduLengthAccepted property")
	}
	segProp := devObj.GetProperty(bacnet.SegmentationSupported)
	if segProp == nil {
		return fmt.Errorf("device object has no SegmentationSupported property")
	}
	segEnum, ok := segProp.GetValue().(*encoding.Enumerated)
	if !ok {
		return fmt.Errorf("wrong type for SegmentationSupported property")
	}
	vendorProp := devObj.GetProperty(bacnet.VendorIdentifier)
	if vendorProp == nil {
		return fmt.Errorf("device object has no VendorIdentifier property")
	}
	vendorUnsigned, ok := vendorProp.GetValue().(*encoding.Unsigned16)
	if !ok {
		return fmt.Errorf("wrong type for VendorIdentifier property")
	}
	iamReq := NewIAmRequest(
		oid.ObjType(), oid.Instance(),
		maxApduUnsigned.Value(), segEnum.Value(), vendorUnsigned.Value(),
	)
	data, err := iamReq.Marshal()
	if err != nil {
		return fmt.Errorf("could not marshal IAm request: %w", err)
	}
	dest := &bacnet.BACnetAddress{Network: bacnet.BroadcastDNET}
	return sh.applicationEntity.SendIAmRequest(dest, networklayer.NormalPriority, data)
}

// SetCOVNotificationCallback registers a callback invoked when this stack
// receives a COV notification (confirmed or unconfirmed).
func (sh *ServiceHandler) SetCOVNotificationCallback(cb func(COVNotificationRequest)) {
	sh.covMu.Lock()
	sh.covNotifyCallback = cb
	sh.covMu.Unlock()
}

// Device returns the first local device registered with this handler.
func (sh *ServiceHandler) Device() *objectmodel.Device {
	if len(sh.devices) > 0 {
		return sh.devices[0]
	}
	return nil
}

// AddDevice appends an additional local device to this service handler.
func (sh *ServiceHandler) AddDevice(device *objectmodel.Device) {
	sh.devices = append(sh.devices, device)
}

// WhoIs sends a WhoIs broadcast. Pass nil for both limits to query all devices.
func (sh *ServiceHandler) WhoIs(lowLimit, highLimit *uint32) error {
	req := NewWhoIsRequest(lowLimit, highLimit)
	data, err := req.Marshal()
	if err != nil {
		return fmt.Errorf("could not marshal WhoIs request: %w", err)
	}
	dest := &bacnet.BACnetAddress{Network: bacnet.BroadcastDNET}
	return sh.applicationEntity.SendWhoIsRequest(dest, networklayer.NormalPriority, data)
}

// IAmBroadcast sends an IAm broadcast for all local devices.
func (sh *ServiceHandler) IAmBroadcast() error {
	for _, device := range sh.devices {
		if err := sh.sendIAmForDevice(device); err != nil {
			logger.Error("IAm broadcast failed: ", err)
		}
	}
	return nil
}

type ConfirmedService interface {
	GetDevice(serviceRequest []byte) *objectmodel.Device
	HandleConfServIndication(*applicationlayer.APDUIndication)
}

type UnconfirmedService interface {
	HandleUnconfServIndication(*applicationlayer.APDUIndication)
}

// ConfirmedServiceConfirmHandler is an optional extension of ConfirmedService
// for services that must observe when their outgoing request is acknowledged.
type ConfirmedServiceConfirmHandler interface {
	HandleConfServConfirm(*applicationlayer.APDUIndication)
}

// Helpers

// openingTag returns the byte for a context-class opening tag with the given tag number.
func openingTag(n byte) byte { return (n << 4) | 0x0E }

// closingTag returns the byte for a context-class closing tag with the given tag number.
func closingTag(n byte) byte { return (n << 4) | 0x0F }
