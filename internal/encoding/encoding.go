package encoding

import (
	"bytes"
	"encoding/binary"
	"github.com/REQUEA/bacnet"
)

const (
	flag16bits byte = 0xFE
	flag32bits byte = 0xFF
)

// Encoder is the struct used to turn bacnet types to byte arrays. All
// public methods of encoder can set the internal error value. If such
// error is set, all encoding methods will be no-ops. This allows to
// defer error checking after several encoding operations
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

// ContextUnsigned write a (context)tag / value pair where the value
// type is an unsigned int
func (e *Encoder) ContextUnsigned(tabNumber byte, value uint32) {
	if e.err != nil {
		return
	}
	t := tag{
		ID:      tabNumber,
		Context: true,
	}
	writeUint(e.buf, t, value)
}

// ContextObjectID write a (context)tag / value pair where the value
// type is an unsigned int
func (e *Encoder) ContextObjectID(tabNumber byte, objectID bacnet.ObjectID) {
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
	encodeTag(e.buf, t)
	v, err := objectID.Encode()
	if err != nil {
		e.err = err
		return
	}
	_ = binary.Write(e.buf, binary.BigEndian, v)
}

// AppData writes a tag and value of any standard bacnet application
// data type. Returns an error if v if of a invalid type
func (e *Encoder) AppData(v any) {
	if e.err != nil {
		return
	}
	switch val := v.(type) {
	case bacnet.SegmentationSupport:
		v := uint32(val)
		t := tag{ID: applicationTagEnumerated}
		writeUint(e.buf, t, v)
	case bacnet.ObjectID:
		t := tag{ID: applicationTagObjectID, Value: 4}
		encodeTag(e.buf, t)
		v, err := val.Encode()
		if err != nil {
			e.err = err
			return
		}
		_ = binary.Write(e.buf, binary.BigEndian, v)
	default:
		writeValue(e.buf, v)
	}
}

func (e *Encoder) ContextAbstractType(tabNumber byte, v bacnet.PropertyValue) {
	encodeTag(e.buf, tag{ID: tabNumber, Context: true, Opening: true})
	writeValue(e.buf, v.Value)
	encodeTag(e.buf, tag{ID: tabNumber, Context: true, Closing: true})
}

// writeValue writes the value in the buffer using a variabled-sized encoding
// current not support 64bit integers
func writeValue(buf *bytes.Buffer, value any) {
	t := tag{}
	if value == nil {
		t.ID = applicationTagNull
		encodeTag(buf, t)
		return
	}
	switch value.(type) {
	case bool:
		t.ID = applicationTagBoolean
		v := value.(bool)
		if v {
			t.Value = 1
		}
		encodeTag(buf, t)
	case uint8:
		t.ID = applicationTagUnsignedInt
		writeUint(buf, t, uint32(value.(uint8)))
	case uint16:
		t.ID = applicationTagUnsignedInt
		writeUint(buf, t, uint32(value.(uint16)))
	case uint32:
		t.ID = applicationTagUnsignedInt
		writeUint(buf, t, value.(uint32))
	case int8:
		t.ID = applicationTagSignedInt
		writeInt(buf, t, int32(value.(int8)))
	case int16:
		t.ID = applicationTagSignedInt
		writeInt(buf, t, int32(value.(int16)))
	case int32:
		t.ID = applicationTagSignedInt
		writeInt(buf, t, value.(int32))
	case float32:
		t.ID = applicationTagReal
		t.Value = 4
		writeFloat(buf, t, float64(value.(float32)))
	case float64:
		t.ID = applicationTagDouble
		t.Value = 8
		writeFloat(buf, t, value.(float64))
	case string:
		v := value.(string)
		t.ID = applicationTagCharacterString
		t.Value = uint32(len(v) + 1)
		encodeTag(buf, t)
		_ = buf.WriteByte(utf8Encoding)
		_, _ = buf.Write([]byte(v))
	}
}

func writeUint(buf *bytes.Buffer, t tag, value uint32) {
	switch {
	case value < 0x100:
		t.Value = 1
		encodeTag(buf, t)
		buf.WriteByte(uint8(value))
	case value < 0x10000:
		t.Value = 2
		encodeTag(buf, t)
		_ = binary.Write(buf, binary.BigEndian, uint16(value))
	case value < 0x1000000:
		// There is no default 24 bit integer in go, so we have to
		// write it manually (in big endian)
		t.Value = 3
		encodeTag(buf, t)
		buf.WriteByte(byte(value >> 16))
		_ = binary.Write(buf, binary.BigEndian, uint16(value))
	default:
		t.Value = 4
		_ = binary.Write(buf, binary.BigEndian, value)
	}
}

func writeInt(buf *bytes.Buffer, t tag, value int32) int {
	switch {
	case value >= -0x80 && value < 0x80:
		t.Value = 1
		encodeTag(buf, t)
		buf.WriteByte(uint8(value))
	case value >= -0x8000 && value < 0x8000:
		t.Value = 2
		encodeTag(buf, t)
		_ = binary.Write(buf, binary.BigEndian, int16(value))
	case value >= -0x800000 && value < 0x800000:
		t.Value = 3
		encodeTag(buf, t)
		buf.WriteByte(byte(value >> 16))
		_ = binary.Write(buf, binary.BigEndian, int16(value))
	default:
		t.Value = 4
		encodeTag(buf, t)
		_ = binary.Write(buf, binary.BigEndian, value)
	}
	return int(t.Value)
}

func writeFloat(buf *bytes.Buffer, t tag, value float64) int {
	encodeTag(buf, t)
	if t.Value == 4 {
		_ = binary.Write(buf, binary.BigEndian, float32(value))
	} else {
		_ = binary.Write(buf, binary.BigEndian, value)
	}
	return int(t.Value)
}
