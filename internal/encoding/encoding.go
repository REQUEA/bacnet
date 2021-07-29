package encoding

import (
	"bacnet/internal/types"
	"bytes"
	"encoding/binary"
	"fmt"
)

const (
	flag16bits byte = 0xFE
	flag32bits byte = 0xFF
)

//Encoder is the struct used to turn bacnet types to byte arrays. All
//public methods of encoder can set the internal error value. If such
//error is set, all encoding methods will be no-ops. This allows to
//defer error checking after several encoding operations
type Encoder struct {
	buf *bytes.Buffer
	err error
}

func NewEncoder() Encoder {
	e := Encoder{
		buf: new(bytes.Buffer),
		err: nil,
	}
	return e
}

func (e *Encoder) Error() error {
	return e.err
}

func (e *Encoder) Bytes() []byte {
	return e.buf.Bytes()
}

//ContextUnsigned write a (context)tag / value pair where the value
//type is an unsigned int
func (e *Encoder) ContextUnsigned(tabNumber byte, value uint32) {
	if e.err != nil {
		return
	}
	length := valueLength(value)
	t := tag{
		ID:      tabNumber,
		Context: true,
		Value:   uint32(length),
		Opening: false,
		Closing: false,
	}
	err := encodeTag(e.buf, t)
	if err != nil {
		e.err = err
		return
	}
	unsigned(e.buf, value)
}

//ContextObjectID write a (context)tag / value pair where the value
//type is an unsigned int
func (e *Encoder) ContextObjectID(tabNumber byte, objectID types.ObjectID) {
	if e.err != nil {
		return
	}
	t := tag{
		ID:      tabNumber,
		Context: true,
		Value:   4, //length of objectID is 4
		Opening: false,
		Closing: false,
	}
	err := encodeTag(e.buf, t)
	if err != nil {
		e.err = err
		return
	}
	v, err := objectID.Encode()
	if err != nil {
		e.err = err
		return
	}
	_ = binary.Write(e.buf, binary.BigEndian, v)
}

func (e *Encoder) AppData(v interface{}) {
	if e.err != nil {
		return
	}
	switch val := v.(type) {
	case float64, bool:
		e.err = fmt.Errorf("not implemented ")
	case float32:
		t := tag{ID: applicationTagReal, Value: 4}
		err := encodeTag(e.buf, t)
		if err != nil {
			e.err = err
			return
		}
		_ = binary.Write(e.buf, binary.BigEndian, val)
	case string:
		//+1 because there will be one byte for the string encoding format
		t := tag{ID: applicationTagCharacterString, Value: uint32(len(val) + 1)}
		err := encodeTag(e.buf, t)
		if err != nil {
			e.err = err
			return
		}
		_ = e.buf.WriteByte(utf8Encoding)
		_, _ = e.buf.Write([]byte(val))
	case uint32:
		length := valueLength(val)
		t := tag{ID: applicationTagUnsignedInt, Value: uint32(length)}
		err := encodeTag(e.buf, t)
		if err != nil {
			e.err = err
			return
		}
		unsigned(e.buf, val)
	case types.SegmentationSupport:
		v := uint32(val)
		length := valueLength(v)
		t := tag{ID: applicationTagEnumerated, Value: uint32(length)}
		err := encodeTag(e.buf, t)
		if err != nil {
			e.err = err
			return
		}
		unsigned(e.buf, v)
	case types.ObjectID:
		//Todo : Maybe use static values for default types ?
		t := tag{ID: applicationTagObjectID, Value: 4}
		err := encodeTag(e.buf, t)
		if err != nil {
			e.err = err
			return
		}
		v, err := val.Encode()
		if err != nil {
			e.err = err
			return
		}
		_ = binary.Write(e.buf, binary.BigEndian, v)
	default:
		e.err = fmt.Errorf("encodeAppdata: unknown type %T", v)
	}
}

// valueLength caclulates how large the necessary value needs to be to fit in the appropriate
// packet length
func valueLength(value uint32) int {
	/* length of enumerated is variable, as per 20.2.11 */
	if value < 0x100 {
		return 1
	} else if value < 0x10000 {
		return 2
	} else if value < 0x1000000 {
		return 3
	}
	return 4
}

//unsigned writes the value in the buffer using a variabled-sized encoding
func unsigned(buf *bytes.Buffer, value uint32) int {
	switch {
	case value < 0x100:
		buf.WriteByte(uint8(value))
		return 1
	case value < 0x10000:
		_ = binary.Write(buf, binary.BigEndian, uint16(value))
		return 2
	case value < 0x100000:
		// There is no default 24 bit integer in go, so we have to
		// write it manually (in big endian)
		buf.WriteByte(byte(value >> 16))
		_ = binary.Write(buf, binary.BigEndian, uint16(value))
		return 3
	default:
		_ = binary.Write(buf, binary.BigEndian, value)
		return 4
	}
}
