package servicelayer

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/applicationlayer"
	"github.com/REQUEA/bacnet/internal/encoding"
	"github.com/REQUEA/bacnet/logger"
	"github.com/REQUEA/bacnet/networklayer"
	"github.com/REQUEA/bacnet/objectmodel"
)

// covCapable is satisfied by any object that can publish COV notifications.
type covCapable interface {
	COVProperties() []bacnet.PropertyIdentifier
	GetProperty(bacnet.PropertyIdentifier) objectmodel.Property
}

// --- Subscription storage ---

type covSubscription struct {
	subscriberProcessId uint32
	subscriber          *bacnet.BACnetAddress
	objectType          uint16
	objectInstance      uint32
	propertyId          *bacnet.PropertyIdentifier // nil = all COV properties (SubscribeCOV)
	propertyArrayIndex  *uint
	covIncrement        *float32    // non-nil: apply COV-increment threshold for Real properties
	confirmed           bool
	expiresAt           time.Time   // zero = permanent
	lastNotifiedValues  map[bacnet.PropertyIdentifier][]byte
}

func (sub *covSubscription) timeRemaining() uint32 {
	if sub.expiresAt.IsZero() {
		return 0
	}
	remaining := time.Until(sub.expiresAt)
	if remaining <= 0 {
		return 0
	}
	return uint32(remaining.Seconds())
}

func covSubMatchesKey(sub *covSubscription, addr *bacnet.BACnetAddress, processId uint32, objType uint16, objInstance uint32) bool {
	return sub.subscriberProcessId == processId &&
		sub.objectType == objType &&
		sub.objectInstance == objInstance &&
		sub.subscriber.Equal(addr)
}

func (sh *ServiceHandler) addOrUpdateSubscription(sub *covSubscription) {
	sh.covMu.Lock()
	defer sh.covMu.Unlock()
	for i, existing := range sh.covSubscriptions {
		if existing == nil {
			continue
		}
		if covSubMatchesKey(existing, sub.subscriber, sub.subscriberProcessId, sub.objectType, sub.objectInstance) {
			sh.covSubscriptions[i] = sub
			return
		}
	}
	sh.covSubscriptions = append(sh.covSubscriptions, sub)
	// Schedule expiry if limited lifetime
	if !sub.expiresAt.IsZero() {
		ttl := time.Until(sub.expiresAt)
		s := sub
		time.AfterFunc(ttl, func() {
			sh.expireSubscription(s)
		})
	}
}

func (sh *ServiceHandler) removeSubscription(addr *bacnet.BACnetAddress, processId uint32, objType uint16, objInstance uint32) {
	sh.covMu.Lock()
	defer sh.covMu.Unlock()
	for i, sub := range sh.covSubscriptions {
		if sub != nil && covSubMatchesKey(sub, addr, processId, objType, objInstance) {
			sh.covSubscriptions[i] = nil
			return
		}
	}
}

func (sh *ServiceHandler) expireSubscription(target *covSubscription) {
	sh.covMu.Lock()
	defer sh.covMu.Unlock()
	for i, sub := range sh.covSubscriptions {
		if sub == target {
			sh.covSubscriptions[i] = nil
			return
		}
	}
}

// --- Tag helpers ---

func isOpeningTag(buf []byte, tag uint) bool {
	if len(buf) < 1 {
		return false
	}
	b := buf[0]
	if b&0x0F != 0x0E {
		return false
	}
	hi := (b & 0xF0) >> 4
	if hi == 0x0F {
		return len(buf) >= 2 && buf[1] == byte(tag)
	}
	return uint(hi) == tag
}

func isClosingTag(buf []byte, tag uint) bool {
	if len(buf) < 1 {
		return false
	}
	b := buf[0]
	if b&0x0F != 0x0F {
		return false
	}
	hi := (b & 0xF0) >> 4
	if hi == 0x0F {
		return len(buf) >= 2 && buf[1] == byte(tag)
	}
	return uint(hi) == tag
}

// skipTag advances past a single tag byte (opening or closing), including extended tags.
func skipTag(buf []byte) []byte {
	if len(buf) < 1 {
		return buf
	}
	if (buf[0]>>4) == 0x0F {
		if len(buf) >= 2 {
			return buf[2:]
		}
		return buf
	}
	return buf[1:]
}

func openingTagBytes(tag uint) []byte {
	if tag < 15 {
		return []byte{byte(tag<<4) | 0x0E}
	}
	return []byte{0xFE, byte(tag)}
}

func closingTagBytes(tag uint) []byte {
	if tag < 15 {
		return []byte{byte(tag<<4) | 0x0F}
	}
	return []byte{0xFF, byte(tag)}
}

// --- SubscribeCOVRequest ---

type SubscribeCOVRequest struct {
	subscriberProcessIdentifier encoding.Unsigned
	monitoredObjectIdentifier   encoding.BACnetObjectIdentifier
	issueConfirmedNotifications encoding.Optional[*encoding.Boolean]
	lifetime                    encoding.Optional[*encoding.Unsigned]
}

// IsCancel returns true when tags [2] and [3] are absent, indicating a cancellation.
func (r *SubscribeCOVRequest) IsCancel() bool {
	return !r.issueConfirmedNotifications.Present()
}

func (r *SubscribeCOVRequest) Unmarshal(buf []byte) ([]byte, error) {
	remaining := buf
	for len(remaining) > 0 {
		tag, err := encoding.ReadTag(remaining)
		if err != nil {
			return remaining, err
		}
		switch tag {
		case 0:
			remaining, err = r.subscriberProcessIdentifier.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("SubscribeCOV: reading subscriber-process-id: %w", err)
			}
		case 1:
			remaining, err = r.monitoredObjectIdentifier.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("SubscribeCOV: reading monitored-object-id: %w", err)
			}
		case 2:
			if isOpeningTag(remaining, 2) || isClosingTag(remaining, 2) {
				return remaining, nil // belongs to an outer structure
			}
			var confirmed encoding.Boolean
			remaining, err = confirmed.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("SubscribeCOV: reading issue-confirmed: %w", err)
			}
			r.issueConfirmedNotifications.Set(&confirmed)
		case 3:
			var lt encoding.Unsigned
			remaining, err = lt.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("SubscribeCOV: reading lifetime: %w", err)
			}
			r.lifetime.Set(&lt)
		default:
			// Unknown tag — stop; let the caller handle the rest.
			return remaining, nil
		}
	}
	return remaining, nil
}

func (r *SubscribeCOVRequest) Marshal() ([]byte, error) {
	result := make([]byte, 0)
	tmp, err := r.subscriberProcessIdentifier.MarshalTagged(0)
	if err != nil {
		return nil, err
	}
	result = append(result, tmp...)
	tmp, err = r.monitoredObjectIdentifier.MarshalTagged(1)
	if err != nil {
		return nil, err
	}
	result = append(result, tmp...)
	if r.issueConfirmedNotifications.Present() {
		tmp, err = r.issueConfirmedNotifications.Get().MarshalTagged(2)
		if err != nil {
			return nil, err
		}
		result = append(result, tmp...)
	}
	if r.lifetime.Present() {
		tmp, err = r.lifetime.Get().MarshalTagged(3)
		if err != nil {
			return nil, err
		}
		result = append(result, tmp...)
	}
	return result, nil
}

// --- covPropertyReference (used inside SubscribeCOVPropertyRequest) ---

type covPropertyReference struct {
	propertyIdentifier encoding.BACnetPropertyIdentifier
	propertyArrayIndex encoding.Optional[*encoding.Unsigned]
}

// --- SubscribeCOVPropertyRequest ---

type SubscribeCOVPropertyRequest struct {
	SubscribeCOVRequest
	monitoredPropertyReference covPropertyReference
	covIncrement               encoding.Optional[*encoding.Real]
}

func (r *SubscribeCOVPropertyRequest) Unmarshal(buf []byte) ([]byte, error) {
	remaining, err := r.SubscribeCOVRequest.Unmarshal(buf)
	if err != nil {
		return remaining, err
	}
	// [4] monitored-property-reference (opening tag 4 ... closing tag 4)
	if len(remaining) > 0 && isOpeningTag(remaining, 4) {
		remaining = skipTag(remaining) // skip opening tag [4]
		// [0] property-identifier
		remaining, err = r.monitoredPropertyReference.propertyIdentifier.Unmarshal(remaining)
		if err != nil {
			return remaining, fmt.Errorf("SubscribeCOVProperty: reading property-id: %w", err)
		}
		// [1] property-array-index (optional)
		if len(remaining) > 0 {
			tag, _ := encoding.ReadTag(remaining)
			if tag == 1 && !isOpeningTag(remaining, 1) && !isClosingTag(remaining, 1) {
				var idx encoding.Unsigned
				remaining, err = idx.Unmarshal(remaining)
				if err != nil {
					return remaining, fmt.Errorf("SubscribeCOVProperty: reading array-index: %w", err)
				}
				idxVal := uint(idx.Value())
				r.monitoredPropertyReference.propertyArrayIndex.Set(&idx)
				_ = idxVal
			}
		}
		// closing tag [4]
		if len(remaining) > 0 && isClosingTag(remaining, 4) {
			remaining = skipTag(remaining)
		}
	}
	// [5] cov-increment (optional Real)
	if len(remaining) > 0 {
		tag, _ := encoding.ReadTag(remaining)
		if tag == 5 {
			var incr encoding.Real
			remaining, err = incr.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("SubscribeCOVProperty: reading cov-increment: %w", err)
			}
			r.covIncrement.Set(&incr)
		}
	}
	return remaining, nil
}

func (r *SubscribeCOVPropertyRequest) Marshal() ([]byte, error) {
	base, err := r.SubscribeCOVRequest.Marshal()
	if err != nil {
		return nil, err
	}
	result := append([]byte{}, base...)
	// [4] monitored-property-reference
	result = append(result, openingTagBytes(4)...)
	tmp, err := r.monitoredPropertyReference.propertyIdentifier.MarshalTagged(0)
	if err != nil {
		return nil, err
	}
	result = append(result, tmp...)
	if r.monitoredPropertyReference.propertyArrayIndex.Present() {
		tmp, err = r.monitoredPropertyReference.propertyArrayIndex.Get().MarshalTagged(1)
		if err != nil {
			return nil, err
		}
		result = append(result, tmp...)
	}
	result = append(result, closingTagBytes(4)...)
	// [5] cov-increment
	if r.covIncrement.Present() {
		tmp, err = r.covIncrement.Get().MarshalTagged(5)
		if err != nil {
			return nil, err
		}
		result = append(result, tmp...)
	}
	return result, nil
}

// --- COVPropertyValue ---

type COVPropertyValue struct {
	propertyIdentifier encoding.BACnetPropertyIdentifier
	propertyArrayIndex encoding.Optional[*encoding.Unsigned]
	value              encoding.Abstract
	priority           encoding.Optional[*encoding.Unsigned]
}

// unmarshal parses a single COVPropertyValue from buf and returns the remaining bytes.
// Fields are parsed sequentially (BACnet SEQUENCE ordering): [0] required, [1] optional,
// [2] required, [3] optional.
func (v *COVPropertyValue) unmarshal(buf []byte) ([]byte, error) {
	remaining := buf
	var err error

	// [0] property-identifier (required)
	remaining, err = v.propertyIdentifier.Unmarshal(remaining)
	if err != nil {
		return buf, fmt.Errorf("COVPropertyValue: reading property-id: %w", err)
	}

	// [1] property-array-index (optional)
	if len(remaining) > 0 {
		tag, _ := encoding.ReadTag(remaining)
		if tag == 1 && !isOpeningTag(remaining, 1) && !isClosingTag(remaining, 1) {
			var idx encoding.Unsigned
			remaining, err = idx.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("COVPropertyValue: reading array-index: %w", err)
			}
			v.propertyArrayIndex.Set(&idx)
		}
	}

	// [2] value (required, Abstract with opening/closing tag 2)
	if len(remaining) == 0 {
		return remaining, fmt.Errorf("COVPropertyValue: missing value field")
	}
	remaining, err = v.value.Unmarshal(remaining)
	if err != nil {
		return remaining, fmt.Errorf("COVPropertyValue: reading value: %w", err)
	}

	// [3] time-increment / priority (optional)
	if len(remaining) > 0 {
		tag, _ := encoding.ReadTag(remaining)
		if tag == 3 && !isOpeningTag(remaining, 3) && !isClosingTag(remaining, 3) {
			var prio encoding.Unsigned
			remaining, err = prio.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("COVPropertyValue: reading priority: %w", err)
			}
			v.priority.Set(&prio)
		}
	}

	return remaining, nil
}

func (v *COVPropertyValue) marshal() ([]byte, error) {
	result := make([]byte, 0)
	tmp, err := v.propertyIdentifier.MarshalTagged(0)
	if err != nil {
		return nil, err
	}
	result = append(result, tmp...)
	if v.propertyArrayIndex.Present() {
		tmp, err = v.propertyArrayIndex.Get().MarshalTagged(1)
		if err != nil {
			return nil, err
		}
		result = append(result, tmp...)
	}
	tmp, err = v.value.MarshalTagged(2)
	if err != nil {
		return nil, err
	}
	result = append(result, tmp...)
	if v.priority.Present() {
		tmp, err = v.priority.Get().MarshalTagged(3)
		if err != nil {
			return nil, err
		}
		result = append(result, tmp...)
	}
	return result, nil
}

// --- COVNotificationRequest (same body for confirmed and unconfirmed) ---

type COVNotificationRequest struct {
	subscriberProcessIdentifier encoding.Unsigned
	initiatingDeviceIdentifier  encoding.BACnetObjectIdentifier
	monitoredObjectIdentifier   encoding.BACnetObjectIdentifier
	timeRemaining               encoding.Unsigned
	listOfValues                []COVPropertyValue
}

func (r *COVNotificationRequest) Unmarshal(buf []byte) ([]byte, error) {
	remaining := buf
	for len(remaining) > 0 {
		if isOpeningTag(remaining, 4) {
			remaining = skipTag(remaining) // skip opening tag [4]
			for len(remaining) > 0 && !isClosingTag(remaining, 4) {
				var v COVPropertyValue
				var err error
				remaining, err = v.unmarshal(remaining)
				if err != nil {
					return remaining, fmt.Errorf("COVNotification: reading list-of-values: %w", err)
				}
				r.listOfValues = append(r.listOfValues, v)
			}
			if len(remaining) > 0 && isClosingTag(remaining, 4) {
				remaining = skipTag(remaining) // skip closing tag [4]
			}
			break
		}
		tag, err := encoding.ReadTag(remaining)
		if err != nil {
			return remaining, err
		}
		switch tag {
		case 0:
			remaining, err = r.subscriberProcessIdentifier.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("COVNotification: reading subscriber-process-id: %w", err)
			}
		case 1:
			remaining, err = r.initiatingDeviceIdentifier.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("COVNotification: reading initiating-device-id: %w", err)
			}
		case 2:
			remaining, err = r.monitoredObjectIdentifier.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("COVNotification: reading monitored-object-id: %w", err)
			}
		case 3:
			remaining, err = r.timeRemaining.Unmarshal(remaining)
			if err != nil {
				return remaining, fmt.Errorf("COVNotification: reading time-remaining: %w", err)
			}
		default:
			return remaining, fmt.Errorf("COVNotification: unexpected tag %d", tag)
		}
	}
	return remaining, nil
}

func (r *COVNotificationRequest) Marshal() ([]byte, error) {
	result := make([]byte, 0)
	tmp, err := r.subscriberProcessIdentifier.MarshalTagged(0)
	if err != nil {
		return nil, err
	}
	result = append(result, tmp...)
	tmp, err = r.initiatingDeviceIdentifier.MarshalTagged(1)
	if err != nil {
		return nil, err
	}
	result = append(result, tmp...)
	tmp, err = r.monitoredObjectIdentifier.MarshalTagged(2)
	if err != nil {
		return nil, err
	}
	result = append(result, tmp...)
	tmp, err = r.timeRemaining.MarshalTagged(3)
	if err != nil {
		return nil, err
	}
	result = append(result, tmp...)
	// [4] list-of-values
	result = append(result, openingTagBytes(4)...)
	for i := range r.listOfValues {
		tmp, err = r.listOfValues[i].marshal()
		if err != nil {
			return nil, err
		}
		result = append(result, tmp...)
	}
	result = append(result, closingTagBytes(4)...)
	return result, nil
}

// --- Server handlers ---

func (sh *ServiceHandler) handleSubscribeCOV(indication *applicationlayer.APDUIndication) {
	var req SubscribeCOVRequest
	if _, err := req.Unmarshal(indication.Data); err != nil {
		logger.Error("could not unmarshal SubscribeCOV: ", err)
		sh.applicationEntity.SendErrorResponse(
			indication.InvokeId, indication.Source,
			bacnet.ConfirmedServiceChoiceSubscribeCov,
			bacnet.ServicesError, bacnet.ServiceRequestDenied,
		)
		return
	}
	objType := req.monitoredObjectIdentifier.ObjType()
	objInstance := req.monitoredObjectIdentifier.Instance()

	src, _ := sh.resolveObject(objType, objInstance)
	if src == nil {
		sh.applicationEntity.SendErrorResponse(
			indication.InvokeId, indication.Source,
			bacnet.ConfirmedServiceChoiceSubscribeCov,
			bacnet.ObjectError, bacnet.UnknownObject,
		)
		return
	}
	covSrc, ok := src.(covCapable)
	if !ok || len(covSrc.COVProperties()) == 0 {
		sh.applicationEntity.SendErrorResponse(
			indication.InvokeId, indication.Source,
			bacnet.ConfirmedServiceChoiceSubscribeCov,
			bacnet.ServicesError, bacnet.OptionalFunctionalityNotSupported,
		)
		return
	}

	processId := uint32(req.subscriberProcessIdentifier.Value())

	if req.IsCancel() {
		sh.removeSubscription(indication.Source, processId, objType, objInstance)
		_ = sh.applicationEntity.SendConfServResponse(
			indication.InvokeId, indication.Source,
			bacnet.ConfirmedServiceChoiceSubscribeCov, nil,
		)
		return
	}

	confirmed := req.issueConfirmedNotifications.Get().Value()
	lifetime := uint32(req.lifetime.Get().Value())

	sub := &covSubscription{
		subscriberProcessId: processId,
		subscriber:          indication.Source,
		objectType:          objType,
		objectInstance:      objInstance,
		confirmed:           confirmed,
		lastNotifiedValues:  make(map[bacnet.PropertyIdentifier][]byte),
	}
	if lifetime > 0 {
		sub.expiresAt = time.Now().Add(time.Duration(lifetime) * time.Second)
	}
	sh.addOrUpdateSubscription(sub)

	// Send initial notification with current values.
	if data, err := sh.buildCOVNotification(sub, covSrc); err == nil {
		sh.sendCOVNotificationToSubscriber(sub, data)
	}

	_ = sh.applicationEntity.SendConfServResponse(
		indication.InvokeId, indication.Source,
		bacnet.ConfirmedServiceChoiceSubscribeCov, nil,
	)
}

func (sh *ServiceHandler) handleSubscribeCOVProperty(indication *applicationlayer.APDUIndication) {
	var req SubscribeCOVPropertyRequest
	if _, err := req.Unmarshal(indication.Data); err != nil {
		logger.Error("could not unmarshal SubscribeCOVProperty: ", err)
		sh.applicationEntity.SendErrorResponse(
			indication.InvokeId, indication.Source,
			bacnet.ConfirmedServiceChoiceSubscribeCovProperty,
			bacnet.ServicesError, bacnet.ServiceRequestDenied,
		)
		return
	}
	objType := req.monitoredObjectIdentifier.ObjType()
	objInstance := req.monitoredObjectIdentifier.Instance()

	src, _ := sh.resolveObject(objType, objInstance)
	if src == nil {
		sh.applicationEntity.SendErrorResponse(
			indication.InvokeId, indication.Source,
			bacnet.ConfirmedServiceChoiceSubscribeCovProperty,
			bacnet.ObjectError, bacnet.UnknownObject,
		)
		return
	}
	covSrc, ok := src.(covCapable)
	if !ok || len(covSrc.COVProperties()) == 0 {
		sh.applicationEntity.SendErrorResponse(
			indication.InvokeId, indication.Source,
			bacnet.ConfirmedServiceChoiceSubscribeCovProperty,
			bacnet.ServicesError, bacnet.OptionalFunctionalityNotSupported,
		)
		return
	}

	processId := uint32(req.subscriberProcessIdentifier.Value())
	propId := bacnet.PropertyIdentifier(req.monitoredPropertyReference.propertyIdentifier.Value())

	if req.IsCancel() {
		sh.removeSubscription(indication.Source, processId, objType, objInstance)
		_ = sh.applicationEntity.SendConfServResponse(
			indication.InvokeId, indication.Source,
			bacnet.ConfirmedServiceChoiceSubscribeCovProperty, nil,
		)
		return
	}

	// Validate that the requested property exists on the object.
	if covSrc.GetProperty(propId) == nil {
		sh.applicationEntity.SendErrorResponse(
			indication.InvokeId, indication.Source,
			bacnet.ConfirmedServiceChoiceSubscribeCovProperty,
			bacnet.PropertyError, bacnet.UnknownProperty,
		)
		return
	}

	confirmed := req.issueConfirmedNotifications.Get().Value()
	lifetime := uint32(req.lifetime.Get().Value())

	sub := &covSubscription{
		subscriberProcessId: processId,
		subscriber:          indication.Source,
		objectType:          objType,
		objectInstance:      objInstance,
		propertyId:          &propId,
		confirmed:           confirmed,
		lastNotifiedValues:  make(map[bacnet.PropertyIdentifier][]byte),
	}
	if req.covIncrement.Present() {
		v := req.covIncrement.Get().Value()
		sub.covIncrement = &v
	}
	if req.monitoredPropertyReference.propertyArrayIndex.Present() {
		idxVal := uint(req.monitoredPropertyReference.propertyArrayIndex.Get().Value())
		sub.propertyArrayIndex = &idxVal
	}
	if lifetime > 0 {
		sub.expiresAt = time.Now().Add(time.Duration(lifetime) * time.Second)
	}
	sh.addOrUpdateSubscription(sub)

	// Send initial notification.
	if data, err := sh.buildCOVNotification(sub, covSrc); err == nil {
		sh.sendCOVNotificationToSubscriber(sub, data)
	}

	_ = sh.applicationEntity.SendConfServResponse(
		indication.InvokeId, indication.Source,
		bacnet.ConfirmedServiceChoiceSubscribeCovProperty, nil,
	)
}

func (sh *ServiceHandler) handleIncomingConfirmedCOVNotification(indication *applicationlayer.APDUIndication) {
	var notif COVNotificationRequest
	if _, err := notif.Unmarshal(indication.Data); err != nil {
		logger.Error("could not unmarshal ConfirmedCOVNotification: ", err)
		sh.applicationEntity.SendErrorResponse(
			indication.InvokeId, indication.Source,
			bacnet.ConfirmedServiceChoiceConfirmedCovNotification,
			bacnet.ServicesError, bacnet.ServiceRequestDenied,
		)
		return
	}
	sh.covMu.Lock()
	cb := sh.covNotifyCallback
	sh.covMu.Unlock()
	if cb != nil {
		cb(notif)
	}
	_ = sh.applicationEntity.SendConfServResponse(
		indication.InvokeId, indication.Source,
		bacnet.ConfirmedServiceChoiceConfirmedCovNotification, nil,
	)
}

func (sh *ServiceHandler) handleIncomingUnconfirmedCOVNotification(indication *applicationlayer.APDUIndication) {
	var notif COVNotificationRequest
	if _, err := notif.Unmarshal(indication.Data); err != nil {
		logger.Error("could not unmarshal UnconfirmedCOVNotification: ", err)
		return
	}
	sh.covMu.Lock()
	cb := sh.covNotifyCallback
	sh.covMu.Unlock()
	if cb != nil {
		cb(notif)
	}
}

// --- COV notification dispatch ---

// CheckAndNotifyCOV sends COV notifications to all matching subscribers after
// a property value has changed. Call this after a successful WriteProperty.
func (sh *ServiceHandler) CheckAndNotifyCOV(src covCapable, changedPropId bacnet.PropertyIdentifier) {
	covProps := src.COVProperties()
	if len(covProps) == 0 {
		return
	}
	// Check if changedPropId is in the object's COV property list.
	found := false
	for _, pid := range covProps {
		if pid == changedPropId {
			found = true
			break
		}
	}
	if !found {
		return
	}

	// Get the object's type and instance so we can match subscriptions.
	oidProp := src.GetProperty(bacnet.ObjectIdentifier)
	if oidProp == nil {
		return
	}
	oid, ok := oidProp.GetValue().(*encoding.BACnetObjectIdentifier)
	if !ok {
		return
	}
	objType := oid.ObjType()
	objInstance := oid.Instance()

	sh.covMu.Lock()
	subs := make([]*covSubscription, len(sh.covSubscriptions))
	copy(subs, sh.covSubscriptions)
	sh.covMu.Unlock()

	for _, sub := range subs {
		if sub == nil {
			continue
		}
		if sub.objectType != objType || sub.objectInstance != objInstance {
			continue
		}
		// For SubscribeCOVProperty: only fire when the specific property changed.
		if sub.propertyId != nil && *sub.propertyId != changedPropId {
			continue
		}
		// COV-increment check for Real properties.
		if sub.covIncrement != nil && *sub.covIncrement > 0 {
			prop := src.GetProperty(changedPropId)
			if prop != nil {
				curBytes, err := prop.MarshalValue()
				if err == nil {
					lastBytes := sub.lastNotifiedValues[changedPropId]
					if lastBytes != nil && len(curBytes) >= 5 && len(lastBytes) >= 5 {
						curBits := uint32(curBytes[1])<<24 | uint32(curBytes[2])<<16 | uint32(curBytes[3])<<8 | uint32(curBytes[4])
						lastBits := uint32(lastBytes[1])<<24 | uint32(lastBytes[2])<<16 | uint32(lastBytes[3])<<8 | uint32(lastBytes[4])
						curVal := math.Float32frombits(curBits)
						lastVal := math.Float32frombits(lastBits)
						diff := curVal - lastVal
						if diff < 0 {
							diff = -diff
						}
						if diff < *sub.covIncrement {
							continue
						}
					}
				}
			}
		}
		data, err := sh.buildCOVNotification(sub, src)
		if err != nil {
			logger.Error("buildCOVNotification failed: ", err)
			continue
		}
		sh.sendCOVNotificationToSubscriber(sub, data)
	}
}

func (sh *ServiceHandler) sendCOVNotificationToSubscriber(sub *covSubscription, data []byte) {
	if sub.confirmed {
		sh.applicationEntity.SendConfServRequest(
			bacnet.ConfirmedServiceChoiceConfirmedCovNotification,
			sub.subscriber, false, networklayer.NormalPriority, data,
		)
	} else {
		_ = sh.applicationEntity.SendUnconfirmedService(
			bacnet.UnconfirmedServiceChoiceUnconfirmedCovNotification,
			sub.subscriber, networklayer.NormalPriority, data,
		)
	}
}

func (sh *ServiceHandler) buildCOVNotification(sub *covSubscription, src covCapable) ([]byte, error) {
	if len(sh.devices) == 0 {
		return nil, fmt.Errorf("no local device for COV notification")
	}
	devOidProp := sh.devices[0].DeviceObject().GetProperty(bacnet.ObjectIdentifier)
	if devOidProp == nil {
		return nil, fmt.Errorf("device has no ObjectIdentifier")
	}
	devOid, ok := devOidProp.GetValue().(*encoding.BACnetObjectIdentifier)
	if !ok {
		return nil, fmt.Errorf("device ObjectIdentifier wrong type")
	}

	// Determine which properties to include.
	covProps := src.COVProperties()
	propIds := covProps
	if sub.propertyId != nil {
		propIds = []bacnet.PropertyIdentifier{*sub.propertyId}
	}

	values := make([]COVPropertyValue, 0, len(propIds))
	for _, pid := range propIds {
		prop := src.GetProperty(pid)
		if prop == nil {
			continue
		}
		valBytes, err := prop.MarshalValue()
		if err != nil {
			continue
		}
		var cpv COVPropertyValue
		cpv.propertyIdentifier.SetValue(uint32(pid))
		cpv.value = encoding.NewAbstract(valBytes)
		values = append(values, cpv)
		sub.lastNotifiedValues[pid] = valBytes
	}

	var notif COVNotificationRequest
	notif.subscriberProcessIdentifier.SetValue(uint64(sub.subscriberProcessId))
	notif.initiatingDeviceIdentifier.SetFromValues(devOid.ObjType(), devOid.Instance())
	notif.monitoredObjectIdentifier.SetFromValues(sub.objectType, sub.objectInstance)
	notif.timeRemaining.SetValue(uint64(sub.timeRemaining()))
	notif.listOfValues = values

	return notif.Marshal()
}

// --- Client-side methods ---

// SubscribeCOV sends a SubscribeCOV request and waits for SimpleAck.
func (sh *ServiceHandler) SubscribeCOV(
	ctx context.Context,
	dest *bacnet.BACnetAddress,
	processId uint32,
	objType uint16,
	objInstance uint32,
	confirmed bool,
	lifetime uint32,
) error {
	var req SubscribeCOVRequest
	req.subscriberProcessIdentifier.SetValue(uint64(processId))
	req.monitoredObjectIdentifier.SetFromValues(objType, objInstance)
	var confirmedBool encoding.Boolean
	confirmedBool.SetValue(confirmed)
	req.issueConfirmedNotifications.Set(&confirmedBool)
	var lt encoding.Unsigned
	lt.SetValue(uint64(lifetime))
	req.lifetime.Set(&lt)

	data, err := req.Marshal()
	if err != nil {
		return fmt.Errorf("SubscribeCOV: marshal: %w", err)
	}
	future := sh.applicationEntity.SendConfServRequest(
		bacnet.ConfirmedServiceChoiceSubscribeCov,
		dest, true, networklayer.NormalPriority, data,
	)
	resp := future.WaitResponse(ctx)
	if resp == nil {
		return fmt.Errorf("SubscribeCOV: timeout or cancelled")
	}
	if resp.Type != applicationlayer.ResponseConfirm {
		return fmt.Errorf("SubscribeCOV: request failed (type %d)", resp.Type)
	}
	return nil
}

// CancelCOV sends a SubscribeCOV cancellation (tags [2],[3] absent).
func (sh *ServiceHandler) CancelCOV(
	ctx context.Context,
	dest *bacnet.BACnetAddress,
	processId uint32,
	objType uint16,
	objInstance uint32,
) error {
	var req SubscribeCOVRequest
	req.subscriberProcessIdentifier.SetValue(uint64(processId))
	req.monitoredObjectIdentifier.SetFromValues(objType, objInstance)
	// No issueConfirmedNotifications and no lifetime → cancel.

	data, err := req.Marshal()
	if err != nil {
		return fmt.Errorf("CancelCOV: marshal: %w", err)
	}
	future := sh.applicationEntity.SendConfServRequest(
		bacnet.ConfirmedServiceChoiceSubscribeCov,
		dest, true, networklayer.NormalPriority, data,
	)
	resp := future.WaitResponse(ctx)
	if resp == nil {
		return fmt.Errorf("CancelCOV: timeout or cancelled")
	}
	if resp.Type != applicationlayer.ResponseConfirm {
		return fmt.Errorf("CancelCOV: request failed (type %d)", resp.Type)
	}
	return nil
}

// SubscribeCOVProperty sends a SubscribeCOVProperty request and waits for SimpleAck.
func (sh *ServiceHandler) SubscribeCOVProperty(
	ctx context.Context,
	dest *bacnet.BACnetAddress,
	processId uint32,
	objType uint16,
	objInstance uint32,
	propId bacnet.PropertyIdentifier,
	propArrayIndex *uint,
	covIncrement *float32,
	confirmed bool,
	lifetime uint32,
) error {
	var req SubscribeCOVPropertyRequest
	req.subscriberProcessIdentifier.SetValue(uint64(processId))
	req.monitoredObjectIdentifier.SetFromValues(objType, objInstance)
	var confirmedBool encoding.Boolean
	confirmedBool.SetValue(confirmed)
	req.issueConfirmedNotifications.Set(&confirmedBool)
	var lt encoding.Unsigned
	lt.SetValue(uint64(lifetime))
	req.lifetime.Set(&lt)
	req.monitoredPropertyReference.propertyIdentifier.SetValue(uint32(propId))
	if propArrayIndex != nil {
		var idx encoding.Unsigned
		idx.SetValue(uint64(*propArrayIndex))
		req.monitoredPropertyReference.propertyArrayIndex.Set(&idx)
	}
	if covIncrement != nil {
		var incr encoding.Real
		incr.SetValue(*covIncrement)
		req.covIncrement.Set(&incr)
	}

	data, err := req.Marshal()
	if err != nil {
		return fmt.Errorf("SubscribeCOVProperty: marshal: %w", err)
	}
	future := sh.applicationEntity.SendConfServRequest(
		bacnet.ConfirmedServiceChoiceSubscribeCovProperty,
		dest, true, networklayer.NormalPriority, data,
	)
	resp := future.WaitResponse(ctx)
	if resp == nil {
		return fmt.Errorf("SubscribeCOVProperty: timeout or cancelled")
	}
	if resp.Type != applicationlayer.ResponseConfirm {
		return fmt.Errorf("SubscribeCOVProperty: request failed (type %d)", resp.Type)
	}
	return nil
}
