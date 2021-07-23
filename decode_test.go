package bacnet

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/matryer/is"
)

//TODO: check for context
func TestDecodeTag(t *testing.T) {
	is := is.New(t)
	ttc := []struct {
		data     string //hex string
		expected tag
	}{
		{
			data: "09",
			expected: tag{
				ID:    0,
				Value: 1,
			},
		},
		{
			data: "1a",
			expected: tag{
				ID:    1,
				Value: 2,
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
			b, err := hex.DecodeString(tc.data)
			is.NoErr(err)
			buf := bytes.NewBuffer(b)
			length, tag, err := decodeTag(buf)
			is.NoErr(err)
			is.Equal(length, len(b))
			if !tagEqual(tag, tc.expected) {
				t.Errorf("Tag differ: expected %+v got %+v", tc.expected, tag)
			}
		})
	}
}

func tagEqual(t1, t2 tag) bool {
	return t1.ID == t2.ID &&
		t1.Context == t2.Context &&
		t1.Value == t2.Value &&
		t1.Opening == t2.Opening &&
		t1.Closing == t2.Closing
}
