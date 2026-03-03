package servicelayer

import (
	"fmt"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/applicationlayer"
	"github.com/REQUEA/bacnet/internal/encoding"
	"github.com/REQUEA/bacnet/logger"
)

// PropertyReference refers to a single property of an object (used in RPM requests).
type PropertyReference struct {
	PropertyIdentifier encoding.BACnetPropertyIdentifier
	PropertyArrayIndex encoding.Optional[*encoding.Unsigned]
}

// ReadAccessSpec is a single object with a list of property references (used in RPM requests).
type ReadAccessSpec struct {
	ObjectIdentifier  encoding.BACnetObjectIdentifier
	PropertyRefs      []PropertyReference
}

// ReadPropertyMultipleRequest is the RPM service request.
type ReadPropertyMultipleRequest struct {
	AccessSpecs []ReadAccessSpec
}

// openingTag returns the byte for a context-class opening tag with the given tag number.
func openingTag(n byte) byte { return (n << 4) | 0x0E }

// closingTag returns the byte for a context-class closing tag with the given tag number.
func closingTag(n byte) byte { return (n << 4) | 0x0F }

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
		device := sh.findDeviceByObjectId(spec.ObjectIdentifier.ObjType(), spec.ObjectIdentifier.Instance())

		// object-identifier [0]: context tag 0, ObjectID (4 bytes)
		objIDBytes, err := spec.ObjectIdentifier.MarshalTagged(0)
		if err != nil {
			return nil, fmt.Errorf("could not marshal object-identifier: %v", err)
		}
		result = append(result, objIDBytes...)

		// listOfResults [1]: opening tag
		result = append(result, openingTag(1))

		for _, ref := range spec.PropertyRefs {
			// propertyIdentifier [2]
			propIdBytes, err := ref.PropertyIdentifier.MarshalTagged(2)
			if err != nil {
				return nil, fmt.Errorf("could not marshal property-identifier: %v", err)
			}
			result = append(result, propIdBytes...)

			// optional propertyArrayIndex [3]
			if ref.PropertyArrayIndex.Present() {
				idxBytes, err := ref.PropertyArrayIndex.Get().MarshalTagged(3)
				if err != nil {
					return nil, fmt.Errorf("could not marshal property-array-index: %v", err)
				}
				result = append(result, idxBytes...)
			}

			if device == nil {
				// propertyAccessError [5]: unknown object
				result = append(result, marshalPropertyError(bacnet.ObjectError, bacnet.UnknownObject)...)
				continue
			}

			devObj := device.DeviceObject()
			propId := bacnet.PropertyIdentifier(ref.PropertyIdentifier.Value())
			prop := devObj.GetProperty(propId)
			if prop == nil {
				// propertyAccessError [5]: unknown property
				result = append(result, marshalPropertyError(bacnet.PropertyError, bacnet.UnknownProperty)...)
				continue
			}

			var valBytes []byte
			rawVal := prop.GetValue()
			if ref.PropertyArrayIndex.Present() {
				idx := uint(ref.PropertyArrayIndex.Get().Value())
				switch v := rawVal.(type) {
				case bacnet.BACnetArray[bacnet.BACnetObjectIdentifier]:
					elem, err := v.Get(idx)
					if err != nil {
						result = append(result, marshalPropertyError(bacnet.PropertyError, bacnet.InvalidArrayIndex)...)
						continue
					}
					if idx == 0 {
						valBytes = marshalUnsignedVal(2, uint64(elem.(uint)))
					} else {
						valBytes, err = marshalPropertyValue(elem)
						if err != nil {
							result = append(result, marshalPropertyError(bacnet.PropertyError, bacnet.DatatypeNotSupported)...)
							continue
						}
					}
				case bacnet.BACnetArray[bacnet.PropertyIdentifier]:
					elem, err := v.Get(idx)
					if err != nil {
						result = append(result, marshalPropertyError(bacnet.PropertyError, bacnet.InvalidArrayIndex)...)
						continue
					}
					if idx == 0 {
						valBytes = marshalUnsignedVal(2, uint64(elem.(uint)))
					} else {
						valBytes, err = marshalPropertyValue(elem)
						if err != nil {
							result = append(result, marshalPropertyError(bacnet.PropertyError, bacnet.DatatypeNotSupported)...)
							continue
						}
					}
				default:
					result = append(result, marshalPropertyError(bacnet.PropertyError, bacnet.PropertyIsNotAnArray)...)
					continue
				}
			} else {
				valBytes, err = marshalPropertyValue(rawVal)
				if err != nil {
					result = append(result, marshalPropertyError(bacnet.PropertyError, bacnet.DatatypeNotSupported)...)
					continue
				}
			}

			// propertyValue [4]: opening tag, app-tagged bytes, closing tag
			result = append(result, openingTag(4))
			result = append(result, valBytes...)
			result = append(result, closingTag(4))
		}

		// listOfResults [1]: closing tag
		result = append(result, closingTag(1))
	}
	return result, nil
}

// marshalPropertyError encodes a propertyAccessError [5] result element.
func marshalPropertyError(errClass bacnet.ErrorClass, errCode bacnet.ErrorCode) []byte {
	return []byte{
		openingTag(5),
		(9 << 4) | 1, byte(errClass), // error-class: enumerated
		(9 << 4) | 1, byte(errCode),  // error-code: enumerated
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

// handleReadPropertyMultiple processes a confirmed ReadPropertyMultiple request.
func (sh *ServiceHandler) handleReadPropertyMultiple(indication *applicationlayer.APDUIndication) {
	var req ReadPropertyMultipleRequest
	_, err := req.Unmarshal(indication.Data)
	if err != nil {
		logger.Error("could not unmarshal ReadPropertyMultiple request: ", err)
		sh.applicationEntity.SendErrorResponse(
			indication.InvokeId, indication.Source,
			bacnet.ConfirmedServiceChoiceReadPropertyMultiple,
			bacnet.ServicesError, bacnet.ServiceRequestDenied,
		)
		return
	}
	ackBytes, err := sh.marshalRPMAck(req.AccessSpecs)
	if err != nil {
		logger.Error("could not marshal ReadPropertyMultiple ACK: ", err)
		sh.applicationEntity.SendErrorResponse(
			indication.InvokeId, indication.Source,
			bacnet.ConfirmedServiceChoiceReadPropertyMultiple,
			bacnet.ServicesError, bacnet.ServiceRequestDenied,
		)
		return
	}
	if err := sh.applicationEntity.SendConfServResponse(
		indication.InvokeId, indication.Source,
		bacnet.ConfirmedServiceChoiceReadPropertyMultiple,
		ackBytes,
	); err != nil {
		logger.Error("could not send ReadPropertyMultiple response: ", err)
	}
}
