package servicelayer

import (
	"context"
	"fmt"
	"math"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/applicationlayer"
	"github.com/REQUEA/bacnet/internal/encoding"
	"github.com/REQUEA/bacnet/logger"
	"github.com/REQUEA/bacnet/networklayer"
	"github.com/REQUEA/bacnet/objectmodel"
)

// ReadRangeAck is the ReadRange-ACK payload.
type ReadRangeAck struct {
	ObjectIdentifier    encoding.BACnetObjectIdentifier
	PropertyIdentifier  encoding.BACnetPropertyIdentifier
	PropertyArrayIndex  encoding.Optional[*encoding.Unsigned]
	FirstItem           bool
	LastItem            bool
	MoreItems           bool
	ItemCount           uint32
	ItemData            []byte // raw concatenated application-tagged item bytes
	FirstSequenceNumber encoding.Optional[*encoding.Unsigned]
}

func (a *ReadRangeAck) Marshal() ([]byte, error) {
	var result []byte

	// [0] objectIdentifier
	b, err := a.ObjectIdentifier.MarshalTagged(0)
	if err != nil {
		return nil, fmt.Errorf("could not marshal objectIdentifier: %w", err)
	}
	result = append(result, b...)

	// [1] propertyIdentifier
	b, err = a.PropertyIdentifier.MarshalTagged(1)
	if err != nil {
		return nil, fmt.Errorf("could not marshal propertyIdentifier: %w", err)
	}
	result = append(result, b...)

	// [2] optional propertyArrayIndex
	if a.PropertyArrayIndex.Present() {
		b, err = a.PropertyArrayIndex.Get().MarshalTagged(2)
		if err != nil {
			return nil, fmt.Errorf("could not marshal propertyArrayIndex: %w", err)
		}
		result = append(result, b...)
	}

	// [3] resultFlags: context-tagged BIT STRING (3 bits, 5 unused)
	flagByte := byte(0)
	if a.FirstItem {
		flagByte |= 0x80
	}
	if a.LastItem {
		flagByte |= 0x40
	}
	if a.MoreItems {
		flagByte |= 0x20
	}
	result = append(result, (3<<4)|0x08|2, 5, flagByte)

	// [4] itemCount
	var itemCount encoding.Unsigned
	itemCount.SetValue(uint64(a.ItemCount))
	b, err = itemCount.MarshalTagged(4)
	if err != nil {
		return nil, fmt.Errorf("could not marshal itemCount: %w", err)
	}
	result = append(result, b...)

	// [5] itemData: opening tag, raw bytes, closing tag
	result = append(result, openingTag(5))
	result = append(result, a.ItemData...)
	result = append(result, closingTag(5))

	// [6] optional firstSequenceNumber
	if a.FirstSequenceNumber.Present() {
		b, err = a.FirstSequenceNumber.Get().MarshalTagged(6)
		if err != nil {
			return nil, fmt.Errorf("could not marshal firstSequenceNumber: %w", err)
		}
		result = append(result, b...)
	}

	return result, nil
}

// UnmarshalReadRangeAck parses a ReadRange-ACK payload.
func UnmarshalReadRangeAck(buf []byte) (*ReadRangeAck, error) {
	var ack ReadRangeAck
	remaining := buf
	for len(remaining) > 0 {
		tag, err := encoding.ReadTag(remaining)
		if err != nil {
			return nil, fmt.Errorf("error reading ReadRange ACK tag: %w", err)
		}
		switch tag {
		case 0:
			remaining, err = ack.ObjectIdentifier.Unmarshal(remaining)
		case 1:
			remaining, err = ack.PropertyIdentifier.Unmarshal(remaining)
		case 2:
			var idx encoding.Unsigned
			remaining, err = idx.Unmarshal(remaining)
			if err == nil {
				ack.PropertyArrayIndex.Set(&idx)
			}
		case 3:
			// ResultFlags: context-tagged BitString [unusedBits, flagByte]
			if len(remaining) < 3 {
				return nil, fmt.Errorf("buffer too short for resultFlags")
			}
			unusedBits := remaining[1]
			flagByte := remaining[2]
			remaining = remaining[3:]
			if unusedBits <= 7 {
				ack.FirstItem = (flagByte & 0x80) != 0
				ack.LastItem = (flagByte & 0x40) != 0
				ack.MoreItems = (flagByte & 0x20) != 0
			}
		case 4:
			var count encoding.Unsigned
			remaining, err = count.Unmarshal(remaining)
			if err == nil {
				ack.ItemCount = uint32(count.Value()) //nolint:gosec
			}
		case 5:
			// itemData: opening/closing context tags, raw bytes inside
			var itemAbstract encoding.Abstract
			remaining, err = itemAbstract.Unmarshal(remaining)
			if err == nil {
				ack.ItemData = itemAbstract.Value()
			}
		case 6:
			var fsn encoding.Unsigned
			remaining, err = fsn.Unmarshal(remaining)
			if err == nil {
				ack.FirstSequenceNumber.Set(&fsn)
			}
		default:
			return nil, fmt.Errorf("unexpected tag %d in ReadRange ACK", tag)
		}
		if err != nil {
			return nil, fmt.Errorf("error parsing ReadRange ACK field [%d]: %w", tag, err)
		}
	}
	return &ack, nil
}

// ReadRangeService is the server-side handler for ReadRange.
type ReadRangeService struct {
	serviceHandler *ServiceHandler
}

func (s *ReadRangeService) GetDevice(request []byte) *objectmodel.Device {
	return s.serviceHandler.findDeviceForRequest(request)
}

func (s *ReadRangeService) HandleConfServIndication(indication *applicationlayer.APDUIndication) {
	var req ReadRangeRequest
	_, err := req.Unmarshal(indication.Data)
	if err != nil {
		logger.Error("could not unmarshal ReadRange request: ", err)
		if sendErr := s.serviceHandler.applicationEntity.SendErrorResponse(
			indication.InvokeID, indication.Source,
			bacnet.ConfirmedServiceChoiceReadRange,
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
			bacnet.ConfirmedServiceChoiceReadRange,
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
			bacnet.ConfirmedServiceChoiceReadRange,
			bacnet.PropertyError, bacnet.UnknownProperty,
		); sendErr != nil {
			logger.Error("could not send error response: ", sendErr)
		}
		return
	}

	rangeProp, ok := prop.(objectmodel.RangeProperty)
	if !ok {
		if sendErr := s.serviceHandler.applicationEntity.SendErrorResponse(
			indication.InvokeID, indication.Source,
			bacnet.ConfirmedServiceChoiceReadRange,
			bacnet.PropertyError, bacnet.ReadAccessDenied,
		); sendErr != nil {
			logger.Error("could not send error response: ", sendErr)
		}
		return
	}

	var result *objectmodel.RangeReadResult
	if req.rangeByPosition.Present() {
		p := req.rangeByPosition.Get()
		result, err = rangeProp.ReadByPosition(uint32(p.referenceIndex.Value()), int32(p.count.Value())) //nolint:gosec
	} else if req.rangeBySequenceNumber.Present() {
		p := req.rangeBySequenceNumber.Get()
		result, err = rangeProp.ReadBySequenceNumber(uint32(p.referenceSequenceNumber.Value()), int32(p.count.Value())) //nolint:gosec
	} else if req.rangeByTime.Present() {
		p := req.rangeByTime.Get()
		result, err = rangeProp.ReadByTime(p.referenceTime, int32(p.count.Value()))
	} else {
		result, err = rangeProp.ReadByPosition(1, math.MaxInt32)
	}

	if err != nil {
		logger.Error("ReadRange operation failed: ", err)
		if sendErr := s.serviceHandler.applicationEntity.SendErrorResponse(
			indication.InvokeID, indication.Source,
			bacnet.ConfirmedServiceChoiceReadRange,
			bacnet.PropertyError, bacnet.ReadAccessDenied,
		); sendErr != nil {
			logger.Error("could not send error response: ", sendErr)
		}
		return
	}

	ackBytes, marshalErr := buildReadRangeAck(req, result)
	if marshalErr != nil {
		logger.Error("could not marshal ReadRange ACK: ", marshalErr)
		if sendErr := s.serviceHandler.applicationEntity.SendErrorResponse(
			indication.InvokeID, indication.Source,
			bacnet.ConfirmedServiceChoiceReadRange,
			bacnet.ServicesError, bacnet.ServiceRequestDenied,
		); sendErr != nil {
			logger.Error("could not send error response: ", sendErr)
		}
		return
	}

	if err := s.serviceHandler.applicationEntity.SendConfServResponse(
		indication.InvokeID, indication.Source,
		bacnet.ConfirmedServiceChoiceReadRange,
		ackBytes,
	); err != nil {
		logger.Error("could not send ReadRange response: ", err)
	}
}

func buildReadRangeAck(req ReadRangeRequest, result *objectmodel.RangeReadResult) ([]byte, error) {
	var ack ReadRangeAck
	ack.ObjectIdentifier = req.objectIdentifier
	ack.PropertyIdentifier = req.propertyIdentifier
	ack.PropertyArrayIndex = req.propertyArrayIndex
	ack.FirstItem = result.FirstItem
	ack.LastItem = result.LastItem
	ack.MoreItems = result.MoreItems
	ack.ItemCount = uint32(len(result.Items)) //nolint:gosec
	for _, item := range result.Items {
		ack.ItemData = append(ack.ItemData, item...)
	}
	if result.FirstSequenceNumber != nil {
		fsn := &encoding.Unsigned{}
		fsn.SetValue(uint64(*result.FirstSequenceNumber))
		ack.FirstSequenceNumber.Set(fsn)
	}
	return ack.Marshal()
}

// ReadRangeSpec describes a ReadRange request from the client.
type ReadRangeSpec struct {
	ObjType  uint16
	Instance uint32
	PropID   bacnet.PropertyIdentifier
	// At most one of the following should be set.
	ByPosition       *ReadRangeByPosition
	BySequenceNumber *ReadRangeBySequenceNumber
	ByTime           *ReadRangeByTime
}

type ReadRangeByPosition struct {
	ReferenceIndex uint32
	Count          int32
}

type ReadRangeBySequenceNumber struct {
	ReferenceSeqNum uint32
	Count           int32
}

type ReadRangeByTime struct {
	ReferenceTime encoding.BACnetDateTime
	Count         int32
}

// ReadRange sends a ReadRange request and waits for the response.
func (sh *ServiceHandler) ReadRange(
	ctx context.Context,
	dest *bacnet.BACnetAddress,
	spec ReadRangeSpec,
) (*ReadRangeAck, error) {
	var req ReadRangeRequest
	req.objectIdentifier.SetFromValues(spec.ObjType, spec.Instance)
	req.propertyIdentifier.SetValue(uint32(spec.PropID))

	if spec.ByPosition != nil {
		var p byPosition
		p.referenceIndex.SetValue(uint64(spec.ByPosition.ReferenceIndex))
		count := spec.ByPosition.Count
		if count > math.MaxInt16 {
			count = math.MaxInt16
		} else if count < math.MinInt16 {
			count = math.MinInt16
		}
		p.count.SetValue(int16(count)) //nolint:gosec
		req.rangeByPosition.Set(&p)
	} else if spec.BySequenceNumber != nil {
		var p bySequenceNumber
		p.referenceSequenceNumber.SetValue(uint64(spec.BySequenceNumber.ReferenceSeqNum))
		count := spec.BySequenceNumber.Count
		if count > math.MaxInt16 {
			count = math.MaxInt16
		} else if count < math.MinInt16 {
			count = math.MinInt16
		}
		p.count.SetValue(int16(count)) //nolint:gosec
		req.rangeBySequenceNumber.Set(&p)
	} else if spec.ByTime != nil {
		var p byTime
		p.referenceTime = spec.ByTime.ReferenceTime
		count := spec.ByTime.Count
		if count > math.MaxInt16 {
			count = math.MaxInt16
		} else if count < math.MinInt16 {
			count = math.MinInt16
		}
		p.count.SetValue(int16(count)) //nolint:gosec
		req.rangeByTime.Set(&p)
	}

	data, err := req.Marshal()
	if err != nil {
		return nil, fmt.Errorf("ReadRange: could not marshal request: %w", err)
	}

	future := sh.applicationEntity.SendConfServRequest(
		bacnet.ConfirmedServiceChoiceReadRange,
		dest, true, networklayer.NormalPriority, data,
	)
	resp := future.WaitResponse(ctx)
	if resp == nil {
		return nil, fmt.Errorf("ReadRange: timeout or cancelled")
	}
	if resp.Type != applicationlayer.ResponseConfirm {
		return nil, fmt.Errorf("ReadRange: request failed (response type %d)", resp.Type)
	}
	ack, err := UnmarshalReadRangeAck(resp.Indication.Data)
	if err != nil {
		return nil, fmt.Errorf("ReadRange: could not unmarshal ACK: %w", err)
	}
	return ack, nil
}
