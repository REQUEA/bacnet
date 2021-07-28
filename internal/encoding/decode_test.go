package encoding

import (
	"bacnet/internal/types"
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"

	"github.com/matryer/is"
)

func TestDecodeTag(t *testing.T) {
	ttc := []struct {
		data     string //hex string
		expected tag
	}{
		{
			data: "09",
			expected: tag{
				ID:      0,
				Value:   1,
				Context: true,
			},
		},
		{
			data: "1a",
			expected: tag{
				ID:      1,
				Value:   2,
				Context: true,
			},
		},
		{
			data: "c4",
			expected: tag{
				ID:    12,
				Value: 4,
			},
		},
		{
			data: "22",
			expected: tag{
				ID:    2,
				Value: 2,
			},
		},
		{
			data: "91",
			expected: tag{
				ID:    9,
				Value: 1,
			},
		},
	}
	for _, tc := range ttc {
		t.Run(fmt.Sprintf("Tag %s", tc.data), func(t *testing.T) {
			is := is.New(t)
			b, err := hex.DecodeString(tc.data)
			is.NoErr(err)
			buf := bytes.NewBuffer(b)
			_, tag, err := decodeTag(buf)
			is.NoErr(err)
			is.Equal(tag, tc.expected)
		})
	}
}
func TestDecodeTagWithFailure(t *testing.T) {
	data := []byte{0x39, 0x42}
	d := NewDecoder(data)
	var val uint32
	d.ContextValue(2, &val)
	var e ErrorIncorrectTag
	if d.Error() == nil || !errors.As(d.Error(), &e) {
		t.Fatal("Error should be set as ErrorIncorectTag: ", d.Error())
	}
	d.ResetError()
	d.ContextValue(3, &val)
	if d.Error() != nil {
		t.Fatal("Unexpected error: ", d.Error())
	}
	if val != 0x42 {
		t.Fatal("Wrong value")
	}
}

func TestDecodeAppData(t *testing.T) {
	ttc := []struct {
		data     string //hex string
		from     interface{}
		expected interface{}
	}{
		{
			data: "c4020075e9",
			from: types.ObjectID{},
			expected: types.ObjectID{
				Type:     8,
				Instance: 30185,
			},
		},
		{
			data:     "2205c4",
			from:     uint32(0),
			expected: uint32(1476),
		},
		{
			data:     "9100",
			from:     types.SegmentationSupport(0),
			expected: types.SegmentationSupportBoth,
		},
		{
			data:     "22016c",
			from:     uint32(0),
			expected: uint32(364),
		},
	}
	for _, tc := range ttc {
		t.Run(fmt.Sprintf("AppData %s", tc.data), func(t *testing.T) {
			is := is.New(t)
			b, err := hex.DecodeString(tc.data)
			is.NoErr(err)
			decoder := NewDecoder(b)
			switch tc.from.(type) {
			case types.ObjectID:
				x := types.ObjectID{}
				decoder.DecodeAppData(&x)
				is.NoErr(decoder.err)
				is.Equal(x, tc.expected)
			case uint32:
				var x uint32
				decoder.DecodeAppData(&x)
				is.NoErr(decoder.err)
				is.Equal(x, tc.expected)
			case types.SegmentationSupport:
				var x types.SegmentationSupport
				decoder.DecodeAppData(&x)
				is.NoErr(decoder.err)
				is.Equal(x, tc.expected)
			default:
				t.Errorf("Invalid from type %T", tc.from)
			}

		})
	}
}
