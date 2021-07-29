package encoding

import (
	"bacnet/internal/types"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
)

//Decoder is the struct used to turn byte arrays to bacnet types. All
//public methods of decoder can set the internal error value. If such
//error is set, all decoding methods will be no-ops. This allows to
//defer error checking after several decoding operations
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
	*objectID = types.ObjectIDFromUint32(val)
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
	//Todo: Ensure that rv.Kind() != reflect.Interface checks if empty interface is passed, maybe chack that number of method is empty ?
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
		obj = types.ObjectIDFromUint32(val)
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
