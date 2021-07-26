package bacnet

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
)

const (
	ApplicationTagNull            byte = 0x00
	ApplicationTagBoolean         byte = 0x01
	ApplicationTagUnsignedInt     byte = 0x02
	ApplicationTagSignedInt       byte = 0x03
	ApplicationTagReal            byte = 0x04
	ApplicationTagDouble          byte = 0x05
	ApplicationTagOctetString     byte = 0x06
	ApplicationTagCharacterString byte = 0x07
	ApplicationTagBitString       byte = 0x08
	ApplicationTagEnumerated      byte = 0x09
	ApplicationTagDate            byte = 0x0A
	ApplicationTagTime            byte = 0x0B
	ApplicationTagObjectId        byte = 0x0C
)

type tag struct {
	// Tag id. Typically sequential, except when it is not...
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
			binary.Write(buf, binary.BigEndian, uint16(t.Value))
		} else {
			buf.WriteByte(flag32bits)
			binary.Write(buf, binary.BigEndian, uint32(t.Value))
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

func contextUnsigned(buf *bytes.Buffer, tabNumber byte, value uint32) int {
	length := valueLength(value)
	t := tag{
		ID:      tabNumber,
		Context: true,
		Value:   uint32(length),
		Opening: false,
		Closing: false,
	}
	b, _ := t.MarshallBinary()
	buf.Write(b)
	return len(b) + unsigned(buf, value)
}

func unsigned(buf *bytes.Buffer, value uint32) int {
	if value < 0x100 {
		buf.WriteByte(uint8(value))
		return 1
	} else if value < 0x10000 {
		binary.Write(buf, binary.BigEndian, uint16(value))
		return 2
	} else if value < 0x1000000 {
		// There is no default 24 bit integer in go, so we have to
		// write it manually (in big endian)
		buf.WriteByte(byte(value >> 16))
		binary.Write(buf, binary.BigEndian, uint16(value))
		return 3
	} else {
		binary.Write(buf, binary.BigEndian, value)
		return 4
	}
}

func decodeTag(buf *bytes.Buffer) (len int, t tag, err error) {
	length := 1
	firstByte, err := buf.ReadByte()
	if err != nil {
		return 0, t, fmt.Errorf("Failed to read tagID: %w", err)
	}
	if isExtendedTagNumber(firstByte) {
		tagNumber, err := buf.ReadByte()
		if err != nil {
			return 0, t, fmt.Errorf("Failed to read extended tagId: %w", err)
		}
		t.ID = tagNumber
		length++
	} else {
		tagNumber := firstByte >> 4
		t.ID = tagNumber
	}

	if isOpeningTag(firstByte) {
		t.Opening = true
		return length, t, nil
	}
	if isClosingTag(firstByte) {
		t.Closing = true
		return length, t, nil
	}
	//TODO: IScontext specific ?
	if isExtendedValue(firstByte) {
		firstValueByte, err := buf.ReadByte()
		if err != nil {
			return 0, t, fmt.Errorf("Failed to read first byte of extended value tag: %w", err)
		}
		length++
		switch firstValueByte {
		case flag16bits:
			var val uint16
			err := binary.Read(buf, binary.BigEndian, &val)
			if err != nil {
				return 0, t, fmt.Errorf("Failed to read extended 16bits tag value: %w ", err)
			}
			length += 2
			t.Value = uint32(val)
		case flag32bits:
			err := binary.Read(buf, binary.BigEndian, &t.Value)
			if err != nil {
				return 0, t, fmt.Errorf("Failed to read extended 32bits tag value: %w", err)
			}
			length += 4
		default:
			t.Value = uint32(firstValueByte)

		}
	} else {
		t.Value = uint32(firstByte & 0x7)
	}
	return length, t, nil
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
		// There is no default 24 bit integer in go, so we have to
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
		return uint32(val), nil
	default:
		//TODO: check If allowed by specification, other
		//implementation allow it but i'm not sure
		return 0, nil
	}
}

//decodeAppData read the next tag and value. The value type advertised
//in tag must be a standard bacnet application data type and must
//match the type passed in the v parameter. If no error is
//returned, v will contain the data read
func decodeAppData(buf *bytes.Buffer, v interface{}) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return errors.New("decodeAppData: interface parameter isn't a pointer")
	}
	_, tag, err := decodeTag(buf)
	if err != nil {
		return fmt.Errorf("decodeAppData: read tag: %w", err)
	}
	//TODO: return err if tag is context
	//Take the pointer value
	rv = rv.Elem()
	//TODO: Make stringer  of AppliactionTag and print them rather than fixed string
	switch tag.ID {
	case ApplicationTagUnsignedInt:
		if rv.Kind() != reflect.Uint8 && rv.Kind() != reflect.Uint16 && rv.Kind() != reflect.Uint32 {
			return fmt.Errorf("decodeAppData: mismatched type, cannot decode %s in type %s", "UnsignedInt", rv.Type().String())
		}
		val, err := decodeUnsignedWithLen(buf, int(tag.Value))
		if err != nil {
			return fmt.Errorf("decodeAppData: read ObjectID: %w", err)
		}

		rv.SetUint(uint64(val))
	case ApplicationTagEnumerated:
		var seg SegmentationSupport
		if rv.Type() != reflect.TypeOf(seg) {
			return fmt.Errorf("decodeAppData: mismatched type, cannot decode %s in type %s", "Enumerated", rv.Type().String())
		}
		val, err := decodeUnsignedWithLen(buf, int(tag.Value))
		if err != nil {
			return fmt.Errorf("decodeAppData: read ObjectID: %w", err)
		}
		rv.SetUint(uint64(val))
	case ApplicationTagObjectId:
		var obj ObjectID
		if rv.Type() != reflect.TypeOf(obj) {
			return fmt.Errorf("decodeAppData: mismatched type, cannot decode %s in type %s", "ObjectID", rv.Type().String())
		}
		var val uint32
		err := binary.Read(buf, binary.BigEndian, &val)
		if err != nil {
			return fmt.Errorf("decodeAppData: read ObjectID: %w", err)
		}
		obj = ObjectID{
			Type:     ObjectType(val >> InstanceBits),
			Instance: ObjectInstance(val & MaxInstance),
		}
		rv.Set(reflect.ValueOf(obj))
	default:
		//TODO: support all app data types
		return fmt.Errorf("decodeAppData: unsupported type 0x%x", tag.ID)
	}
	return nil
}
