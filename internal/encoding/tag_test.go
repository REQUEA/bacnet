package encoding

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/matryer/is"
)

func TestValidTag(t *testing.T) {
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
		{
			data: "3e",
			expected: tag{
				ID:      3,
				Context: true,
				Opening: true,
			},
		},
		{
			data: "3f",
			expected: tag{
				ID:      3,
				Context: true,
				Closing: true,
			},
		},
		{
			data: "7511",
			expected: tag{
				ID:    applicationTagCharacterString,
				Value: 17,
			},
		},
	}
	for _, tc := range ttc {
		t.Run(fmt.Sprintf("Tag decode %s", tc.data), func(t *testing.T) {
			is := is.New(t)
			b, err := hex.DecodeString(tc.data)
			is.NoErr(err)
			buf := bytes.NewBuffer(b)
			_, tag, err := decodeTag(buf)
			is.NoErr(err)
			is.Equal(tag, tc.expected)
		})
		t.Run(fmt.Sprintf("Tag encode %s", tc.data), func(t *testing.T) {
			is := is.New(t)
			buf := &bytes.Buffer{}
			is.NoErr(encodeTag(buf, tc.expected))
			is.Equal(hex.EncodeToString(buf.Bytes()), tc.data)
		})
	}
}
