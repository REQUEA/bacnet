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
	//Todo:  check objectID is valid, use name constant for tag value
	_ = binary.Write(e.buf, binary.BigEndian, ((uint32(objectID.Type))<<types.InstanceBits)|(uint32(objectID.Instance)&types.MaxInstance))
}

func (e *Encoder) AppData(v interface{}) {
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
func (d *Decoder) ResetError() {
	d.err = nil
}

//unread unread the last n bytes read from the decoder. This allows to retry decoding of the same data
func (d *Decoder) unread(n int) error {
	for x := 0; x < n; x++ {
		err := d.buf.UnreadByte()
		if err != nil {
			return err
		}
	}
	return nil
}

//Todo: maybe add context to errors
//ContextValue reads the next context tag/value couple and set val accordingly.
//Sets the decoder error  if the tagID isn't the expected or if the tag isn't contextual.
//If ErrorIncorrectTag is set, the internal buffer cursor is ready to read again the same tag.
func (d *Decoder) ContextValue(expectedTagID byte, val *uint32) {
	if d.err != nil {
		return
	}
	length, t, err := decodeTag(d.buf)
	if err != nil {
		d.err = err
		return
	}
	if t.ID != expectedTagID {
		d.err = ErrorIncorrectTag{Expected: expectedTagID, Got: t.ID}
		err := d.unread(length)
		if err != nil {
			d.err = err
		}
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

//ContextObjectID read a (context)tag / value pair where the value
//type is an unsigned int
//If ErrorIncorrectTag is set, the internal buffer cursor is ready to read again the same tag.
func (d *Decoder) ContextObjectID(expectedTagID byte, objectID *types.ObjectID) {
	if d.err != nil {
		return
	}
	length, t, err := decodeTag(d.buf)
	if err != nil {
		d.err = err
		return
	}

	if t.ID != expectedTagID {
		d.err = ErrorIncorrectTag{Expected: expectedTagID, Got: t.ID}
		err := d.unread(length)
		if err != nil {
			d.err = err
		}
		return
	}
	if !t.Context {
		d.err = errors.New("tag isn't contextual")
		return
	}
	//Todo: check is tag size is ok
	var val uint32
	_ = binary.Read(d.buf, binary.BigEndian, &val)
	obj := types.ObjectID{
		Type:     types.ObjectType(val >> types.InstanceBits),
		Instance: types.ObjectInstance(val & types.MaxInstance),
	}
	*objectID = obj
}

//AppData read the next tag and value. The value type advertised
//in tag must be a standard bacnet application data type and must
//match the type passed in the v parameter. If no error is
//returned, v will contain the data read
func (d *Decoder) AppData(v interface{}) {
	if d.err != nil {
		return
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		d.err = errors.New("decodeAppData: interface parameter isn't a pointer")
		return
	}
	_, tag, err := decodeTag(d.buf)
	if err != nil {
		d.err = fmt.Errorf("decodeAppData: read tag: %w", err)
		return
	}
	//TODO: return err if tag is context
	//Take the pointer value
	rv = rv.Elem()
	//TODO: Make stringer  of AppliactionTag and print them rather than fixed string
	//Todo: Ensure that rv.Kind() != reflect.Interface checks if empty interface is passed
	switch tag.ID {
	case applicationTagUnsignedInt:
		if rv.Kind() != reflect.Uint8 && rv.Kind() != reflect.Uint16 && rv.Kind() != reflect.Uint32 && rv.Kind() != reflect.Interface {
			d.err = fmt.Errorf("decodeAppData: mismatched type, cannot decode %s in type %s", "UnsignedInt", rv.Type().String())
			return
		}
		val, err := decodeUnsignedWithLen(d.buf, int(tag.Value))
		if err != nil {
			d.err = fmt.Errorf("decodeAppData: read ObjectID: %w", err)
			return
		}

		rv.Set(reflect.ValueOf(val))
	case applicationTagEnumerated:
		var seg types.SegmentationSupport
		if rv.Type() != reflect.TypeOf(seg) && rv.Kind() != reflect.Interface {
			d.err = fmt.Errorf("decodeAppData: mismatched type, cannot decode %s in type %s", "Enumerated", rv.Type().String())
			return
		}
		val, err := decodeUnsignedWithLen(d.buf, int(tag.Value))
		if err != nil {
			d.err = fmt.Errorf("decodeAppData: read ObjectID: %w", err)
			return
		}
		rv.Set(reflect.ValueOf(types.SegmentationSupport(val)))
	case applicationTagObjectID:
		var obj types.ObjectID
		if rv.Type() != reflect.TypeOf(obj) && rv.Kind() != reflect.Interface {
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
	case applicationTagCharacterString:
		var s string
		if rv.Type() != reflect.TypeOf(s) && rv.Kind() != reflect.Interface {
			d.err = fmt.Errorf("decodeAppData: mismatched type, cannot decode %s in type %s", "CharacterString", rv.Type().String())
			return
		}
		sEncoding, err := d.buf.ReadByte()
		if err != nil {
			d.err = err //Todo, wrap
			return
		}
		if sEncoding != utf8Encoding {
			d.err = fmt.Errorf("unsuported strign encoding: 0x%x", sEncoding)
			return
		}
		b := make([]byte, int(tag.Value)-1) //Minus one because encoding is already consumed
		err = binary.Read(d.buf, binary.BigEndian, b)
		if err != nil {
			d.err = err //todo: wrap
			return
		}
		s = string(b) //Conversion allowed because string are utf8 only in go
		rv.Set(reflect.ValueOf(s))
	default:
		//TODO: support all app data types
		d.err = fmt.Errorf("decodeAppData: unsupported type 0x%x", tag.ID)
		return
	}
}

const utf8Encoding = byte(0)

func (d *Decoder) ContextAbstractType(expectedTagNumber byte, v interface{}) {
	if d.err != nil {
		return
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		d.err = errors.New("decodeAppData: interface parameter isn't a pointer")
		return
	}
	_, tag, err := decodeTag(d.buf)
	if err != nil {
		d.err = fmt.Errorf("decoder abstractType: read opening tag: %w", err)
		return
	}
	if !tag.Opening {
		d.err = fmt.Errorf("decoder abstractType: expected opening tag")
		return
	}
	if tag.ID != expectedTagNumber {
		d.err = ErrorIncorrectTag{Expected: expectedTagNumber, Got: tag.ID}
	}
	//Todo: check if we can have several tag inside the Opening/closing pair
	d.AppData(v)
	if d.err != nil {
		return
	}
	_, tag, err = decodeTag(d.buf)
	if err != nil {
		d.err = fmt.Errorf("decoder abstractType: read closing tag: %w", err)
		return
	}
	if !tag.Closing {
		d.err = fmt.Errorf("decoder abstractType: expected closing tag")
		return
	}
	if tag.ID != expectedTagNumber {
		d.err = ErrorIncorrectTag{Expected: expectedTagNumber, Got: tag.ID}
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
