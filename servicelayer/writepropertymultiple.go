package servicelayer

import (
	"context"
	"fmt"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/applicationlayer"
	"github.com/REQUEA/bacnet/internal/encoding"
	"github.com/REQUEA/bacnet/logger"
	"github.com/REQUEA/bacnet/networklayer"
	"github.com/REQUEA/bacnet/objectmodel"
)

// WritePropertyValue holds a single property write operation.
type WritePropertyValue struct {
	PropertyIdentifier encoding.BACnetPropertyIdentifier
	PropertyArrayIndex encoding.Optional[*encoding.Unsigned]
	Value              encoding.Abstract
	Priority           encoding.Optional[*encoding.Unsigned]
}

// WriteAccessSpec is a single object with a list of property values (used in WPM requests).
type WriteAccessSpec struct {
	ObjectIdentifier encoding.BACnetObjectIdentifier
	ListOfProperties []WritePropertyValue
}

// WritePropertyMultipleRequest is the WPM service request.
type WritePropertyMultipleRequest struct {
	WriteAccessSpecs []WriteAccessSpec
}

func (r *WritePropertyMultipleRequest) Marshal() ([]byte, error) {
	var result []byte
	for _, spec := range r.WriteAccessSpecs {
		objIDBytes, err := spec.ObjectIdentifier.MarshalTagged(0)
		if err != nil {
			return nil, fmt.Errorf("could not marshal object-identifier: %v", err)
		}
		result = append(result, objIDBytes...)
		result = append(result, openingTag(1))
		for _, pv := range spec.ListOfProperties {
			propIdBytes, err := pv.PropertyIdentifier.MarshalTagged(0)
			if err != nil {
				return nil, fmt.Errorf("could not marshal property-identifier: %v", err)
			}
			result = append(result, propIdBytes...)
			if pv.PropertyArrayIndex.Present() {
				idxBytes, err := pv.PropertyArrayIndex.Get().MarshalTagged(1)
				if err != nil {
					return nil, fmt.Errorf("could not marshal property-array-index: %v", err)
				}
				result = append(result, idxBytes...)
			}
			valBytes, err := pv.Value.MarshalTagged(2)
			if err != nil {
				return nil, fmt.Errorf("could not marshal property-value: %v", err)
			}
			result = append(result, valBytes...)
			if pv.Priority.Present() {
				prioBytes, err := pv.Priority.Get().MarshalTagged(3)
				if err != nil {
					return nil, fmt.Errorf("could not marshal priority: %v", err)
				}
				result = append(result, prioBytes...)
			}
		}
		result = append(result, closingTag(1))
	}
	return result, nil
}

func (r *WritePropertyMultipleRequest) Unmarshal(buf []byte) ([]byte, error) {
	remaining := buf
	for len(remaining) > 0 {
		var spec WriteAccessSpec

		// objectIdentifier [0]
		tag, err := encoding.ReadTag(remaining)
		if err != nil {
			return remaining, fmt.Errorf("expected object-identifier tag: %v", err)
		}
		if tag != 0 {
			return remaining, fmt.Errorf("expected context tag 0 for object-identifier, got %d", tag)
		}
		remaining, err = spec.ObjectIdentifier.Unmarshal(remaining)
		if err != nil {
			return remaining, fmt.Errorf("could not unmarshal object-identifier: %v", err)
		}

		// listOfProperties [1]: opening tag
		if len(remaining) < 1 || remaining[0] != openingTag(1) {
			return remaining, fmt.Errorf("expected opening tag [1] for listOfProperties")
		}
		remaining = remaining[1:]

		// parse property values until closing tag [1]
		for len(remaining) > 0 && remaining[0] != closingTag(1) {
			var pv WritePropertyValue

			// propertyIdentifier [0]
			tag, err = encoding.ReadTag(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading property-identifier tag: %v", err)
			}
			if tag != 0 {
				return remaining, fmt.Errorf("expected context tag 0 for property-identifier, got %d", tag)
			}
			remaining, err = pv.PropertyIdentifier.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("could not unmarshal property-identifier: %v", err)
			}

			// optional arrayIndex [1]
			if len(remaining) > 0 {
				nextTag, err2 := encoding.ReadTag(remaining)
				if err2 == nil && nextTag == 1 {
					var arrayIdx encoding.Unsigned
					remaining, err = arrayIdx.Unmarshal(remaining)
					if err != nil {
						return remaining, fmt.Errorf("could not unmarshal property-array-index: %v", err)
					}
					pv.PropertyArrayIndex.Set(&arrayIdx)
				}
			}

			// propertyValue [2]: Abstract opening/closing tags
			nextTag, err2 := encoding.ReadTag(remaining)
			if err2 != nil || nextTag != 2 {
				return remaining, fmt.Errorf("expected context tag 2 for property-value")
			}
			remaining, err = pv.Value.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("could not unmarshal property-value: %v", err)
			}

			// optional priority [3]
			if len(remaining) > 0 && remaining[0] != closingTag(1) {
				nextTag, err2 = encoding.ReadTag(remaining)
				if err2 == nil && nextTag == 3 {
					var prio encoding.Unsigned
					remaining, err = prio.Unmarshal(remaining)
					if err != nil {
						return remaining, fmt.Errorf("could not unmarshal priority: %v", err)
					}
					pv.Priority.Set(&prio)
				}
			}

			spec.ListOfProperties = append(spec.ListOfProperties, pv)
		}

		// consume closing tag [1]
		if len(remaining) < 1 || remaining[0] != closingTag(1) {
			return remaining, fmt.Errorf("expected closing tag [1] for listOfProperties")
		}
		remaining = remaining[1:]

		r.WriteAccessSpecs = append(r.WriteAccessSpecs, spec)
	}
	return remaining, nil
}

// WritePropertyMultipleService is the server-side handler for WPM.
type WritePropertyMultipleService struct {
	serviceHandler *ServiceHandler
}

func (s *WritePropertyMultipleService) GetDevice(request []byte) *objectmodel.Device {
	return s.serviceHandler.findDeviceForRequest(request)
}

func (s *WritePropertyMultipleService) HandleConfServIndication(indication *applicationlayer.APDUIndication) {
	var req WritePropertyMultipleRequest
	_, err := req.Unmarshal(indication.Data)
	if err != nil {
		logger.Error("could not unmarshal WritePropertyMultiple request: ", err)
		s.serviceHandler.applicationEntity.SendErrorResponse(
			indication.InvokeId, indication.Source,
			bacnet.ConfirmedServiceChoiceWritePropertyMultiple,
			bacnet.ServicesError, bacnet.ServiceRequestDenied,
		)
		return
	}

	// Validate all writes first (all-or-nothing semantics).
	type pendingWrite struct {
		prop   objectmodel.Property
		data   []byte
		src    propertySource
		propId bacnet.PropertyIdentifier
	}

	var writes []pendingWrite
	for _, spec := range req.WriteAccessSpecs {
		src, _ := s.serviceHandler.resolveObject(spec.ObjectIdentifier.ObjType(), spec.ObjectIdentifier.Instance())
		if src == nil {
			s.serviceHandler.applicationEntity.SendErrorResponse(
				indication.InvokeId, indication.Source,
				bacnet.ConfirmedServiceChoiceWritePropertyMultiple,
				bacnet.ObjectError, bacnet.UnknownObject,
			)
			return
		}
		for _, pv := range spec.ListOfProperties {
			propId := bacnet.PropertyIdentifier(pv.PropertyIdentifier.Value())
			prop := src.GetProperty(propId)
			if prop == nil {
				s.serviceHandler.applicationEntity.SendErrorResponse(
					indication.InvokeId, indication.Source,
					bacnet.ConfirmedServiceChoiceWritePropertyMultiple,
					bacnet.PropertyError, bacnet.UnknownProperty,
				)
				return
			}
			if !prop.IsWritable() {
				s.serviceHandler.applicationEntity.SendErrorResponse(
					indication.InvokeId, indication.Source,
					bacnet.ConfirmedServiceChoiceWritePropertyMultiple,
					bacnet.PropertyError, bacnet.WriteAccessDenied,
				)
				return
			}
			writes = append(writes, pendingWrite{
				prop:   prop,
				data:   pv.Value.Value(),
				src:    src,
				propId: propId,
			})
		}
	}

	// Apply all writes.
	for _, w := range writes {
		if err := w.prop.UnmarshalValue(w.data); err != nil {
			s.serviceHandler.applicationEntity.SendErrorResponse(
				indication.InvokeId, indication.Source,
				bacnet.ConfirmedServiceChoiceWritePropertyMultiple,
				bacnet.PropertyError, bacnet.InvalidDataType,
			)
			return
		}
		if covSrc, ok := w.src.(covCapable); ok {
			s.serviceHandler.CheckAndNotifyCOV(covSrc, w.propId)
		}
	}

	if err := s.serviceHandler.applicationEntity.SendConfServResponse(
		indication.InvokeId, indication.Source,
		bacnet.ConfirmedServiceChoiceWritePropertyMultiple,
		nil,
	); err != nil {
		logger.Error("could not send WritePropertyMultiple response: ", err)
	}
}

// WritePropertyMultiple sends a WritePropertyMultiple request and waits for the response.
func (sh *ServiceHandler) WritePropertyMultiple(
	ctx context.Context,
	dest *bacnet.BACnetAddress,
	specs []WriteAccessSpec,
) error {
	req := WritePropertyMultipleRequest{WriteAccessSpecs: specs}
	data, err := req.Marshal()
	if err != nil {
		return fmt.Errorf("WritePropertyMultiple: could not marshal request: %w", err)
	}
	future := sh.applicationEntity.SendConfServRequest(
		bacnet.ConfirmedServiceChoiceWritePropertyMultiple,
		dest, true, networklayer.NormalPriority, data,
	)
	resp := future.WaitResponse(ctx)
	if resp == nil {
		return fmt.Errorf("WritePropertyMultiple: timeout or cancelled")
	}
	if resp.Type != applicationlayer.ResponseConfirm {
		return fmt.Errorf("WritePropertyMultiple: request failed (response type %d)", resp.Type)
	}
	return nil
}
