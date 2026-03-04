package servicelayer

import (
	"fmt"
	"time"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/applicationlayer"
	"github.com/REQUEA/bacnet/internal/encoding"
	"github.com/REQUEA/bacnet/logger"
	"github.com/REQUEA/bacnet/objectmodel"
)

type IAmService struct {
	serviceHandler *ServiceHandler
}

// handleIAm processes a received IAm and updates the remote device cache.
func (s *IAmService) HandleUnconfServIndication(indication *applicationlayer.APDUIndication) {
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
	s.serviceHandler.remoteDeviceCache.Add(device)
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
