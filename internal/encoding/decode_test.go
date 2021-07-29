package encoding

import (
	"bacnet/internal/types"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"

	"github.com/matryer/is"
)

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
		expected interface{}
	}{
		{
			data: "c4020075e9",
			expected: types.ObjectID{
				Type:     8,
				Instance: 30185,
			},
		},
		{
			data:     "2205c4",
			expected: uint32(1476),
		},
		{
			data:     "9100",
			expected: types.SegmentationSupportBoth,
		},
		{
			data:     "22016c",
			expected: uint32(364),
		},
		{
			data:     "7511004543592d53313030302d413437383035",
			expected: "ECY-S1000-A47805",
		},
	}
	for _, tc := range ttc {
		t.Run(fmt.Sprintf("AppData %s (%T)", tc.data, tc.expected), func(t *testing.T) {
			is := is.New(t)
			b, err := hex.DecodeString(tc.data)
			is.NoErr(err)
			decoder := NewDecoder(b)
			//Ensure that it work when passed the concrete type
			switch tc.expected.(type) {
			case types.ObjectID:
				x := types.ObjectID{}
				decoder.AppData(&x)
				is.NoErr(decoder.err)
				is.Equal(x, tc.expected)
			case uint32:
				var x uint32
				decoder.AppData(&x)
				is.NoErr(decoder.err)
				is.Equal(x, tc.expected)
			case types.SegmentationSupport:
				var x types.SegmentationSupport
				decoder.AppData(&x)
				is.NoErr(decoder.err)
				is.Equal(x, tc.expected)
			case string:
				var x string
				decoder.AppData(&x)
				is.NoErr(decoder.err)
				is.Equal(x, tc.expected)
			default:
				t.Errorf("Invalid from type %T", tc.expected)
			}
			//Ensure that it work when passed an empty interface
			var v interface{}
			decoder = NewDecoder(b)
			decoder.AppData(&v)
			is.NoErr(decoder.err)
			is.Equal(v, tc.expected)

		})
	}
}
