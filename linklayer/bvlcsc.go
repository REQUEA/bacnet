// Package linklayer implements BACnet link-layer protocols including
// BACnet/SC (Secure Connect) per ASHRAE 135-2024 Annex AB.
package linklayer

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/REQUEA/bacnet"
)

// BVMAC is a 6-byte BACnet/SC Virtual MAC Address.
type BVMAC [6]byte

// BroadcastVMAC is the all-ones broadcast address used in BACnet/SC (FF:FF:FF:FF:FF:FF).
var BroadcastVMAC = BVMAC{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

// NewRandomVMAC generates a cryptographically random VMAC.
func NewRandomVMAC() (BVMAC, error) {
	var v BVMAC
	if _, err := rand.Read(v[:]); err != nil {
		return v, err
	}
	// Ensure it is not the broadcast (all-ones) address.
	for v == BroadcastVMAC {
		if _, err := rand.Read(v[:]); err != nil {
			return v, err
		}
	}
	return v, nil
}

// GetBytes implements bacnet.MAC.
func (v *BVMAC) GetBytes() []byte {
	b := make([]byte, 6)
	copy(b, v[:])
	return b
}

// FromBytes implements bacnet.MAC.
func (v *BVMAC) FromBytes(b []byte) {
	copy(v[:], b)
}

// String implements bacnet.MAC.
func (v *BVMAC) String() string {
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
		v[0], v[1], v[2], v[3], v[4], v[5])
}

// IsBroadcast implements bacnet.MAC; returns true if all bytes are zero.
func (v *BVMAC) IsBroadcast() bool {
	return *v == BroadcastVMAC
}

// Equal implements bacnet.MAC.
func (v *BVMAC) Equal(other bacnet.MAC) bool {
	o, ok := other.(*BVMAC)
	if !ok {
		return false
	}
	return *v == *o
}

// BVLCSCFunction identifies the BVLC-SC message type (Annex AB Table AB-1).
type BVLCSCFunction uint8

const (
	BVLCSCFuncResult                    BVLCSCFunction = 0x00
	BVLCSCFuncEncapsulatedNPDU          BVLCSCFunction = 0x01
	BVLCSCFuncAddressResolution         BVLCSCFunction = 0x02
	BVLCSCFuncAddressResolutionACK      BVLCSCFunction = 0x03
	BVLCSCFuncAdvertisement             BVLCSCFunction = 0x04
	BVLCSCFuncAdvertisementSolicitation BVLCSCFunction = 0x05
	BVLCSCFuncConnectRequest            BVLCSCFunction = 0x06
	BVLCSCFuncConnectAccept             BVLCSCFunction = 0x07
	BVLCSCFuncDisconnectRequest         BVLCSCFunction = 0x08
	BVLCSCFuncDisconnectACK             BVLCSCFunction = 0x09
	BVLCSCFuncHeartbeatRequest          BVLCSCFunction = 0x0A
	BVLCSCFuncHeartbeatACK              BVLCSCFunction = 0x0B
	BVLCSCFuncProprietaryMessage        BVLCSCFunction = 0x0C
)

// BVLCSCControlFlags are the 8-bit control flags in the BVLC-SC header.
type BVLCSCControlFlags uint8

const (
	ControlOriginVMACPresent  BVLCSCControlFlags = 1 << 3
	ControlDestVMACPresent    BVLCSCControlFlags = 1 << 2
	ControlDestOptionsPresent BVLCSCControlFlags = 1 << 1
	ControlDataOptionsPresent BVLCSCControlFlags = 1
)

// ---------------------------------------------------------------------------
// Option types (Annex AB Table AB-5)
// ---------------------------------------------------------------------------

// BVLCSCOptionType identifies the type of a BVLC-SC header option.
type BVLCSCOptionType uint8

const (
	OptionTypeSecurePath  BVLCSCOptionType = 0x01
	OptionTypeHello       BVLCSCOptionType = 0x02
	OptionTypeIdentity    BVLCSCOptionType = 0x03
	OptionTypeHint        BVLCSCOptionType = 0x04
	OptionTypeToken       BVLCSCOptionType = 0x05
	OptionTypeProprietary BVLCSCOptionType = 0x1F
)

type BVLCSCHeaderMarker struct {
	MoreOptions    bool
	MustUnderstand bool
	DataFlag       bool
	OptionType     uint8
}

const (
	HeaderMarkerMoreOptions    uint8 = 1 << 7
	HeaderMarkerMustUnderstand uint8 = 1 << 6
	HeaderMarkerDataFlag       uint8 = 1 << 5
	HeaderOptionTypeMask       uint8 = 0x1F
)

type BVLCSCOption interface {
	Marshal() []byte
	Unmarshal(b []byte) ([]byte, error)
}

type BVLCSCOptionBase struct {
	HeaderMarker BVLCSCHeaderMarker
	HeaderLength uint16
}

func (o *BVLCSCOptionBase) Marshal() []byte {
	markerByte := uint8(0)
	if o.HeaderMarker.MoreOptions {
		markerByte |= HeaderMarkerMoreOptions
	}
	if o.HeaderMarker.MustUnderstand {
		markerByte |= HeaderMarkerMustUnderstand
	}
	markerByte |= (o.HeaderMarker.OptionType & HeaderOptionTypeMask)
	if o.HeaderMarker.DataFlag {
		markerByte |= HeaderMarkerDataFlag
		return []byte{
			markerByte, byte(o.HeaderLength >> 8), byte(o.HeaderLength & 0xff),
		}
	}
	return []byte{markerByte}
}

func (o *BVLCSCOptionBase) Umarshal(b []byte) ([]byte, error) {
	if b[0]&HeaderMarkerMoreOptions != 0 {
		o.HeaderMarker.MoreOptions = true
	} else {
		o.HeaderMarker.MoreOptions = false
	}
	if b[0]&HeaderMarkerMustUnderstand != 0 {
		o.HeaderMarker.MustUnderstand = true
	} else {
		o.HeaderMarker.MustUnderstand = false
	}
	if b[0]&HeaderMarkerDataFlag != 0 {
		o.HeaderMarker.DataFlag = true
	} else {
		o.HeaderMarker.DataFlag = false
	}
	if o.HeaderMarker.DataFlag {
		o.HeaderLength = uint16(b[1])<<8 | uint16(b[2])
		return b[3:], nil
	} else {
		o.HeaderLength = 0
		return b[1:], nil
	}
}

// SecurePathOption carries no data (Header-Length = 0).
type SecurePathOption struct {
	BVLCSCOptionBase
}

// Unmarshal implements BVLCSCOption.
func (o *SecurePathOption) Unmarshal(b []byte) ([]byte, error) {
	return o.BVLCSCOptionBase.Umarshal(b)
}

// RawOption holds raw bytes for an unrecognised option type.
type RawOption struct {
	BVLCSCOptionBase
	Data []byte
}

func (o *RawOption) Marshal() []byte {
	commonData := o.BVLCSCOptionBase.Marshal()
	return append(commonData, o.Data...)
}

func (o *RawOption) Unmarshal(b []byte) ([]byte, error) {
	remaining, err := o.BVLCSCOptionBase.Umarshal(b)
	if err != nil {
		return remaining, err
	}
	o.Data = append([]byte(nil), remaining[:o.HeaderLength]...)
	return remaining[o.HeaderLength:], nil
}

// HelloOption carries opaque bytes.
type HelloOption struct {
	BVLCSCOptionBase
	Capabilities uint8
}

func (o *HelloOption) Marshal() []byte {
	commonBytes := o.BVLCSCOptionBase.Marshal()
	return append(commonBytes, o.Capabilities)
}

func (o *HelloOption) Unmarshal(b []byte) ([]byte, error) {
	remaining, err := o.BVLCSCOptionBase.Umarshal(b)
	if err != nil {
		return remaining, err
	}
	if o.BVLCSCOptionBase.HeaderLength != 1 {
		return remaining, fmt.Errorf("wrong size for Hello Capabilities")
	}
	o.Capabilities = remaining[0]
	return remaining[1:], nil
}

// IdentityOption carries opaque bytes.
type IdentityOption struct {
	BVLCSCOptionBase
	DeviceInstance uint32
}

func (o *IdentityOption) Marshal() []byte {
	commonData := o.BVLCSCOptionBase.Marshal()
	devInstanceData := []byte{
		byte((o.DeviceInstance >> 16) & 0xff),
		byte((o.DeviceInstance >> 8) & 0xff),
		byte(o.DeviceInstance & 0xff),
	}
	return append(commonData, devInstanceData...)
}
func (o *IdentityOption) Unmarshal(b []byte) ([]byte, error) {
	remaining, err := o.BVLCSCOptionBase.Umarshal(b)
	if err != nil {
		return remaining, err
	}
	if o.BVLCSCOptionBase.HeaderLength != 3 {
		return remaining, fmt.Errorf("wrong size for Identity Device Instance")
	}
	o.DeviceInstance = uint32(remaining[0])<<16 | uint32(remaining[1])<<8 | uint32(remaining[2])
	return remaining[3:], nil
}

// HintOption carries opaque bytes.
type HintOption struct {
	BVLCSCOptionBase
	Scope []byte
}

func (o *HintOption) Marshal() []byte {
	commonData := o.BVLCSCOptionBase.Marshal()
	return append(commonData, o.Scope...)
}

func (o *HintOption) Unmarshal(b []byte) ([]byte, error) {
	remaining, err := o.BVLCSCOptionBase.Umarshal(b)
	if err != nil {
		return remaining, err
	}
	o.Scope = remaining[:o.HeaderLength]
	return remaining[o.HeaderLength:], nil
}

// TokenOption carries opaque bytes.
type TokenOption struct {
	BVLCSCOptionBase
	Token []byte
}

func (o *TokenOption) Marshal() []byte {
	commonData := o.BVLCSCOptionBase.Marshal()
	return append(commonData, o.Token...)
}

func (o *TokenOption) Unmarshal(b []byte) ([]byte, error) {
	remaining, err := o.BVLCSCOptionBase.Umarshal(b)
	if err != nil {
		return remaining, err
	}
	o.Token = remaining[:o.HeaderLength]
	return remaining[o.HeaderLength:], nil
}

// ProprietaryOption carries a VendorID, ProprietaryType, and opaque data.
type ProprietaryOption struct {
	BVLCSCOptionBase
	VendorID        uint16
	ProprietaryType uint8
	Data            []byte
}

func (o *ProprietaryOption) Marshal() []byte {
	out := o.BVLCSCOptionBase.Marshal()
	out = binary.BigEndian.AppendUint16(out, o.VendorID)
	out = append(out, o.ProprietaryType)
	out = append(out, o.Data...)
	return out
}

func (o *ProprietaryOption) Unmarshal(b []byte) ([]byte, error) {
	remaining, err := o.BVLCSCOptionBase.Umarshal(b)
	if err != nil {
		return remaining, err
	}
	if len(remaining) < 3 {
		return remaining, fmt.Errorf("bvlcsc: proprietary option too short")
	}
	o.VendorID = binary.BigEndian.Uint16(remaining[:2])
	o.ProprietaryType = remaining[2]
	o.Data = append([]byte(nil), remaining[3:o.HeaderLength]...)
	return remaining[o.HeaderLength:], nil
}

// newOptionFromBytes creates a typed BVLCSCOption from the option type and
// payload bytes (marker and length already consumed by the caller).
func newOptionFromBytes(t BVLCSCOptionType, data []byte) (BVLCSCOption, error) {
	base := BVLCSCOptionBase{
		HeaderMarker: BVLCSCHeaderMarker{
			OptionType: uint8(t),
			DataFlag:   len(data) > 0,
		},
		HeaderLength: uint16(len(data)), //nolint:gosec
	}
	switch t {
	case OptionTypeSecurePath:
		return &SecurePathOption{BVLCSCOptionBase: base}, nil
	case OptionTypeHello:
		if len(data) < 1 {
			return nil, fmt.Errorf("bvlcsc: hello option too short")
		}
		return &HelloOption{BVLCSCOptionBase: base, Capabilities: data[0]}, nil
	case OptionTypeIdentity:
		if len(data) < 3 {
			return nil, fmt.Errorf("bvlcsc: identity option too short")
		}
		return &IdentityOption{
			BVLCSCOptionBase: base,
			DeviceInstance:   uint32(data[0])<<16 | uint32(data[1])<<8 | uint32(data[2]),
		}, nil
	case OptionTypeHint:
		return &HintOption{BVLCSCOptionBase: base, Scope: append([]byte(nil), data...)}, nil
	case OptionTypeToken:
		return &TokenOption{BVLCSCOptionBase: base, Token: append([]byte(nil), data...)}, nil
	case OptionTypeProprietary:
		if len(data) < 3 {
			return nil, fmt.Errorf("bvlcsc: proprietary option too short")
		}
		return &ProprietaryOption{
			BVLCSCOptionBase: base,
			VendorID:         binary.BigEndian.Uint16(data[:2]),
			ProprietaryType:  data[2],
			Data:             append([]byte(nil), data[3:]...),
		}, nil
	default:
		return &RawOption{BVLCSCOptionBase: base, Data: append([]byte(nil), data...)}, nil
	}
}

// marshalOptionList serialises a []BVLCSCOption, auto-setting the MoreOptions
// bit in the first byte of every entry except the last.
func marshalOptionList(opts []BVLCSCOption) []byte {
	var out []byte
	for i, opt := range opts {
		entry := opt.Marshal()
		if i < len(opts)-1 {
			entry[0] |= HeaderMarkerMoreOptions
		} else {
			entry[0] &^= HeaderMarkerMoreOptions
		}
		out = append(out, entry...)
	}
	return out
}

// unmarshalOptionList parses a concatenated option list and returns the
// decoded options plus the total number of bytes consumed.
// Per Annex AB Table AB-5: if HeaderMarkerDataFlag is clear the entry is a
// single marker byte with no length or data fields.
func unmarshalOptionList(b []byte) ([]BVLCSCOption, int, error) {
	var opts []BVLCSCOption
	pos := 0
	for {
		if len(b[pos:]) < 1 {
			return nil, 0, fmt.Errorf("bvlcsc: option list truncated")
		}
		marker := b[pos]
		more := marker&HeaderMarkerMoreOptions != 0
		hasData := marker&HeaderMarkerDataFlag != 0
		optType := BVLCSCOptionType(marker & HeaderOptionTypeMask)
		pos++
		var dataLen int
		if hasData {
			if len(b[pos:]) < 2 {
				return nil, 0, fmt.Errorf("bvlcsc: option list truncated")
			}
			dataLen = int(binary.BigEndian.Uint16(b[pos : pos+2]))
			pos += 2
		}
		if len(b[pos:]) < dataLen {
			return nil, 0, fmt.Errorf("bvlcsc: option data truncated (type 0x%02X)", optType)
		}
		opt, err := newOptionFromBytes(optType, b[pos:pos+dataLen])
		if err != nil {
			return nil, 0, err
		}
		opts = append(opts, opt)
		pos += dataLen
		if !more {
			break
		}
	}
	return opts, pos, nil
}

// ---------------------------------------------------------------------------
// BVLCSCMessage
// ---------------------------------------------------------------------------

// BVLCSCMessage is a parsed BACnet/SC BVLC message.
type BVLCSCMessage struct {
	Function           BVLCSCFunction
	Control            BVLCSCControlFlags
	MessageID          uint16
	OriginVMAC         *BVMAC         // nil if not present
	DestVMAC           *BVMAC         // nil if not present
	DestinationOptions []BVLCSCOption // nil if not present
	DataOptions        []BVLCSCOption // nil if not present
	Payload            []byte         // nil if not present
}

// Marshal encodes the message to a byte slice.
func (m *BVLCSCMessage) Marshal() ([]byte, error) {
	control := m.Control
	if m.OriginVMAC != nil {
		control |= ControlOriginVMACPresent
	}
	if m.DestVMAC != nil {
		control |= ControlDestVMACPresent
	}
	if len(m.DestinationOptions) > 0 {
		control |= ControlDestOptionsPresent
	}
	if len(m.DataOptions) > 0 {
		control |= ControlDataOptionsPresent
	}

	out := make([]byte, 0, 4+6+6+len(m.Payload))
	out = append(out, byte(m.Function))
	out = append(out, byte(control))
	out = binary.BigEndian.AppendUint16(out, m.MessageID)
	if m.OriginVMAC != nil {
		out = append(out, m.OriginVMAC[:]...)
	}
	if m.DestVMAC != nil {
		out = append(out, m.DestVMAC[:]...)
	}
	if len(m.DestinationOptions) > 0 {
		out = append(out, marshalOptionList(m.DestinationOptions)...)
	}
	if len(m.DataOptions) > 0 {
		out = append(out, marshalOptionList(m.DataOptions)...)
	}
	if m.Payload != nil {
		out = append(out, m.Payload...)
	}
	return out, nil
}

// Unmarshal decodes a BVLC-SC message from b.
func (m *BVLCSCMessage) Unmarshal(b []byte) error {
	if len(b) < 4 {
		return fmt.Errorf("bvlcsc: message too short (%d bytes)", len(b))
	}
	m.Function = BVLCSCFunction(b[0])
	m.Control = BVLCSCControlFlags(b[1])
	m.MessageID = binary.BigEndian.Uint16(b[2:4])
	pos := 4

	if m.Control&ControlOriginVMACPresent != 0 {
		if len(b) < pos+6 {
			return fmt.Errorf("bvlcsc: truncated origin VMAC")
		}
		v := BVMAC{}
		copy(v[:], b[pos:pos+6])
		m.OriginVMAC = &v
		pos += 6
	}
	if m.Control&ControlDestVMACPresent != 0 {
		if len(b) < pos+6 {
			return fmt.Errorf("bvlcsc: truncated dest VMAC")
		}
		v := BVMAC{}
		copy(v[:], b[pos:pos+6])
		m.DestVMAC = &v
		pos += 6
	}
	if m.Control&ControlDestOptionsPresent != 0 {
		opts, n, err := unmarshalOptionList(b[pos:])
		if err != nil {
			return fmt.Errorf("bvlcsc: dest-options: %w", err)
		}
		m.DestinationOptions = opts
		pos += n
	}
	if m.Control&ControlDataOptionsPresent != 0 {
		opts, n, err := unmarshalOptionList(b[pos:])
		if err != nil {
			return fmt.Errorf("bvlcsc: data-options: %w", err)
		}
		m.DataOptions = opts
		pos += n
	}
	m.Payload = b[pos:]
	return nil
}

// ---------------------------------------------------------------------------
// Payload types
// ---------------------------------------------------------------------------

// BVLCResultPayload is the payload for BVLC-Result (0x00).
type BVLCResultPayload struct {
	Function          BVLCSCFunction
	ResultCode        uint8
	ErrorHeaderMarker uint8
	ErrorClass        uint16
	ErrorCode         uint16
	ErrorMsg          string
}

func (p *BVLCResultPayload) Marshal() []byte {
	out := make([]byte, 0, 2)
	out = append(out, byte(p.Function))
	out = append(out, p.ResultCode)
	if p.ResultCode == 1 {
		out = append(out, p.ErrorHeaderMarker)
		out = binary.BigEndian.AppendUint16(out, p.ErrorClass)
		out = binary.BigEndian.AppendUint16(out, p.ErrorCode)
		out = append(out, []byte(p.ErrorMsg)...)
	}
	return out
}

func (p *BVLCResultPayload) Unmarshal(b []byte) error {
	if len(b) < 3 {
		return fmt.Errorf("bvlc-result payload too short")
	}
	p.Function = BVLCSCFunction(b[0])
	p.ResultCode = b[1]
	if p.ResultCode == 1 {
		if len(b) < 7 {
			return fmt.Errorf("bvlc-result payload (NAK) too short")
		}
		p.ErrorHeaderMarker = b[2]
		p.ErrorClass = binary.BigEndian.Uint16(b[3:5])
		p.ErrorCode = binary.BigEndian.Uint16(b[5:7])
		p.ErrorMsg = string(b[7:])
	}
	return nil
}

// AddressResolutionPayload is the payload for Address-Resolution (0x02).
// TODO: see if we can remove this definition
type AddressResolutionPayload struct{}

func (p *AddressResolutionPayload) Marshal() []byte {
	return []byte{}
}

func (p *AddressResolutionPayload) Unmarshal(b []byte) error {
	return nil
}

// AddressResolutionACKPayload is the payload for Address-Resolution-ACK (0x03).
type AddressResolutionACKPayload struct {
	URIs []string
}

func (p *AddressResolutionACKPayload) Marshal() []byte {
	out := make([]byte, 0, 6)
	for i := 0; i < len(p.URIs)-1; i++ {
		uriBytes := []byte(p.URIs[i])
		out = append(out, uriBytes...)
		out = append(out, 0x20) // add a space character
	}
	if len(p.URIs) > 0 {
		// add the last one
		uriBytes := []byte(p.URIs[len(p.URIs)-1])
		out = append(out, uriBytes...)
	}
	return out
}

func (p *AddressResolutionACKPayload) Unmarshal(b []byte) error {
	p.URIs = strings.Split(string(b), " ")
	return nil
}

// HubConnectionState indicates the hub connection state of an advertising device.
type HubConnectionState uint8

const (
	HubConnectionNoHub     HubConnectionState = 0 // no hub connection
	HubConnectionConnected HubConnectionState = 1 // connected to primary hub
	HubConnectionFailover  HubConnectionState = 2 // connected to failover hub
)

// AdvertisementPayload is the payload for Advertisement (0x04).
// Per Annex AB Table AB-11: Hub-Connection-State(1) + Accept-Direct-Connections(1) +
// Max-BVLC-Length(2) + Max-NPDU-Length(2) = 6 bytes.
// The originating VMAC is carried in the BVLC-SC header, not in the Data field.
type AdvertisementPayload struct {
	HubConnectionState      HubConnectionState
	AcceptDirectConnections bool
	MaxBVLCLength           uint16
	MaxNPDULength           uint16
}

func (p *AdvertisementPayload) Marshal() []byte {
	out := make([]byte, 0, 6)
	out = append(out, byte(p.HubConnectionState))
	if p.AcceptDirectConnections {
		out = append(out, 1)
	} else {
		out = append(out, 0)
	}
	out = binary.BigEndian.AppendUint16(out, p.MaxBVLCLength)
	out = binary.BigEndian.AppendUint16(out, p.MaxNPDULength)
	return out
}

func (p *AdvertisementPayload) Unmarshal(b []byte) error {
	if len(b) < 6 {
		return fmt.Errorf("advertisement payload too short")
	}
	p.HubConnectionState = HubConnectionState(b[0])
	p.AcceptDirectConnections = b[1] != 0
	p.MaxBVLCLength = binary.BigEndian.Uint16(b[2:4])
	p.MaxNPDULength = binary.BigEndian.Uint16(b[4:6])
	return nil
}

// ConnectRequestPayload is the payload for Connect-Request (0x06).
// Per Annex AB Table AB-12: VMAC(6) + MaxBVLC(2) + MaxNPDU(2) + DeviceUUID(16) = 26 bytes.
type ConnectRequestPayload struct {
	VMAC          BVMAC
	DeviceUUID    [16]byte
	MaxBVLCLength uint16
	MaxNPDULength uint16
}

func (p *ConnectRequestPayload) Marshal() []byte {
	out := make([]byte, 0, 26)
	out = append(out, p.VMAC[:]...)
	out = append(out, p.DeviceUUID[:]...)
	out = binary.BigEndian.AppendUint16(out, p.MaxBVLCLength)
	out = binary.BigEndian.AppendUint16(out, p.MaxNPDULength)
	return out
}

func (p *ConnectRequestPayload) Unmarshal(b []byte) error {
	if len(b) < 26 {
		return fmt.Errorf("connect-request payload too short")
	}
	copy(p.VMAC[:], b[:6])
	copy(p.DeviceUUID[:], b[6:22])
	p.MaxBVLCLength = binary.BigEndian.Uint16(b[22:24])
	p.MaxNPDULength = binary.BigEndian.Uint16(b[24:26])
	return nil
}

// ConnectAcceptPayload is the payload for Connect-Accept (0x07).
// Per Annex AB Table AB-13: VMAC(6) + MaxBVLC(2) + MaxNPDU(2) + DeviceUUID(16) = 26 bytes.
type ConnectAcceptPayload struct {
	VMAC          BVMAC
	DeviceUUID    [16]byte
	MaxBVLCLength uint16
	MaxNPDULength uint16
}

func (p *ConnectAcceptPayload) Marshal() []byte {
	out := make([]byte, 0, 26)
	out = append(out, p.VMAC[:]...)
	out = append(out, p.DeviceUUID[:]...)
	out = binary.BigEndian.AppendUint16(out, p.MaxBVLCLength)
	out = binary.BigEndian.AppendUint16(out, p.MaxNPDULength)
	return out
}

func (p *ConnectAcceptPayload) Unmarshal(b []byte) error {
	if len(b) < 26 {
		return fmt.Errorf("connect-accept payload too short")
	}
	copy(p.VMAC[:], b[:6])
	copy(p.DeviceUUID[:], b[6:22])
	p.MaxBVLCLength = binary.BigEndian.Uint16(b[22:24])
	p.MaxNPDULength = binary.BigEndian.Uint16(b[24:26])
	return nil
}

type ProprietaryMessagePayload struct {
	VendorID            uint16
	ProprietaryFunction uint8
	ProprietaryData     []byte
}

func (p *ProprietaryMessagePayload) Marshal() []byte {
	out := make([]byte, 0, 3+len(p.ProprietaryData))
	out = binary.BigEndian.AppendUint16(out, p.VendorID)
	out = append(out, p.ProprietaryFunction)
	out = append(out, p.ProprietaryData...)
	return out
}

func (p *ProprietaryMessagePayload) Unmarshal(b []byte) error {
	if len(b) < 3 {
		return fmt.Errorf("proprietary-message payload too short")
	}
	p.VendorID = binary.BigEndian.Uint16(b[0:2])
	p.ProprietaryFunction = b[2]
	copy(p.ProprietaryData[:], b[3:])
	return nil
}
