package bacnet

import (
	"encoding/hex"
	"testing"

	"github.com/matryer/is"
)

func TestWhoIsDec(t *testing.T) {
	is := is.New(t)
	data, err := hex.DecodeString("09001affff") //With range
	is.NoErr(err)
	w := &WhoIs{}
	err = w.UnmarshalBinary(data)
	is.NoErr(err)
	if w.Low == nil || *w.Low != 0 {
		t.Error("Invalid whois decoding of low range ")
	}
	if w.High == nil || *w.High != 0xFFFF {
		t.Error("Invalid whois decoding of high range ")
	}

	data, err = hex.DecodeString("09121b012345") //No range
	is.NoErr(err)
	w = &WhoIs{}
	err = w.UnmarshalBinary(data)
	if w.Low == nil || *w.Low != 0x12 {
		t.Error("Invalid whois decoding of low range ")
	}
	if w.High == nil || *w.High != 0x12345 {
		t.Error("Invalid whois decoding of high range ")
	}

	data, err = hex.DecodeString("") //No range
	is.NoErr(err)
	w = &WhoIs{}
	err = w.UnmarshalBinary(data)
	if w.High != nil || w.Low != nil {
		t.Error("Non nil range value")
	}
}
