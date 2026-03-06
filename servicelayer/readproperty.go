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
		if sendErr := s.serviceHandler.applicationEntity.SendErrorResponse(
			indication.InvokeID, indication.Source,
			bacnet.ConfirmedServiceChoiceReadProperty,
			bacnet.ServicesError, bacnet.ServiceRequestDenied,
		); sendErr != nil {
			logger.Error("could not send error response: ", sendErr)
		}
		return
	}
	src, _ := s.serviceHandler.resolveObject(req.objectIdentifier.ObjType(), req.objectIdentifier.Instance())
	if src == nil {
		if sendErr := s.serviceHandler.applicationEntity.SendErrorResponse(
			indication.InvokeID, indication.Source,
			bacnet.ConfirmedServiceChoiceReadProperty,
			bacnet.ObjectError, bacnet.UnknownObject,
		); sendErr != nil {
			logger.Error("could not send error response: ", sendErr)
		}
		return
	}
	propID := bacnet.PropertyIdentifier(req.propertyIdentifier.Value())
	prop := src.GetProperty(propID)
	if prop == nil {
		if sendErr := s.serviceHandler.applicationEntity.SendErrorResponse(
			indication.InvokeID, indication.Source,
			bacnet.ConfirmedServiceChoiceReadProperty,
			bacnet.PropertyError, bacnet.UnknownProperty,
		); sendErr != nil {
			logger.Error("could not send error response: ", sendErr)
		}
		return
	}
	var valBytes []byte
	if req.propertyArrayIndex.Present() {
		idx := uint(req.propertyArrayIndex.Get().Value())
		arrayProp, ok := prop.(objectmodel.ArrayProperty)
		if !ok {
			if sendErr := s.serviceHandler.applicationEntity.SendErrorResponse(
				indication.InvokeID, indication.Source,
				bacnet.ConfirmedServiceChoiceReadProperty,
				bacnet.PropertyError, bacnet.PropertyIsNotAnArray,
			); sendErr != nil {
				logger.Error("could not send error response: ", sendErr)
			}
			return
		}
		elem, err := arrayProp.GetAt(idx)
		if err != nil {
			if sendErr := s.serviceHandler.applicationEntity.SendErrorResponse(
				indication.InvokeID, indication.Source,
				bacnet.ConfirmedServiceChoiceReadProperty,
				bacnet.PropertyError, bacnet.InvalidArrayIndex,
			); sendErr != nil {
				logger.Error("could not send error response: ", sendErr)
			}
			return
		}
		valBytes, err = elem.MarshalPrimitive()
		if err != nil {
			if sendErr := s.serviceHandler.applicationEntity.SendErrorResponse(
				indication.InvokeID, indication.Source,
				bacnet.ConfirmedServiceChoiceReadProperty,
				bacnet.PropertyError, bacnet.DatatypeNotSupported,
			); sendErr != nil {
				logger.Error("could not send error response: ", sendErr)
			}
			return
		}
	} else {
		var err error
		valBytes, err = prop.MarshalValue()
		if err != nil {
			logger.Error("could not marshal property value: ", err)
			if sendErr := s.serviceHandler.applicationEntity.SendErrorResponse(
				indication.InvokeID, indication.Source,
				bacnet.ConfirmedServiceChoiceReadProperty,
				bacnet.PropertyError, bacnet.DatatypeNotSupported,
			); sendErr != nil {
				logger.Error("could not send error response: ", sendErr)
			}
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
		indication.InvokeID, indication.Source,
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
			return remaining, fmt.Errorf("failed to read tag: %w", err)
		}
		switch tag {
		case 0:
			remaining, err = r.objectIdentifier.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading object-identifier: %w", err)
			}
		case 1:
			remaining, err = r.propertyIdentifier.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading property-identifier: %w", err)
			}
		case 2:
			var arrayIndex encoding.Unsigned
			remaining, err = arrayIndex.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading property-array-index: %w", err)
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
			return remaining, fmt.Errorf("failed to read tag: %w", err)
		}
		switch tag {
		case 0:
			remaining, err = r.objectIdentifier.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading object-identifier: %w", err)
			}
		case 1:
			remaining, err = r.propertyIdentifier.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading property-identifier: %w", err)
			}
		case 2:
			var arrayIndex encoding.Unsigned
			remaining, err = arrayIndex.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading property-array-index: %w", err)
			}
			r.propertyArrayIndex.Set(&arrayIndex)
		case 3:
			remaining, err = r.propertyValue.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading property-value: %w", err)
			}
		default:
			return remaining, fmt.Errorf("unexpected tag %v", tag)
		}
	}
	return remaining, nil
}

func (r *ReadPropertyAck) Marshal() ([]byte, error) {
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
	return result, nil
}
