package servicelayer

import (
	"context"
	"fmt"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/applicationlayer"
	"github.com/REQUEA/bacnet/internal/encoding"
	"github.com/REQUEA/bacnet/networklayer"
)

// ReadPropertyRaw sends a ReadProperty request to dest and waits for the response.
// On success, returns the raw application-tagged property value bytes from the ACK.
// Pass nil for arrayIndex to read the whole property.
func (sh *ServiceHandler) ReadPropertyRaw(
	ctx context.Context,
	dest *bacnet.BACnetAddress,
	objType uint16,
	instance uint32,
	propId bacnet.PropertyIdentifier,
	arrayIndex *uint,
) ([]byte, error) {
	var req ReadPropertyRequest
	req.objectIdentifier.SetFromValues(objType, instance)
	req.propertyIdentifier.SetValue(uint32(propId))
	if arrayIndex != nil {
		var idx encoding.Unsigned
		idx.SetValue(uint64(*arrayIndex))
		req.propertyArrayIndex.Set(&idx)
	}
	data, err := req.Marshal()
	if err != nil {
		return nil, fmt.Errorf("ReadProperty: could not marshal request: %w", err)
	}
	future := sh.applicationEntity.SendConfServRequest(
		bacnet.ConfirmedServiceChoiceReadProperty,
		dest, true, networklayer.NormalPriority, data,
	)
	resp := future.WaitResponse(ctx)
	if resp == nil {
		return nil, fmt.Errorf("ReadProperty: timeout or cancelled")
	}
	if resp.Type != applicationlayer.ResponseConfirm {
		return nil, fmt.Errorf("ReadProperty: request failed (response type %d)", resp.Type)
	}
	var ack ReadPropertyAck
	if _, err := ack.Unmarshal(resp.Indication.Data); err != nil {
		return nil, fmt.Errorf("ReadProperty: could not unmarshal ACK: %w", err)
	}
	return ack.propertyValue.Value(), nil
}

// ReadProperty sends a ReadProperty request and returns the decoded Go value.
// The concrete type depends on the BACnet application tag in the response
// (float32 for Real, bool for Boolean, uint64 for Unsigned, string for CharacterString, etc.).
func (sh *ServiceHandler) ReadProperty(
	ctx context.Context,
	dest *bacnet.BACnetAddress,
	objType uint16,
	instance uint32,
	propId bacnet.PropertyIdentifier,
	arrayIndex *uint,
) (interface{}, error) {
	raw, err := sh.ReadPropertyRaw(ctx, dest, objType, instance, propId, arrayIndex)
	if err != nil {
		return nil, err
	}
	return encoding.DecodeApplicationValue(raw)
}

// WriteProperty sends a WriteProperty request to dest and waits for the response.
// valueBytes are the pre-encoded application-tagged property value bytes.
// Pass nil for arrayIndex to write the whole property.
// Pass nil for priority to use default priority.
func (sh *ServiceHandler) WriteProperty(
	ctx context.Context,
	dest *bacnet.BACnetAddress,
	objType uint16,
	instance uint32,
	propId bacnet.PropertyIdentifier,
	arrayIndex *uint,
	valueBytes []byte,
	priority *uint8,
) error {
	var req WritePropertyRequest
	req.objectIdentifier.SetFromValues(objType, instance)
	req.propertyIdentifier.SetValue(uint32(propId))
	if arrayIndex != nil {
		var idx encoding.Unsigned
		idx.SetValue(uint64(*arrayIndex))
		req.propertyArrayIndex.Set(&idx)
	}
	req.propertyValue = encoding.NewAbstract(valueBytes)
	if priority != nil {
		var prio encoding.Unsigned
		prio.SetValue(uint64(*priority))
		req.priority.Set(&prio)
	}
	data, err := req.Marshal()
	if err != nil {
		return fmt.Errorf("WriteProperty: could not marshal request: %w", err)
	}
	future := sh.applicationEntity.SendConfServRequest(
		bacnet.ConfirmedServiceChoiceWriteProperty,
		dest, true, networklayer.NormalPriority, data,
	)
	resp := future.WaitResponse(ctx)
	if resp == nil {
		return fmt.Errorf("WriteProperty: timeout or cancelled")
	}
	if resp.Type != applicationlayer.ResponseConfirm {
		return fmt.Errorf("WriteProperty: request failed (response type %d)", resp.Type)
	}
	return nil
}

// ReadPropertyMultiple sends a ReadPropertyMultiple request to dest and waits for the response.
func (sh *ServiceHandler) ReadPropertyMultiple(
	ctx context.Context,
	dest *bacnet.BACnetAddress,
	specs []ReadAccessSpec,
) ([]ReadAccessResult, error) {
	req := ReadPropertyMultipleRequest{AccessSpecs: specs}
	data, err := req.Marshal()
	if err != nil {
		return nil, fmt.Errorf("ReadPropertyMultiple: could not marshal request: %w", err)
	}
	future := sh.applicationEntity.SendConfServRequest(
		bacnet.ConfirmedServiceChoiceReadPropertyMultiple,
		dest, true, networklayer.NormalPriority, data,
	)
	resp := future.WaitResponse(ctx)
	if resp == nil {
		return nil, fmt.Errorf("ReadPropertyMultiple: timeout or cancelled")
	}
	if resp.Type != applicationlayer.ResponseConfirm {
		return nil, fmt.Errorf("ReadPropertyMultiple: request failed (response type %d)", resp.Type)
	}
	results, err := UnmarshalRPMAck(resp.Indication.Data)
	if err != nil {
		return nil, fmt.Errorf("ReadPropertyMultiple: could not unmarshal ACK: %w", err)
	}
	return results, nil
}
