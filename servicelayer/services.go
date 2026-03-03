package servicelayer

import (
	"fmt"
	"time"

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

func (sh *ServiceHandler) HandleConfServIndication(
	indication *applicationlayer.APDUIndication,
	serviceChoice bacnet.BACnetConfirmedServiceChoice,
) {
	switch serviceChoice {
	case bacnet.ConfirmedServiceChoiceReadProperty:
		sh.handleReadProperty(indication)
	case bacnet.ConfirmedServiceChoiceWriteProperty:
		sh.handleWriteProperty(indication)
	case bacnet.ConfirmedServiceChoiceReadPropertyMultiple:
		sh.handleReadPropertyMultiple(indication)
	default:
		logger.Trace("unhandled confirmed service: ", serviceChoice)
		sh.applicationEntity.SendErrorResponse(
			indication.InvokeId,
			indication.Source,
			serviceChoice,
			bacnet.ServicesError,
			bacnet.ServiceRequestDenied,
		)
	}
}

func (sh *ServiceHandler) HandleConfServConfirm(
	indication *applicationlayer.APDUIndication,
	serviceChoice bacnet.BACnetConfirmedServiceChoice,
) {
	// TODO: implement in Phase 7
}

func (sh *ServiceHandler) HandleUnconfServIndication(
	indication *applicationlayer.APDUIndication,
	serviceChoice bacnet.BACnetUnconfirmedServiceChoice,
) {
	switch serviceChoice {
	case bacnet.UnconfirmedServiceChoiceWhoIs:
		sh.handleWhoIs(indication)
	case bacnet.UnconfirmedServiceChoiceIAm:
		sh.handleIAm(indication)
	default:
		logger.Trace("unconfirmed service not handled: ", serviceChoice)
	}
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


// handleReadProperty processes a confirmed ReadProperty request.
func (sh *ServiceHandler) handleReadProperty(indication *applicationlayer.APDUIndication) {
	var req ReadPropertyRequest
	_, err := req.Unmarshal(indication.Data)
	if err != nil {
		logger.Error("could not unmarshal ReadProperty request: ", err)
		sh.applicationEntity.SendErrorResponse(
			indication.InvokeId, indication.Source,
			bacnet.ConfirmedServiceChoiceReadProperty,
			bacnet.ServicesError, bacnet.ServiceRequestDenied,
		)
		return
	}
	device := sh.findDeviceByObjectId(req.objectIdentifier.ObjType(), req.objectIdentifier.Instance())
	if device == nil {
		sh.applicationEntity.SendErrorResponse(
			indication.InvokeId, indication.Source,
			bacnet.ConfirmedServiceChoiceReadProperty,
			bacnet.ObjectError, bacnet.UnknownObject,
		)
		return
	}
	devObj := device.DeviceObject()
	propId := bacnet.PropertyIdentifier(req.propertyIdentifier.Value())
	prop := devObj.GetProperty(propId)
	if prop == nil {
		sh.applicationEntity.SendErrorResponse(
			indication.InvokeId, indication.Source,
			bacnet.ConfirmedServiceChoiceReadProperty,
			bacnet.PropertyError, bacnet.UnknownProperty,
		)
		return
	}
	var valBytes []byte
	if req.propertyArrayIndex.Present() {
		idx := uint(req.propertyArrayIndex.Get().Value())
		arrayProp, ok := prop.(objectmodel.ArrayProperty)
		if !ok {
			sh.applicationEntity.SendErrorResponse(
				indication.InvokeId, indication.Source,
				bacnet.ConfirmedServiceChoiceReadProperty,
				bacnet.PropertyError, bacnet.PropertyIsNotAnArray,
			)
			return
		}
		elem, err := arrayProp.GetAt(idx)
		if err != nil {
			sh.applicationEntity.SendErrorResponse(
				indication.InvokeId, indication.Source,
				bacnet.ConfirmedServiceChoiceReadProperty,
				bacnet.PropertyError, bacnet.InvalidArrayIndex,
			)
			return
		}
		valBytes, err = elem.MarshalPrimitive()
		if err != nil {
			sh.applicationEntity.SendErrorResponse(
				indication.InvokeId, indication.Source,
				bacnet.ConfirmedServiceChoiceReadProperty,
				bacnet.PropertyError, bacnet.DatatypeNotSupported,
			)
			return
		}
	} else {
		var err error
		valBytes, err = prop.MarshalValue()
		if err != nil {
			logger.Error("could not marshal property value: ", err)
			sh.applicationEntity.SendErrorResponse(
				indication.InvokeId, indication.Source,
				bacnet.ConfirmedServiceChoiceReadProperty,
				bacnet.PropertyError, bacnet.DatatypeNotSupported,
			)
			return
		}
	}
	ack := ReadPropertyAck{}
	ack.objectIdentifier.SetFromValues(req.objectIdentifier.ObjType(), req.objectIdentifier.Instance())
	ack.propertyIdentifier.SetValue(req.propertyIdentifier.Value())
	if req.propertyArrayIndex.Present() {
		ack.propertyArrayIndex = req.propertyArrayIndex
	}
	ack.propertyValue = encoding.NewAbstract(valBytes)
	ackBytes, err := ack.Marshal()
	if err != nil {
		logger.Error("could not marshal ReadPropertyAck: ", err)
		return
	}
	if err := sh.applicationEntity.SendConfServResponse(
		indication.InvokeId, indication.Source,
		bacnet.ConfirmedServiceChoiceReadProperty,
		ackBytes,
	); err != nil {
		logger.Error("could not send ReadProperty response: ", err)
	}
}

// handleWriteProperty processes a confirmed WriteProperty request.
func (sh *ServiceHandler) handleWriteProperty(indication *applicationlayer.APDUIndication) {
	var req WritePropertyRequest
	_, err := req.Unmarshal(indication.Data)
	if err != nil {
		logger.Error("could not unmarshal WriteProperty request: ", err)
		sh.applicationEntity.SendErrorResponse(
			indication.InvokeId, indication.Source,
			bacnet.ConfirmedServiceChoiceWriteProperty,
			bacnet.ServicesError, bacnet.ServiceRequestDenied,
		)
		return
	}
	device := sh.findDeviceByObjectId(req.objectIdentifier.ObjType(), req.objectIdentifier.Instance())
	if device == nil {
		sh.applicationEntity.SendErrorResponse(
			indication.InvokeId, indication.Source,
			bacnet.ConfirmedServiceChoiceWriteProperty,
			bacnet.ObjectError, bacnet.UnknownObject,
		)
		return
	}
	devObj := device.DeviceObject()
	propId := bacnet.PropertyIdentifier(req.propertyIdentifier.Value())
	prop := devObj.GetProperty(propId)
	if prop == nil {
		sh.applicationEntity.SendErrorResponse(
			indication.InvokeId, indication.Source,
			bacnet.ConfirmedServiceChoiceWriteProperty,
			bacnet.PropertyError, bacnet.UnknownProperty,
		)
		return
	}
	if !prop.IsWritable() {
		sh.applicationEntity.SendErrorResponse(
			indication.InvokeId, indication.Source,
			bacnet.ConfirmedServiceChoiceWriteProperty,
			bacnet.PropertyError, bacnet.WriteAccessDenied,
		)
		return
	}
	if err := prop.UnmarshalValue(req.propertyValue.Value()); err != nil {
		sh.applicationEntity.SendErrorResponse(
			indication.InvokeId, indication.Source,
			bacnet.ConfirmedServiceChoiceWriteProperty,
			bacnet.PropertyError, bacnet.InvalidDataType,
		)
		return
	}
	// SimpleAck
	if err := sh.applicationEntity.SendConfServResponse(
		indication.InvokeId, indication.Source,
		bacnet.ConfirmedServiceChoiceWriteProperty,
		nil,
	); err != nil {
		logger.Error("could not send WriteProperty response: ", err)
	}
}

// handleWhoIs responds to a received WhoIs with an IAm for each matching local device.
func (sh *ServiceHandler) handleWhoIs(indication *applicationlayer.APDUIndication) {
	var req WhoIsRequest
	_, err := req.Unmarshal(indication.Data)
	if err != nil {
		logger.Error("could not unmarshal WhoIs request: ", err)
		return
	}
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
		instance := oid.Instance()
		if req.deviceInstanceRangeLow.Present() && req.deviceInstanceRangeHigh.Present() {
			low := uint32(req.deviceInstanceRangeLow.Get().Value())
			high := uint32(req.deviceInstanceRangeHigh.Get().Value())
			if instance < low || instance > high {
				continue
			}
		}
		if err := sh.sendIAmForDevice(device); err != nil {
			logger.Error("could not send IAm in response to WhoIs: ", err)
		}
	}
}

// handleIAm processes a received IAm and updates the remote device cache.
func (sh *ServiceHandler) handleIAm(indication *applicationlayer.APDUIndication) {
	var req IAmRequest
	_, err := req.Unmarshal(indication.Data)
	if err != nil {
		logger.Error("could not unmarshal IAm request: ", err)
		return
	}
	rawId := bacnet.BACnetObjectIdentifier(
		uint32(req.iAmDeviceIdentifier.ObjType())<<22 | req.iAmDeviceIdentifier.Instance(),
	)
	segVal := bacnet.SegmentationSupport(req.segmentationSupported.Value())
	segSupported := segVal == bacnet.SegmentationSupportBoth || segVal == bacnet.SegmentationSupportReceive
	device := objectmodel.NewRemoteDevice(
		rawId,
		indication.Source,
		uint(req.maxApduLengthAccepted.Value()),
		segSupported,
		req.vendorId.Value(),
		time.Now(),
	)
	sh.remoteDeviceCache.Add(device)
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
}

type UnconfirmedService interface {
}

// --- WhoIsRequest ---

type WhoIsRequest struct {
	deviceInstanceRangeLow  encoding.Optional[*encoding.Unsigned]
	deviceInstanceRangeHigh encoding.Optional[*encoding.Unsigned]
}

// NewWhoIsRequest creates a WhoIsRequest. Pass nil for both to query all devices.
func NewWhoIsRequest(low, high *uint32) *WhoIsRequest {
	r := &WhoIsRequest{}
	if low != nil {
		var lowVal encoding.Unsigned
		lowVal.SetValue(uint64(*low))
		r.deviceInstanceRangeLow.Set(&lowVal)
	}
	if high != nil {
		var highVal encoding.Unsigned
		highVal.SetValue(uint64(*high))
		r.deviceInstanceRangeHigh.Set(&highVal)
	}
	return r
}

func (r *WhoIsRequest) Unmarshal(buf []byte) ([]byte, error) {
	remaining := buf
	eltCount := 0
	for len(remaining) > 0 {
		tag, err := encoding.ReadTag(remaining)
		if err != nil {
			return remaining, fmt.Errorf("failed to read tag: %v", err)
		}
		switch tag {
		case 0:
			var low encoding.Unsigned
			remaining, err = low.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading low: %v", err)
			}
			r.deviceInstanceRangeLow.Set(&low)
			eltCount++
		case 1:
			var high encoding.Unsigned
			remaining, err = high.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading high: %v", err)
			}
			r.deviceInstanceRangeHigh.Set(&high)
			eltCount++
		default:
			return remaining, fmt.Errorf("unexpected tag %v", tag)
		}
	}
	if eltCount != 0 && eltCount != 2 {
		return remaining, fmt.Errorf("invalid who-is request: expected 0 or 2 range elements, got %d", eltCount)
	}
	return remaining, nil
}

func (r *WhoIsRequest) Marshal() ([]byte, error) {
	if !r.deviceInstanceRangeLow.Present() && !r.deviceInstanceRangeHigh.Present() {
		return []byte{}, nil // no range = WhoIs to all devices
	}
	if !r.deviceInstanceRangeLow.Present() || !r.deviceInstanceRangeHigh.Present() {
		return nil, fmt.Errorf("both deviceInstanceRangeLow and deviceInstanceRangeHigh must be present together")
	}
	result := make([]byte, 0)
	tmp, err := r.deviceInstanceRangeLow.Get().MarshalTagged(0)
	if err != nil {
		return nil, fmt.Errorf("could not marshal deviceInstanceRangeLow: %w", err)
	}
	result = append(result, tmp...)
	tmp, err = r.deviceInstanceRangeHigh.Get().MarshalTagged(1)
	if err != nil {
		return nil, fmt.Errorf("could not marshal deviceInstanceRangeHigh: %w", err)
	}
	result = append(result, tmp...)
	return result, nil
}

// --- IAmRequest ---

type IAmRequest struct {
	iAmDeviceIdentifier   encoding.BACnetObjectIdentifier
	maxApduLengthAccepted encoding.Unsigned
	segmentationSupported encoding.BACnetSegmentation
	vendorId              encoding.Unsigned16
}

// NewIAmRequest creates an IAmRequest ready for marshaling.
func NewIAmRequest(objType uint16, instance uint32, maxApduLengthAccepted uint64, segmentation uint32, vendorId uint16) *IAmRequest {
	r := &IAmRequest{}
	r.iAmDeviceIdentifier.SetFromValues(objType, instance)
	r.maxApduLengthAccepted.SetValue(maxApduLengthAccepted)
	r.segmentationSupported.SetValue(segmentation)
	r.vendorId.SetValue(vendorId)
	return r
}

func (r *IAmRequest) Unmarshal(buf []byte) ([]byte, error) {
	remaining, err := r.iAmDeviceIdentifier.Unmarshal(buf)
	if err != nil {
		return remaining, fmt.Errorf("could not read device identifier: %v", err)
	}
	remaining, err = r.maxApduLengthAccepted.Unmarshal(remaining)
	if err != nil {
		return remaining, fmt.Errorf("could not read max APDU length: %v", err)
	}
	remaining, err = r.segmentationSupported.Unmarshal(remaining)
	if err != nil {
		return remaining, fmt.Errorf("could not read segmentation supported: %v", err)
	}
	remaining, err = r.vendorId.Unmarshal(remaining)
	if err != nil {
		return remaining, fmt.Errorf("could not read vendor ID: %v", err)
	}
	return remaining, nil
}

func (r *IAmRequest) Marshal() ([]byte, error) {
	result := make([]byte, 0)
	tmp, err := r.iAmDeviceIdentifier.MarshalPrimitive()
	if err != nil {
		return nil, err
	}
	result = append(result, tmp...)
	tmp, err = r.maxApduLengthAccepted.MarshalPrimitive()
	if err != nil {
		return nil, err
	}
	result = append(result, tmp...)
	tmp, err = r.segmentationSupported.MarshalPrimitive()
	if err != nil {
		return nil, err
	}
	result = append(result, tmp...)
	tmp, err = r.vendorId.MarshalPrimitive()
	if err != nil {
		return nil, err
	}
	result = append(result, tmp...)
	return result, nil
}

// --- ReadPropertyService ---

type ReadPropertyService struct {
	serviceLayer *ServiceHandler
}

func (s *ReadPropertyService) GetDevice(request []byte) *objectmodel.Device {
	return nil
}

type ReadPropertyRequest struct {
	objectIdentifier   encoding.BACnetObjectIdentifier
	propertyIdentifier encoding.BACnetPropertyIdentifier
	propertyArrayIndex encoding.Optional[*encoding.Unsigned]
}

func (r *ReadPropertyRequest) Unmarshal(buf []byte) ([]byte, error) {
	remaining := buf
	for len(remaining) > 0 {
		tag, err := encoding.ReadTag(remaining)
		if err != nil {
			return remaining, fmt.Errorf("failed to read tag: %v", err)
		}
		switch tag {
		case 0:
			remaining, err = r.objectIdentifier.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading object-identifier: %v", err)
			}
		case 1:
			remaining, err = r.propertyIdentifier.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading property-identifier: %v", err)
			}
		case 2:
			var arrayIndex encoding.Unsigned
			remaining, err = arrayIndex.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading property-array-index: %v", err)
			}
			r.propertyArrayIndex.Set(&arrayIndex)
		default:
			return remaining, fmt.Errorf("unexpected tag %v", tag)
		}
	}
	return remaining, nil
}

func (r *ReadPropertyRequest) Marshal() ([]byte, error) {
	result := make([]byte, 0)
	tmp, err := r.objectIdentifier.MarshalTagged(0)
	if err != nil {
		return nil, err
	}
	result = append(result, tmp...)
	tmp, err = r.propertyIdentifier.MarshalTagged(1)
	if err != nil {
		return nil, err
	}
	result = append(result, tmp...)
	if r.propertyArrayIndex.Present() {
		tmp, err = r.propertyArrayIndex.Get().MarshalTagged(2)
		if err != nil {
			return nil, err
		}
		result = append(result, tmp...)
	}
	return result, nil
}

type ReadPropertyAck struct {
	objectIdentifier   encoding.BACnetObjectIdentifier
	propertyIdentifier encoding.BACnetPropertyIdentifier
	propertyArrayIndex encoding.Optional[*encoding.Unsigned]
	propertyValue      encoding.Abstract
}

func (r *ReadPropertyAck) Unmarshal(buf []byte) ([]byte, error) {
	remaining := buf
	for len(remaining) > 0 {
		tag, err := encoding.ReadTag(remaining)
		if err != nil {
			return remaining, fmt.Errorf("failed to read tag: %v", err)
		}
		switch tag {
		case 0:
			remaining, err = r.objectIdentifier.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading object-identifier: %v", err)
			}
		case 1:
			remaining, err = r.propertyIdentifier.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading property-identifier: %v", err)
			}
		case 2:
			var arrayIndex encoding.Unsigned
			remaining, err = arrayIndex.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading property-array-index: %v", err)
			}
			r.propertyArrayIndex.Set(&arrayIndex)
		case 3:
			remaining, err = r.propertyValue.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading property-value: %v", err)
			}
		default:
			return remaining, fmt.Errorf("unexpected tag %v", tag)
		}
	}
	return remaining, nil
}

func (a *ReadPropertyAck) Marshal() ([]byte, error) {
	result := make([]byte, 0)
	tmp, err := a.objectIdentifier.MarshalTagged(0)
	if err != nil {
		return nil, err
	}
	result = append(result, tmp...)
	tmp, err = a.propertyIdentifier.MarshalTagged(1)
	if err != nil {
		return nil, err
	}
	result = append(result, tmp...)
	if a.propertyArrayIndex.Present() {
		tmp, err = a.propertyArrayIndex.Get().MarshalTagged(2)
		if err != nil {
			return nil, err
		}
		result = append(result, tmp...)
	}
	tmp, err = a.propertyValue.MarshalTagged(3)
	if err != nil {
		return nil, err
	}
	result = append(result, tmp...)
	return result, nil
}

// --- WritePropertyRequest ---

type WritePropertyRequest struct {
	objectIdentifier   encoding.BACnetObjectIdentifier
	propertyIdentifier encoding.BACnetPropertyIdentifier
	propertyArrayIndex encoding.Optional[*encoding.Unsigned]
	propertyValue      encoding.Abstract
	priority           encoding.Optional[*encoding.Unsigned]
}

func (r *WritePropertyRequest) Unmarshal(buf []byte) ([]byte, error) {
	remaining := buf
	for len(remaining) > 0 {
		tag, err := encoding.ReadTag(remaining)
		if err != nil {
			return remaining, fmt.Errorf("failed to read tag: %v", err)
		}
		switch tag {
		case 0:
			remaining, err = r.objectIdentifier.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading object-identifier: %v", err)
			}
		case 1:
			remaining, err = r.propertyIdentifier.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading property-identifier: %v", err)
			}
		case 2:
			var arrayIndex encoding.Unsigned
			remaining, err = arrayIndex.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading property-array-index: %v", err)
			}
			r.propertyArrayIndex.Set(&arrayIndex)
		case 3:
			remaining, err = r.propertyValue.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading property-value: %v", err)
			}
		case 4:
			var prio encoding.Unsigned
			remaining, err = prio.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading priority: %v", err)
			}
			r.priority.Set(&prio)
		default:
			return remaining, fmt.Errorf("unexpected tag %v", tag)
		}
	}
	return remaining, nil
}

func (r *WritePropertyRequest) Marshal() ([]byte, error) {
	result := make([]byte, 0)
	tmp, err := r.objectIdentifier.MarshalTagged(0)
	if err != nil {
		return nil, err
	}
	result = append(result, tmp...)
	tmp, err = r.propertyIdentifier.MarshalTagged(1)
	if err != nil {
		return nil, err
	}
	result = append(result, tmp...)
	if r.propertyArrayIndex.Present() {
		tmp, err = r.propertyArrayIndex.Get().MarshalTagged(2)
		if err != nil {
			return nil, err
		}
		result = append(result, tmp...)
	}
	tmp, err = r.propertyValue.MarshalTagged(3)
	if err != nil {
		return nil, err
	}
	result = append(result, tmp...)
	if r.priority.Present() {
		tmp, err = r.priority.Get().MarshalTagged(4)
		if err != nil {
			return nil, err
		}
		result = append(result, tmp...)
	}
	return result, nil
}
