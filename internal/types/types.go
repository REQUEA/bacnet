package types

import (
	"errors"
)

const (
	MaxInstance   = 0x3FFFFF
	instanceBits  = 22
	maxObjectType = 0x400
)

type ObjectType uint16
type ObjectInstance uint32

type ObjectID struct {
	Type     ObjectType
	Instance ObjectInstance
}

//Encode turns the object ID into a uint32 for encoding.  Returns an
//error if the ObjectID is invalid
func (o ObjectID) Encode() (uint32, error) {
	if o.Instance > MaxInstance {
		return 0, errors.New("invalid ObjectID: instance too high")
	}
	if o.Type > maxObjectType {
		return 0, errors.New("invalid ObjectID: objectType too high too high")
	}
	v := uint32(o.Type)<<instanceBits | (uint32(o.Instance))
	return v, nil
}

func ObjectIDFromUint32(v uint32) ObjectID {
	return ObjectID{
		Type:     ObjectType(v >> instanceBits),
		Instance: ObjectInstance(v & MaxInstance),
	}
}

//go:generate stringer -type=SegmentationSupport
type SegmentationSupport byte

const (
	SegmentationSupportBoth     SegmentationSupport = 0x00
	SegmentationSupportTransmit SegmentationSupport = 0x01
	SegmentationSupportReceive  SegmentationSupport = 0x02
	SegmentationSupportNone     SegmentationSupport = 0x03
)

//PropertyIdentifier is used to control a ReadProperty request
type PropertyIdentifier struct {
	Type uint32
	//Not null if it's an array property and we want only one index of
	//this array
	ArrayIndex *uint32
}
