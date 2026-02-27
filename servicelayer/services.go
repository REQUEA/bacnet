package servicelayer

import (
	"fmt"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/applicationlayer"
	"github.com/REQUEA/bacnet/internal/encoding"
	"github.com/REQUEA/bacnet/objectmodel"
)

type ServiceHandler struct {
	devices             []*objectmodel.Device
	objectIndex         map[bacnet.BACnetObjectIdentifier]*objectmodel.Object
	confirmedServices   map[bacnet.BACnetConfirmedServiceChoice]ConfirmedService
	unconfirmedServices map[bacnet.BACnetUnconfirmedServiceChoice]UnconfirmedService
}

func (e *ServiceHandler) ConfServIndication(
	indication *applicationlayer.APDUIndication,
	serviceChoice bacnet.BACnetConfirmedServiceChoice,
	data []byte,
) {
	// TODO: implement
	go func() {
		// find service
	}()
}

func (e *ServiceHandler) AbortIndication(indication *applicationlayer.APDUIndication, reason uint8) {
	// TODO: implement
}

func (e *ServiceHandler) ConfServConfirm(
	indication *applicationlayer.APDUIndication,
	serviceChoice bacnet.BACnetConfirmedServiceChoice,
	data []byte,
) {
	// TODO implement
}

func (l *ServiceHandler) GetDevice(
	serviceChoice bacnet.BACnetConfirmedServiceChoice,
	request []byte,
) *objectmodel.Device {
	service, ok := l.confirmedServices[serviceChoice]
	if !ok {
		return nil
	}
	return service.GetDevice(request)
}

type ConfirmedService interface {
	GetDevice(serviceRequest []byte) *objectmodel.Device
}

type UnconfirmedService interface {
}

type WhoIsService struct {
	serviceLayer *ServiceHandler
}

type WhoIsRequest struct {
	deviceInstanceRangeLow  encoding.Optional[*encoding.Unsigned]
	deviceInstanceRangeHigh encoding.Optional[*encoding.Unsigned]
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
			remaining, err = low.Unmarshal(buf)
			if err != nil {
				return remaining, fmt.Errorf("error reading low: %v", err)
			}
			r.deviceInstanceRangeLow.Set(&low)
			eltCount++
		case 1:
			var high encoding.Unsigned
			remaining, err = high.Unmarshal(buf)
			if err != nil {
				return remaining, fmt.Errorf("error reading low: %v", err)
			}
			r.deviceInstanceRangeLow.Set(&high)
			eltCount++
		default:
			return remaining, fmt.Errorf("unexpected tag %v", tag)
		}
	}
	if eltCount != 0 && eltCount != 2 {
		return remaining, fmt.Errorf("invalid who-is request")
	}
	return remaining, nil
}

func (r *WhoIsRequest) Marshal() ([]byte, error) {
	if !r.deviceInstanceRangeHigh.Present() || !r.deviceInstanceRangeLow.Present() {
		return nil, fmt.Errorf("both deviceinstance-range-high and deviceinstance-range-low must be present")
	}
	result := make([]byte, 0)
	tmp, err := r.deviceInstanceRangeLow.Get().MarshalTagged(0)
	if err != nil {
		return nil, fmt.Errorf("could not marshal deviceinstance-range-low")
	}
	result = append(result, tmp...)
	tmp, err = r.deviceInstanceRangeHigh.Get().MarshalTagged(1)
	if err != nil {
		return nil, fmt.Errorf("could not marshal deviceinstance-range-low")
	}
	result = append(result, tmp...)
	return result, nil
}

type IAmRequest struct {
	iAmDeviceIdentifier   encoding.BACnetObjectIdentifier
	maxApduLengthAccepted encoding.Unsigned
	segmentationSupported encoding.BACnetSegmentation
	vendorId              encoding.Unsigned16
}

func (r *IAmRequest) Unmarshal(buf []byte) ([]byte, error) {
	remaining, err := r.iAmDeviceIdentifier.Unmarshal(buf)
	if err != nil {
		return remaining, fmt.Errorf("could not read device identifier: %v", err)
	}
	remaining, err = r.maxApduLengthAccepted.Unmarshal(remaining)
	if err != nil {
		return remaining, fmt.Errorf("could not read max ADPU length: %v", err)
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
	return result, nil
}

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
	eltCount := 0
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
			eltCount++
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
	eltCount := 0
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
			eltCount++
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
