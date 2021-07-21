package main

import (
	"bytes"
	"encoding/binary"
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
