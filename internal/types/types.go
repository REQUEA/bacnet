package types

const MaxInstance = 0x3FFFFF
const InstanceBits = 22

type ObjectType uint16
type ObjectInstance uint32

type ObjectID struct {
	Type     ObjectType
	Instance ObjectInstance
}

//go:generate stringer -type=SegmentationSupport
type SegmentationSupport byte

const (
	SegmentationSupportBoth     SegmentationSupport = 0x00
	SegmentationSupportTransmit SegmentationSupport = 0x01
	SegmentationSupportReceive  SegmentationSupport = 0x02
	SegmentationSupportNone     SegmentationSupport = 0x03
)
