package encoding

import (
	"bacnet/internal/types"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
)

const (
	applicationTagNull            byte = 0x00
	applicationTagBoolean         byte = 0x01
	applicationTagUnsignedInt     byte = 0x02
	applicationTagSignedInt       byte = 0x03
	applicationTagReal            byte = 0x04
	applicationTagDouble          byte = 0x05
	applicationTagOctetString     byte = 0x06
	applicationTagCharacterString byte = 0x07
	applicationTagBitString       byte = 0x08
	applicationTagEnumerated      byte = 0x09
	applicationTagDate            byte = 0x0A
	applicationTagTime            byte = 0x0B
	applicationTagObjectID        byte = 0x0C
)

//Todo: maybe not several boolean but a type field
type tag struct {
	// Tag id. Typically sequential when tag is contextual. Or refer
	// to the standard AppData Types
	ID      byte
	Context bool
	// Either has a value or length of the next value
	Value   uint32
	Opening bool
	Closing bool
}

func isExtendedTagNumber(x byte) bool {
	return x&0xF0 == 0xF0
}

func isExtendedValue(x byte) bool {
	return x&7 == 5
}

func isOpeningTag(x byte) bool {
	return x&7 == 6

}
func isClosingTag(x byte) bool {
	return x&7 == 7
}

func isContextSpecific(x byte) bool {
	return x&8 > 0
}

const (
	flag16bits byte = 0xFE
	flag32bits byte = 0xFF
)

type Encoder struct {
	buf *bytes.Buffer
	err error
}

//Todo: doc
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

//ContextUnsigned write a (context)tag / value pair
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
	b, err := t.MarshallBinary()
	if err != nil {
		e.err = err
		return
	}
	_, _ = e.buf.Write(b)
	unsigned(e.buf, value)
}

func (e *Encoder) EncodeAppData(v interface{}) {
	if e.err != nil {
		return
	}
	switch val := v.(type) {
	case float32:
	case float64:
	case bool:
	case string:
		e.err = fmt.Errorf("not implemented ")
	case uint32:
		length := valueLength(val)
		t := tag{ID: applicationTagUnsignedInt, Value: uint32(length)}
		b, err := t.MarshallBinary()
		if err != nil {
			e.err = err
			return
		}
		_, _ = e.buf.Write(b)
		unsigned(e.buf, val)
	case types.SegmentationSupport:
		v := uint32(val)
		length := valueLength(v)
		t := tag{ID: applicationTagEnumerated, Value: uint32(length)}
		b, err := t.MarshallBinary()
		if err != nil {
			e.err = err
			return
		}
		_, _ = e.buf.Write(b)
		unsigned(e.buf, v)
	case types.ObjectID:
		//Todo : Maybe use static values for default types ?
		t := tag{ID: applicationTagObjectID, Value: 4}
		b, err := t.MarshallBinary()
		if err != nil {
			e.err = err
			return
		}
		_, _ = e.buf.Write(b)
		//Todo: maybe check that Type and instance are not invalid ?
		_ = binary.Write(e.buf, binary.BigEndian, ((uint32(val.Type))<<types.InstanceBits)|(uint32(val.Instance)&types.MaxInstance))
	default:
		e.err = fmt.Errorf("encodeAppdata: unknown type %T", v)
	}
}

//Todo: doc
type Decoder struct {
	buf *bytes.Buffer
	err error
	//tagCounter int
}

func NewDecoder(b []byte) *Decoder {
	return &Decoder{
		buf: bytes.NewBuffer(b),
		err: nil,
	}
}

func (d *Decoder) Error() error {
	return d.err
}

//Todo: maybe add context to errors
//ContextValue reads the next context tag/value couple and set val accordingly.
//Sets the decoder error  if the tagID isn't the expected or if the tag isn't contextual.
func (d *Decoder) ContextValue(expectedTagID byte, val *uint32) {
	if d.err != nil {
		return
	}
	t, err := decodeTag(d.buf)
	if err != nil {
		d.err = err
		return
	}
	if t.ID != expectedTagID {
		d.err = ErrorIncorrectTag{Expected: expectedTagID, Got: t.ID}
		return
	}
	if !t.Context {
		d.err = errors.New("tag isn't contextual")
	}
	v, err := decodeUnsignedWithLen(d.buf, int(t.Value))
	if err != nil {
		d.err = err
		return
	}
	*val = v
}

type ErrorIncorrectTag struct {
	Expected byte
	Got      byte
}

func (e ErrorIncorrectTag) Error() string {
	return fmt.Sprintf("incorrect tag %d, expected %d.", e.Got, e.Expected)
}

//DecodeAppData read the next tag and value. The value type advertised
//in tag must be a standard bacnet application data type and must
//match the type passed in the v parameter. If no error is
//returned, v will contain the data read
func (d *Decoder) DecodeAppData(v interface{}) {
	if d.err != nil {
		return
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		d.err = errors.New("decodeAppData: interface parameter isn't a pointer")
		return
	}
	tag, err := decodeTag(d.buf)
	if err != nil {
		d.err = fmt.Errorf("decodeAppData: read tag: %w", err)
		return
	}
	//TODO: return err if tag is context
	//Take the pointer value
	rv = rv.Elem()
	//TODO: Make stringer  of AppliactionTag and print them rather than fixed string
	switch tag.ID {
	case applicationTagUnsignedInt:
		if rv.Kind() != reflect.Uint8 && rv.Kind() != reflect.Uint16 && rv.Kind() != reflect.Uint32 {
			d.err = fmt.Errorf("decodeAppData: mismatched type, cannot decode %s in type %s", "UnsignedInt", rv.Type().String())
			return
		}
		val, err := decodeUnsignedWithLen(d.buf, int(tag.Value))
		if err != nil {
			d.err = fmt.Errorf("decodeAppData: read ObjectID: %w", err)
			return
		}

		rv.SetUint(uint64(val))
	case applicationTagEnumerated:
		var seg types.SegmentationSupport
		if rv.Type() != reflect.TypeOf(seg) {
			d.err = fmt.Errorf("decodeAppData: mismatched type, cannot decode %s in type %s", "Enumerated", rv.Type().String())
			return
		}
		val, err := decodeUnsignedWithLen(d.buf, int(tag.Value))
		if err != nil {
			d.err = fmt.Errorf("decodeAppData: read ObjectID: %w", err)
			return
		}
		rv.SetUint(uint64(val))
	case applicationTagObjectID:
		var obj types.ObjectID
		if rv.Type() != reflect.TypeOf(obj) {
			d.err = fmt.Errorf("decodeAppData: mismatched type, cannot decode %s in type %s", "ObjectID", rv.Type().String())
			return
		}
		var val uint32
		err := binary.Read(d.buf, binary.BigEndian, &val)
		if err != nil {
			d.err = fmt.Errorf("decodeAppData: read ObjectID: %w", err)
			return
		}
		obj = types.ObjectID{
			Type:     types.ObjectType(val >> types.InstanceBits),
			Instance: types.ObjectInstance(val & types.MaxInstance),
		}
		rv.Set(reflect.ValueOf(obj))
	default:
		//TODO: support all app data types
		d.err = fmt.Errorf("decodeAppData: unsupported type 0x%x", tag.ID)
		return
	}
}

//Todo: should we really return an error here ?
func (t tag) MarshallBinary() ([]byte, error) {
	buf := &bytes.Buffer{}
	var tagMeta byte
	if t.Context {
		tagMeta |= 0x8
	}
	if t.Opening {
		tagMeta |= 0x6
	}
	if t.Closing {
		tagMeta |= 0x7
	}
	if t.Value <= 4 {
		tagMeta |= byte(t.Value)
	} else {
		tagMeta |= 5
	}

	if t.ID <= 14 {
		tagMeta |= (t.ID << 4)
		buf.WriteByte(tagMeta)

		// We don't have enough space so make it in a new byte
	} else {
		tagMeta |= byte(0xF0)
		buf.WriteByte(tagMeta)
		buf.WriteByte(t.ID)
	}

	if t.Value > 4 {
		// Depending on the length, we will either write it as an 8 bit, 32 bit, or 64 bit integer
		if t.Value <= 253 {
			buf.WriteByte(byte(t.Value))
		} else if t.Value <= 65535 {
			buf.WriteByte(flag16bits)
			_ = binary.Write(buf, binary.BigEndian, uint16(t.Value))
		} else {
			buf.WriteByte(flag32bits)
			_ = binary.Write(buf, binary.BigEndian, t.Value)
		}
	}
	return buf.Bytes(), nil
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

func decodeTag(buf *bytes.Buffer) (t tag, err error) {
	firstByte, err := buf.ReadByte()
	if err != nil {
		return t, fmt.Errorf("read tagID: %w", err)
	}
	if isExtendedTagNumber(firstByte) {
		tagNumber, err := buf.ReadByte()
		if err != nil {
			return t, fmt.Errorf("read extended tagId: %w", err)
		}
		t.ID = tagNumber
	} else {
		tagNumber := firstByte >> 4
		t.ID = tagNumber
	}

	if isOpeningTag(firstByte) {
		t.Opening = true
		return t, nil
	}
	if isClosingTag(firstByte) {
		t.Closing = true
		return t, nil
	}
	if isContextSpecific(firstByte) {
		t.Context = true
	}
	if isExtendedValue(firstByte) {
		firstValueByte, err := buf.ReadByte()
		if err != nil {
			return t, fmt.Errorf("read first byte of extended value tag: %w", err)
		}
		switch firstValueByte {
		case flag16bits:
			var val uint16
			err := binary.Read(buf, binary.BigEndian, &val)
			if err != nil {
				return t, fmt.Errorf("read extended 16bits tag value: %w ", err)
			}
			t.Value = uint32(val)
		case flag32bits:
			err := binary.Read(buf, binary.BigEndian, &t.Value)
			if err != nil {
				return t, fmt.Errorf("read extended 32bits tag value: %w", err)
			}
		default:
			t.Value = uint32(firstValueByte)

		}
	} else {
		t.Value = uint32(firstByte & 0x7)
	}
	return t, nil
}

const (
	size8  = 1
	size16 = 2
	size24 = 3
	size32 = 4
)

func decodeUnsignedWithLen(buf *bytes.Buffer, length int) (uint32, error) {
	switch length {
	case size8:
		val, err := buf.ReadByte()
		if err != nil {
			return 0, fmt.Errorf("read unsigned with length 1 : %w", err)
		}
		return uint32(val), nil
	case size16:
		var val uint16
		err := binary.Read(buf, binary.BigEndian, &val)
		if err != nil {
			return 0, fmt.Errorf("read unsigned with length 2 : %w", err)
		}
		return uint32(val), nil
	case size24:
		// There is no default 24 bit integer in go, so we have tXo
		// write it manually (in big endian)
		var val uint16
		msb, err := buf.ReadByte()
		if err != nil {
			return 0, fmt.Errorf("read unsigned with length 3 : %w", err)
		}
		err = binary.Read(buf, binary.BigEndian, &val)
		if err != nil {
			return 0, fmt.Errorf("read unsigned with length 3 : %w", err)
		}
		return uint32(msb)<<16 + uint32(val), nil
	case size32:
		var val uint32
		err := binary.Read(buf, binary.BigEndian, &val)
		if err != nil {
			return 0, fmt.Errorf("read unsigned with length 4 : %w", err)
		}
		return val, nil
	default:
		//TODO: check If allowed by specification, other
		//implementation allow it but i'm not sure
		return 0, nil
	}
}
