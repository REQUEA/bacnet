package servicelayer

import (
	"fmt"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/applicationlayer"
	"github.com/REQUEA/bacnet/internal/encoding"
	"github.com/REQUEA/bacnet/logger"
	"github.com/REQUEA/bacnet/objectmodel"
)

type ReadPropertyService struct {
	serviceHandler *ServiceHandler
}

func (s *ReadPropertyService) GetDevice(request []byte) *objectmodel.Device {
	return s.serviceHandler.findDeviceForRequest(request)
}

func (s *ReadPropertyService) HandleConfServIndication(indication *applicationlayer.APDUIndication) {
	var req ReadPropertyRequest
	_, err := req.Unmarshal(indication.Data)
	if err != nil {
		logger.Error("could not unmarshal ReadProperty request: ", err)
		s.serviceHandler.applicationEntity.SendErrorResponse(
			indication.InvokeId, indication.Source,
			bacnet.ConfirmedServiceChoiceReadProperty,
			bacnet.ServicesError, bacnet.ServiceRequestDenied,
		)
		return
	}
	src, _ := s.serviceHandler.resolveObject(req.objectIdentifier.ObjType(), req.objectIdentifier.Instance())
	if src == nil {
		s.serviceHandler.applicationEntity.SendErrorResponse(
			indication.InvokeId, indication.Source,
			bacnet.ConfirmedServiceChoiceReadProperty,
			bacnet.ObjectError, bacnet.UnknownObject,
		)
		return
	}
	propId := bacnet.PropertyIdentifier(req.propertyIdentifier.Value())
	prop := src.GetProperty(propId)
	if prop == nil {
		s.serviceHandler.applicationEntity.SendErrorResponse(
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
			s.serviceHandler.applicationEntity.SendErrorResponse(
				indication.InvokeId, indication.Source,
				bacnet.ConfirmedServiceChoiceReadProperty,
				bacnet.PropertyError, bacnet.PropertyIsNotAnArray,
			)
			return
		}
		elem, err := arrayProp.GetAt(idx)
		if err != nil {
			s.serviceHandler.applicationEntity.SendErrorResponse(
				indication.InvokeId, indication.Source,
				bacnet.ConfirmedServiceChoiceReadProperty,
				bacnet.PropertyError, bacnet.InvalidArrayIndex,
			)
			return
		}
		valBytes, err = elem.MarshalPrimitive()
		if err != nil {
			s.serviceHandler.applicationEntity.SendErrorResponse(
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
			s.serviceHandler.applicationEntity.SendErrorResponse(
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
	if err := s.serviceHandler.applicationEntity.SendConfServResponse(
		indication.InvokeId, indication.Source,
		bacnet.ConfirmedServiceChoiceReadProperty,
		ackBytes,
	); err != nil {
		logger.Error("could not send ReadProperty response: ", err)
	}
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
