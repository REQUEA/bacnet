package encoding

import (
	"reflect"
	"testing"
)

func TestParseVarLenNotExtendedBelow5(t *testing.T) {
	buf := []byte{
		byte(5)<<tagNumberShift | 3,
		1,
		2,
		3,
		4,
		5,
	}
	result, remaining, err := parseVarLen(5, buf)
	if err != nil {
		t.Fatal("did not expect an error")
	}
	expectedRemaining := []byte{4, 5}
	if !reflect.DeepEqual(expectedRemaining, remaining) {
		t.Errorf("expected remaining %v, %v", expectedRemaining, remaining)
	}
	expected := []byte{1, 2, 3}
	if !reflect.DeepEqual(expected, result) {
		t.Errorf("expected result %v, got %v", expected, result)
	}
}

func TestParseVarLenNotExtendedBelow254(t *testing.T) {
	buf := []byte{
		byte(5)<<tagNumberShift | 5,
		42, // data is 42 bytes long
	}
	expected := make([]byte, 42)
	for i := range 42 {
		expected[i] = byte(i)
	}
	buf = append(buf, expected...)

	expectedRemaining := []byte{4, 5}
	buf = append(buf, expectedRemaining...)

	result, remaining, err := parseVarLen(5, buf)
	if err != nil {
		t.Fatal("did not expect an error")
	}
	if !reflect.DeepEqual(expectedRemaining, remaining) {
		t.Errorf("expected remaining %v, %v", expectedRemaining, remaining)
	}
	if !reflect.DeepEqual(expected, result) {
		t.Errorf("expected result %v, got %v", expected, result)
	}
}

func TestParseVarLenNotExtendedBelow65536(t *testing.T) {
	dataLen := 4087
	buf := []byte{
		byte(5)<<tagNumberShift | 5,
		254, // length is coded on two bytes
		uint8((dataLen & 0xff00) >> 8),
		uint8(dataLen & 0xff),
	}
	expected := make([]byte, dataLen)
	for i := range dataLen {
		expected[i] = byte(i)
	}
	buf = append(buf, expected...)

	expectedRemaining := []byte{4, 5}
	buf = append(buf, expectedRemaining...)

	result, remaining, err := parseVarLen(5, buf)
	if err != nil {
		t.Fatal("did not expect an error")
	}
	if !reflect.DeepEqual(expectedRemaining, remaining) {
		t.Errorf("expected remaining %v, %v", expectedRemaining, remaining)
	}
	if !reflect.DeepEqual(expected, result) {
		t.Errorf("expected result %v, got %v", expected, result)
	}
}

func TestParseVarLenNotExtended65536AndAbove(t *testing.T) {
	dataLen := 1284325
	buf := []byte{
		byte(5)<<tagNumberShift | 5,
		255, // length is coded on four bytes
		uint8((dataLen & 0xff000000) >> 24),
		uint8((dataLen & 0x00ff0000) >> 16),
		uint8((dataLen & 0x0000ff00) >> 8),
		uint8(dataLen & 0x000000ff),
	}
	expected := make([]byte, dataLen)
	for i := range dataLen {
		expected[i] = byte(i)
	}
	buf = append(buf, expected...)

	expectedRemaining := []byte{4, 5}
	buf = append(buf, expectedRemaining...)

	result, remaining, err := parseVarLen(5, buf)
	if err != nil {
		t.Fatal("did not expect an error")
	}
	if !reflect.DeepEqual(expectedRemaining, remaining) {
		t.Errorf("expected remaining %v, %v", expectedRemaining, remaining)
	}
	if !reflect.DeepEqual(expected, result) {
		t.Errorf("expected result %v, got %v", expected, result)
	}
}

func TestParseVarLenExtendedBelow5(t *testing.T) {
	buf := []byte{
		0xf3,
		0x66, // extended tag
		1,
		2,
		3,
		4,
		5,
	}
	result, remaining, err := parseVarLen(15, buf)
	if err != nil {
		t.Fatal("did not expect an error")
	}
	expectedRemaining := []byte{4, 5}
	if !reflect.DeepEqual(expectedRemaining, remaining) {
		t.Errorf("expected remaining %v, %v", expectedRemaining, remaining)
	}
	expected := []byte{1, 2, 3}
	if !reflect.DeepEqual(expected, result) {
		t.Errorf("expected result %v, got %v", expected, result)
	}
}

func TestParseVarLenExtendedBelow254(t *testing.T) {
	dataLen := 42
	buf := []byte{
		0xf5,
		0x66, // extended tag
		byte(dataLen & 0xff),
	}
	expected := make([]byte, 42)
	for i := range 42 {
		expected[i] = byte(i)
	}
	buf = append(buf, expected...)

	expectedRemaining := []byte{4, 5}
	buf = append(buf, expectedRemaining...)

	result, remaining, err := parseVarLen(15, buf)
	if err != nil {
		t.Fatal("did not expect an error")
	}
	if !reflect.DeepEqual(expectedRemaining, remaining) {
		t.Errorf("expected remaining %v, %v", expectedRemaining, remaining)
	}
	if !reflect.DeepEqual(expected, result) {
		t.Errorf("expected result %v, got %v", expected, result)
	}
}

func TestParseVarLenExtendedBelow65536(t *testing.T) {
	dataLen := 4087
	buf := []byte{
		0xf5,
		0x66, // extended tag
		254,  // length is coded on two bytes
		uint8((dataLen & 0xff00) >> 8),
		uint8(dataLen & 0xff),
	}
	expected := make([]byte, dataLen)
	for i := range dataLen {
		expected[i] = byte(i)
	}
	buf = append(buf, expected...)

	expectedRemaining := []byte{4, 5}
	buf = append(buf, expectedRemaining...)

	result, remaining, err := parseVarLen(15, buf)
	if err != nil {
		t.Fatal("did not expect an error")
	}
	if !reflect.DeepEqual(expectedRemaining, remaining) {
		t.Errorf("expected remaining %v, %v", expectedRemaining, remaining)
	}
	if !reflect.DeepEqual(expected, result) {
		t.Errorf("expected result %v, got %v", expected, result)
	}
}

func TestParseVarLenExtended65536AndAbove(t *testing.T) {
	dataLen := 1284325
	buf := []byte{
		0xf5,
		0x66, // extended tag
		255,  // length is coded on four bytes
		uint8((dataLen & 0xff000000) >> 24),
		uint8((dataLen & 0x00ff0000) >> 16),
		uint8((dataLen & 0x0000ff00) >> 8),
		uint8(dataLen & 0x000000ff),
	}
	expected := make([]byte, dataLen)
	for i := range dataLen {
		expected[i] = byte(i)
	}
	buf = append(buf, expected...)

	expectedRemaining := []byte{4, 5}
	buf = append(buf, expectedRemaining...)

	result, remaining, err := parseVarLen(15, buf)
	if err != nil {
		t.Fatal("did not expect an error")
	}
	if !reflect.DeepEqual(expectedRemaining, remaining) {
		t.Errorf("expected remaining %v, %v", expectedRemaining, remaining)
	}
	if !reflect.DeepEqual(expected, result) {
		t.Errorf("expected result %v, got %v", expected, result)
	}
}

func TestUnsigned16MarshalPrimitive(t *testing.T) {
	u := Unsigned16{value: 0x1234}

	// tag: application (0b0), number: 2 (unsigned int), len: 2
	expected := []byte{0x22, 0x12, 0x34}

	result, err := u.MarshalPrimitive()
	if err != nil {
		t.Fatalf("did not expect error: %v", err)
	}
	if !reflect.DeepEqual(expected, result) {
		t.Errorf("expected %v, got %v", expected, result)
	}

	u = Unsigned16{value: 0x0042}

	// leading zero byte is stripped, len: 1
	expected = []byte{0x21, 0x42}

	result, err = u.MarshalPrimitive()
	if err != nil {
		t.Fatalf("did not expect error: %v", err)
	}
	if !reflect.DeepEqual(expected, result) {
		t.Errorf("expected %v, got %v", expected, result)
	}

	u = Unsigned16{value: 0x0000}

	// zero value: must still encode as 1 byte per BACnet standard
	expected = []byte{0x21, 0x00}

	result, err = u.MarshalPrimitive()
	if err != nil {
		t.Fatalf("did not expect error: %v", err)
	}
	if !reflect.DeepEqual(expected, result) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestUnsignedMarshalPrimitive(t *testing.T) {
	u := Unsigned{
		value: uint64(0x1122334455667788),
	}

	// tag: 0b0010
	// class: 0b0
	// len: 0b101 + next byte: 0x8
	expected := []byte{
		0x25,
		0x8,
		0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
	}

	result, err := u.MarshalPrimitive()
	if err != nil {
		t.Fatalf("did not expect error: %v", err)
	}

	if !reflect.DeepEqual(expected, result) {
		t.Errorf("expected %v, got %v", expected, result)
	}

	u = Unsigned{
		value: uint64(0x22334455667788),
	}
	// tag: 0b0010
	// class: 0b0
	// len: 0b101 + next byte: 0x7
	expected = []byte{
		0x25,
		0x7,
		0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
	}

	result, err = u.MarshalPrimitive()
	if err != nil {
		t.Fatalf("did not expect error: %v", err)
	}

	if !reflect.DeepEqual(expected, result) {
		t.Errorf("expected %v, got %v", expected, result)
	}

	u = Unsigned{
		value: uint64(0x4455667788),
	}
	// tag: 0b0010
	// class: 0b0
	// len: 0b101 + next byte: 0x5
	expected = []byte{
		0x25,
		0x5,
		0x44, 0x55, 0x66, 0x77, 0x88,
	}

	result, err = u.MarshalPrimitive()
	if err != nil {
		t.Fatalf("did not expect error: %v", err)
	}

	if !reflect.DeepEqual(expected, result) {
		t.Errorf("expected %v, got %v", expected, result)
	}

	u = Unsigned{
		value: uint64(0x55667788),
	}
	// tag: 0b0010
	// class: 0b0
	// len: 0b100
	expected = []byte{
		0x24,
		0x55, 0x66, 0x77, 0x88,
	}

	result, err = u.MarshalPrimitive()
	if err != nil {
		t.Fatalf("did not expect error: %v", err)
	}

	if !reflect.DeepEqual(expected, result) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}
