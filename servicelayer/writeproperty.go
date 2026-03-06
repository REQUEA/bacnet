package servicelayer

import (
	"fmt"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/applicationlayer"
	"github.com/REQUEA/bacnet/internal/encoding"
	"github.com/REQUEA/bacnet/logger"
	"github.com/REQUEA/bacnet/objectmodel"
)

type WritePropertyService struct {
	serviceHandler *ServiceHandler
}

func (s *WritePropertyService) GetDevice(request []byte) *objectmodel.Device {
	return s.serviceHandler.findDeviceForRequest(request)
}

func (s *WritePropertyService) HandleConfServIndication(indication *applicationlayer.APDUIndication) {
	var req WritePropertyRequest
	sendErr := func(class bacnet.ErrorClass, code bacnet.ErrorCode) {
		if err := s.serviceHandler.applicationEntity.SendErrorResponse(
			indication.InvokeID, indication.Source,
			bacnet.ConfirmedServiceChoiceWriteProperty,
			class, code,
		); err != nil {
			logger.Error("could not send error response: ", err)
		}
	}

	_, err := req.Unmarshal(indication.Data)
	if err != nil {
		logger.Error("could not unmarshal WriteProperty request: ", err)
		sendErr(bacnet.ServicesError, bacnet.ServiceRequestDenied)
		return
	}
	src, _ := s.serviceHandler.resolveObject(req.objectIdentifier.ObjType(), req.objectIdentifier.Instance())
	if src == nil {
		sendErr(bacnet.ObjectError, bacnet.UnknownObject)
		return
	}
	propID := bacnet.PropertyIdentifier(req.propertyIdentifier.Value())
	prop := src.GetProperty(propID)
	if prop == nil {
		sendErr(bacnet.PropertyError, bacnet.UnknownProperty)
		return
	}
	if !prop.IsWritable() {
		sendErr(bacnet.PropertyError, bacnet.WriteAccessDenied)
		return
	}
	if err := prop.UnmarshalValue(req.propertyValue.Value()); err != nil {
		sendErr(bacnet.PropertyError, bacnet.InvalidDataType)
		return
	}
	// Trigger COV notifications if applicable.
	if covSrc, ok := src.(covCapable); ok {
		s.serviceHandler.CheckAndNotifyCOV(covSrc, propID)
	}
	// SimpleAck
	if err := s.serviceHandler.applicationEntity.SendConfServResponse(
		indication.InvokeID, indication.Source,
		bacnet.ConfirmedServiceChoiceWriteProperty,
		nil,
	); err != nil {
		logger.Error("could not send WriteProperty response: ", err)
	}
}

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
		case 4:
			var prio encoding.Unsigned
			remaining, err = prio.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading priority: %w", err)
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
