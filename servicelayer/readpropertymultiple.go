package servicelayer

import (
	"fmt"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/applicationlayer"
	"github.com/REQUEA/bacnet/internal/encoding"
	"github.com/REQUEA/bacnet/logger"
	"github.com/REQUEA/bacnet/objectmodel"
)

type ReadPropertyMultipleService struct {
	serviceHandler *ServiceHandler
}

func (s *ReadPropertyMultipleService) GetDevice(request []byte) *objectmodel.Device {
	return s.serviceHandler.findDeviceForRequest(request)
}

func (s *ReadPropertyMultipleService) HandleConfServIndication(indication *applicationlayer.APDUIndication) {
	var req ReadPropertyMultipleRequest
	_, err := req.Unmarshal(indication.Data)
	if err != nil {
		logger.Error("could not unmarshal ReadPropertyMultiple request: ", err)
		s.serviceHandler.applicationEntity.SendErrorResponse(
			indication.InvokeId, indication.Source,
			bacnet.ConfirmedServiceChoiceReadPropertyMultiple,
			bacnet.ServicesError, bacnet.ServiceRequestDenied,
		)
		return
	}
	ackBytes, err := s.serviceHandler.marshalRPMAck(req.AccessSpecs)
	if err != nil {
		logger.Error("could not marshal ReadPropertyMultiple ACK: ", err)
		s.serviceHandler.applicationEntity.SendErrorResponse(
			indication.InvokeId, indication.Source,
			bacnet.ConfirmedServiceChoiceReadPropertyMultiple,
			bacnet.ServicesError, bacnet.ServiceRequestDenied,
		)
		return
	}
	if err := s.serviceHandler.applicationEntity.SendConfServResponse(
		indication.InvokeId, indication.Source,
		bacnet.ConfirmedServiceChoiceReadPropertyMultiple,
		ackBytes,
	); err != nil {
		logger.Error("could not send ReadPropertyMultiple response: ", err)
	}
}

// PropertyReference refers to a single property of an object (used in RPM requests).
type PropertyReference struct {
	PropertyIdentifier encoding.BACnetPropertyIdentifier
	PropertyArrayIndex encoding.Optional[*encoding.Unsigned]
}

// ReadAccessSpec is a single object with a list of property references (used in RPM requests).
type ReadAccessSpec struct {
	ObjectIdentifier encoding.BACnetObjectIdentifier
	PropertyRefs     []PropertyReference
}

// ReadPropertyMultipleRequest is the RPM service request.
type ReadPropertyMultipleRequest struct {
	AccessSpecs []ReadAccessSpec
}

func (r *ReadPropertyMultipleRequest) Marshal() ([]byte, error) {
	var result []byte
	for _, spec := range r.AccessSpecs {
		objIDBytes, err := spec.ObjectIdentifier.MarshalTagged(0)
		if err != nil {
			return nil, fmt.Errorf("could not marshal object-identifier: %v", err)
		}
		result = append(result, objIDBytes...)
		result = append(result, openingTag(1))
		for _, ref := range spec.PropertyRefs {
			propIdBytes, err := ref.PropertyIdentifier.MarshalTagged(0)
			if err != nil {
				return nil, fmt.Errorf("could not marshal property-identifier: %v", err)
			}
			result = append(result, propIdBytes...)
			if ref.PropertyArrayIndex.Present() {
				idxBytes, err := ref.PropertyArrayIndex.Get().MarshalTagged(1)
				if err != nil {
					return nil, fmt.Errorf("could not marshal property-array-index: %v", err)
				}
				result = append(result, idxBytes...)
			}
		}
		result = append(result, closingTag(1))
	}
	return result, nil
}

func (r *ReadPropertyMultipleRequest) Unmarshal(buf []byte) ([]byte, error) {
	remaining := buf
	for len(remaining) > 0 {
		var spec ReadAccessSpec
		// objectIdentifier: context tag [0], ObjectID (4 bytes) → first byte 0x0C
		tag, err := encoding.ReadTag(remaining)
		if err != nil {
			return remaining, fmt.Errorf("expected objectIdentifier tag: %v", err)
		}
		if tag != 0 {
			return remaining, fmt.Errorf("expected context tag 0 for objectIdentifier, got %d", tag)
		}
		remaining, err = spec.ObjectIdentifier.Unmarshal(remaining)
		if err != nil {
			return remaining, fmt.Errorf("could not unmarshal objectIdentifier: %v", err)
		}

		// listOfPropertyReferences: context opening tag [1] = 0x1E
		if len(remaining) < 1 || remaining[0] != openingTag(1) {
			return remaining, fmt.Errorf("expected opening tag [1] for listOfPropertyReferences")
		}
		remaining = remaining[1:]

		// parse property references until closing tag [1]
		for len(remaining) > 0 && remaining[0] != closingTag(1) {
			var ref PropertyReference
			tag, err = encoding.ReadTag(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading property reference tag: %v", err)
			}
			if tag != 0 {
				return remaining, fmt.Errorf("expected context tag 0 for propertyIdentifier, got %d", tag)
			}
			remaining, err = ref.PropertyIdentifier.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("could not unmarshal propertyIdentifier: %v", err)
			}
			// optional array index: context tag [1]
			if len(remaining) > 0 && remaining[0] != closingTag(1) {
				nextTag, err2 := encoding.ReadTag(remaining)
				if err2 == nil && nextTag == 1 {
					var arrayIdx encoding.Unsigned
					remaining, err = arrayIdx.Unmarshal(remaining)
					if err != nil {
						return remaining, fmt.Errorf("could not unmarshal propertyArrayIndex: %v", err)
					}
					ref.PropertyArrayIndex.Set(&arrayIdx)
				}
			}
			spec.PropertyRefs = append(spec.PropertyRefs, ref)
		}

		// consume closing tag [1]
		if len(remaining) < 1 || remaining[0] != closingTag(1) {
			return remaining, fmt.Errorf("expected closing tag [1]")
		}
		remaining = remaining[1:]

		r.AccessSpecs = append(r.AccessSpecs, spec)
	}
	return remaining, nil
}

// marshalRPMAck marshals a ReadPropertyMultiple-ACK from a list of ReadAccessSpecs.
// For each spec, it reads each requested property from the local device and encodes the result.
func (sh *ServiceHandler) marshalRPMAck(specs []ReadAccessSpec) ([]byte, error) {
	var result []byte
	for _, spec := range specs {
		src, _ := sh.resolveObject(spec.ObjectIdentifier.ObjType(), spec.ObjectIdentifier.Instance())

		// object-identifier [0]: context tag 0, ObjectID (4 bytes)
		objIDBytes, err := spec.ObjectIdentifier.MarshalTagged(0)
		if err != nil {
			return nil, fmt.Errorf("could not marshal object-identifier: %v", err)
		}
		result = append(result, objIDBytes...)

		// listOfResults [1]: opening tag
		result = append(result, openingTag(1))

		for _, ref := range spec.PropertyRefs {
			propId := bacnet.PropertyIdentifier(ref.PropertyIdentifier.Value())

			// Expand All / Required / Optional selectors.
			if propId == bacnet.All || propId == bacnet.Required || propId == bacnet.Optional {
				if src == nil {
					result = append(result, marshalPropertyError(bacnet.ObjectError, bacnet.UnknownObject)...)
					continue
				}
				for _, id := range src.AllPropertyIdentifiers() {
					result = append(result, marshalOnePropertyResult(src, id, ref.PropertyArrayIndex)...)
				}
				continue
			}

			result = append(result, marshalOnePropertyResult(src, propId, ref.PropertyArrayIndex)...)
		}

		// listOfResults [1]: closing tag
		result = append(result, closingTag(1))
	}
	return result, nil
}

// marshalOnePropertyResult encodes a single property result entry (propertyIdentifier [2],
// optional propertyArrayIndex [3], then propertyValue [4] or propertyAccessError [5]).
func marshalOnePropertyResult(
	src propertySource,
	propId bacnet.PropertyIdentifier,
	arrayIndex encoding.Optional[*encoding.Unsigned],
) []byte {
	var result []byte

	// propertyIdentifier [2]
	var propIdEnc encoding.BACnetPropertyIdentifier
	propIdEnc.SetValue(uint32(propId))
	propIdBytes, err := propIdEnc.MarshalTagged(2)
	if err != nil {
		return marshalPropertyError(bacnet.PropertyError, bacnet.DatatypeNotSupported)
	}
	result = append(result, propIdBytes...)

	// optional propertyArrayIndex [3]
	if arrayIndex.Present() {
		idxBytes, err := arrayIndex.Get().MarshalTagged(3)
		if err != nil {
			return append(result, marshalPropertyError(bacnet.PropertyError, bacnet.DatatypeNotSupported)...)
		}
		result = append(result, idxBytes...)
	}

	if src == nil {
		return append(result, marshalPropertyError(bacnet.ObjectError, bacnet.UnknownObject)...)
	}

	prop := src.GetProperty(propId)
	if prop == nil {
		return append(result, marshalPropertyError(bacnet.PropertyError, bacnet.UnknownProperty)...)
	}

	var valBytes []byte
	if arrayIndex.Present() {
		idx := uint(arrayIndex.Get().Value())
		arrayProp, ok := prop.(objectmodel.ArrayProperty)
		if !ok {
			return append(result, marshalPropertyError(bacnet.PropertyError, bacnet.PropertyIsNotAnArray)...)
		}
		elem, err := arrayProp.GetAt(idx)
		if err != nil {
			return append(result, marshalPropertyError(bacnet.PropertyError, bacnet.InvalidArrayIndex)...)
		}
		valBytes, err = elem.MarshalPrimitive()
		if err != nil {
			return append(result, marshalPropertyError(bacnet.PropertyError, bacnet.DatatypeNotSupported)...)
		}
	} else {
		valBytes, err = prop.MarshalValue()
		if err != nil {
			return append(result, marshalPropertyError(bacnet.PropertyError, bacnet.DatatypeNotSupported)...)
		}
	}

	// propertyValue [4]: opening tag, app-tagged bytes, closing tag
	result = append(result, openingTag(4))
	result = append(result, valBytes...)
	result = append(result, closingTag(4))
	return result
}

// marshalPropertyError encodes a propertyAccessError [5] result element.
func marshalPropertyError(errClass bacnet.ErrorClass, errCode bacnet.ErrorCode) []byte {
	return []byte{
		openingTag(5),
		(9 << 4) | 1, byte(errClass), // error-class: enumerated
		(9 << 4) | 1, byte(errCode), // error-code: enumerated
		closingTag(5),
	}
}

// PropertyResult holds the result for one property reference in a ReadPropertyMultiple-ACK.
type PropertyResult struct {
	PropertyIdentifier encoding.BACnetPropertyIdentifier
	PropertyArrayIndex encoding.Optional[*encoding.Unsigned]
	Value              *encoding.Abstract // non-nil on success
	ErrorClass         *bacnet.ErrorClass // non-nil on error
	ErrorCode          *bacnet.ErrorCode  // non-nil on error
}

// ReadAccessResult holds all property results for one object in a ReadPropertyMultiple-ACK.
type ReadAccessResult struct {
	ObjectIdentifier encoding.BACnetObjectIdentifier
	PropertyResults  []PropertyResult
}

// UnmarshalRPMAck parses a ReadPropertyMultiple-ACK payload into a slice of ReadAccessResults.
func UnmarshalRPMAck(buf []byte) ([]ReadAccessResult, error) {
	var results []ReadAccessResult
	remaining := buf
	for len(remaining) > 0 {
		var res ReadAccessResult

		// object-identifier [0]
		tag, err := encoding.ReadTag(remaining)
		if err != nil {
			return nil, fmt.Errorf("expected object-identifier tag: %v", err)
		}
		if tag != 0 {
			return nil, fmt.Errorf("expected context tag 0 for object-identifier, got %d", tag)
		}
		remaining, err = res.ObjectIdentifier.Unmarshal(remaining)
		if err != nil {
			return nil, fmt.Errorf("could not unmarshal object-identifier: %v", err)
		}

		// listOfResults [1]: opening tag
		if len(remaining) < 1 || remaining[0] != openingTag(1) {
			return nil, fmt.Errorf("expected opening tag [1] for listOfResults")
		}
		remaining = remaining[1:]

		// parse property results until closing tag [1]
		for len(remaining) > 0 && remaining[0] != closingTag(1) {
			var pr PropertyResult

			// propertyIdentifier [2]
			tag, err = encoding.ReadTag(remaining)
			if err != nil {
				return nil, fmt.Errorf("error reading property-identifier tag: %v", err)
			}
			if tag != 2 {
				return nil, fmt.Errorf("expected context tag 2 for property-identifier, got %d", tag)
			}
			remaining, err = pr.PropertyIdentifier.Unmarshal(remaining)
			if err != nil {
				return nil, fmt.Errorf("could not unmarshal property-identifier: %v", err)
			}

			// optional propertyArrayIndex [3]
			if len(remaining) > 0 {
				nextTag, err2 := encoding.ReadTag(remaining)
				if err2 == nil && nextTag == 3 {
					var idx encoding.Unsigned
					remaining, err = idx.Unmarshal(remaining)
					if err != nil {
						return nil, fmt.Errorf("could not unmarshal property-array-index: %v", err)
					}
					pr.PropertyArrayIndex.Set(&idx)
				}
			}

			// next should be [4] propertyValue or [5] propertyAccessError
			if len(remaining) < 1 {
				return nil, fmt.Errorf("unexpected end of buffer in property result")
			}
			nextTag, err2 := encoding.ReadTag(remaining)
			if err2 != nil {
				return nil, fmt.Errorf("error reading result type tag: %v", err2)
			}
			switch nextTag {
			case 4: // propertyValue [4]: opening tag, app-tagged bytes, closing tag
				var val encoding.Abstract
				remaining, err = val.Unmarshal(remaining)
				if err != nil {
					return nil, fmt.Errorf("could not unmarshal property value: %v", err)
				}
				pr.Value = &val
			case 5: // propertyAccessError [5]
				if remaining[0] != openingTag(5) {
					return nil, fmt.Errorf("expected opening tag [5] for propertyAccessError")
				}
				remaining = remaining[1:]
				if len(remaining) < 2 {
					return nil, fmt.Errorf("buffer too short for error-class")
				}
				errClass := bacnet.ErrorClass(remaining[1])
				remaining = remaining[2:]
				if len(remaining) < 2 {
					return nil, fmt.Errorf("buffer too short for error-code")
				}
				errCode := bacnet.ErrorCode(remaining[1])
				remaining = remaining[2:]
				if len(remaining) < 1 || remaining[0] != closingTag(5) {
					return nil, fmt.Errorf("expected closing tag [5] for propertyAccessError")
				}
				remaining = remaining[1:]
				pr.ErrorClass = &errClass
				pr.ErrorCode = &errCode
			default:
				return nil, fmt.Errorf("unexpected tag %d in property result", nextTag)
			}
			res.PropertyResults = append(res.PropertyResults, pr)
		}

		// consume closing tag [1]
		if len(remaining) < 1 || remaining[0] != closingTag(1) {
			return nil, fmt.Errorf("expected closing tag [1] for listOfResults")
		}
		remaining = remaining[1:]
		results = append(results, res)
	}
	return results, nil
}
