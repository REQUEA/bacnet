package bacnet

import (
	"encoding/hex"
	"testing"

	"github.com/matryer/is"
)

func TestFullEncodingAndCoherency(t *testing.T) {
	ttc := []struct {
		bvlc    BVLC
		encoded string //hex string
	}{
		{
			bvlc: BVLC{
				Type:     TypeBacnetIP,
				Function: BacFuncBroadcast,
				NPDU: NPDU{
					Version:               Version1,
					IsNetworkLayerMessage: false,
					ExpectingReply:        false,
					Priority:              Normal,
					ADPU: &APDU{
						DataType:    UnconfirmedServiceRequest,
						ServiceType: ServiceUnconfirmedWhoIs,
						Payload:     &WhoIs{},
					},
				},
			},
			encoded: "810b000801001008",
		},
		{
			bvlc: BVLC{
				Type:     TypeBacnetIP,
				Function: BacFuncBroadcast,
				NPDU: NPDU{
					Version:               Version1,
					IsNetworkLayerMessage: false,
					ExpectingReply:        false,
					Priority:              Normal,
					Destination: &Address{
						Net: 0xffff,
						Len: 0,
						Adr: []byte{},
					},
					Source:   &Address{},
					HopCount: 255,
					ADPU: &APDU{
						DataType:    UnconfirmedServiceRequest,
						ServiceType: ServiceUnconfirmedIAm,
						Payload: &Iam{
							ObjectID: ObjectID{
								Type:     8,
								Instance: 30185,
							},
							MaxApduLength:       1476,
							SegmentationSupport: SegmentationSupportBoth,
							VendorID:            364,
						},
					},
				},
			},
			encoded: "810b00190120ffff00ff1000c4020075e92205c4910022016c",
		},
	}

	for _, tc := range ttc {
		t.Run(tc.encoded, func(t *testing.T) {
			is := is.New(t)
			result, err := tc.bvlc.MarshalBinary()
			is.NoErr(err)
			is.Equal(tc.encoded, hex.EncodeToString(result))
			w := BVLC{}
			is.NoErr(w.UnmarshalBinary(result))
			result2, err := w.MarshalBinary()
			is.NoErr(err)
			is.Equal(tc.encoded, hex.EncodeToString(result2))
		})
	}
}