package servicelayer

import (
	"fmt"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/applicationlayer"
	"github.com/REQUEA/bacnet/internal/encoding"
	"github.com/REQUEA/bacnet/logger"
)

type WhoIsService struct {
	serviceHandler *ServiceHandler
}

// handleWhoIs responds to a received WhoIs with an IAm for each matching local device.
func (s *WhoIsService) HandleUnconfServIndication(indication *applicationlayer.APDUIndication) {
	var req WhoIsRequest
	_, err := req.Unmarshal(indication.Data)
	if err != nil {
		logger.Error("could not unmarshal WhoIs request: ", err)
		return
	}
	for _, device := range s.serviceHandler.devices {
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
			low := uint32(req.deviceInstanceRangeLow.Get().Value())   //nolint:gosec
			high := uint32(req.deviceInstanceRangeHigh.Get().Value()) //nolint:gosec
			if instance < low || instance > high {
				continue
			}
		}
		if err := s.serviceHandler.sendIAmForDevice(device); err != nil {
			logger.Error("could not send IAm in response to WhoIs: ", err)
		}
	}
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
			return remaining, fmt.Errorf("failed to read tag: %w", err)
		}
		switch tag {
		case 0:
			var low encoding.Unsigned
			remaining, err = low.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading low: %w", err)
			}
			r.deviceInstanceRangeLow.Set(&low)
			eltCount++
		case 1:
			var high encoding.Unsigned
			remaining, err = high.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("error reading high: %w", err)
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
