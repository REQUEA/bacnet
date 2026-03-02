package linklayer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/logger"
	"golang.org/x/net/ipv4"
)

type NPDUHandler interface {
	HandleNPDU(dadr bacnet.MAC, sadr bacnet.MAC, buf []byte) error
}

type DatalinkPort interface {
	Mac() bacnet.MAC
	BroadcastMac() bacnet.MAC
	Send(data []byte, destMAC []byte) error
	MaxPDULength() uint
}

type BACnetIPMAC struct {
	net.IP
	Port        int
	isBroadcast bool
}

func (m *BACnetIPMAC) GetBytes() []byte {
	result := &bytes.Buffer{}
	// TODO: check if this is also correct for IPv6
	result.Write(m.IP.To4())
	_ = binary.Write(result, binary.BigEndian, uint16(m.Port))
	return result.Bytes()
}

func (m *BACnetIPMAC) FromBytes(b []byte) {
	m.IP = net.IPv4(b[0], b[1], b[2], b[3])
	m.Port = int(b[4])<<8 | int(b[5])
}

func (m *BACnetIPMAC) String() string {
	return fmt.Sprintf("%v:%v", m.IP, m.Port)
}

func (m *BACnetIPMAC) IsBroadcast() bool {
	return m.isBroadcast
}

func (m *BACnetIPMAC) Equal(other bacnet.MAC) bool {
	o, ok := other.(*BACnetIPMAC)
	if !ok {
		return false
	}
	return m.IP.Equal(o.IP) && m.Port == o.Port
}

type BACnetIPPort struct {
	mac          *BACnetIPMAC
	broadcastMac *BACnetIPMAC
	datalink     *BACnetIPDatalink
	npduHandler  NPDUHandler
}

func NewBACnetIPPort(addr net.IP, prefixLen int, port int) *BACnetIPPort {
	mac := BACnetIPMAC{
		IP:   addr,
		Port: port,
	}
	broadcastMac := BACnetIPMAC{
		IP:          getBroadcastAddress(addr, prefixLen),
		Port:        port,
		isBroadcast: true,
	}
	return &BACnetIPPort{
		mac:          &mac,
		broadcastMac: &broadcastMac,
	}
}

func (p *BACnetIPPort) SetNPDUHandler(h NPDUHandler) {
	p.npduHandler = h
}

func (p *BACnetIPPort) SetDatalink(l *BACnetIPDatalink) {
	p.datalink = l
}

func (p *BACnetIPPort) Mac() bacnet.MAC {
	return p.mac
}

func (p *BACnetIPPort) BroadcastMac() bacnet.MAC {
	return p.broadcastMac
}

func (p *BACnetIPPort) Send(data []byte, destMAC []byte) error {
	return p.datalink.Send(data, destMAC)
}

// MaxPDULength returns the maximum APDU length for BACnet/IP (ASHRAE 135 Annex J).
func (p *BACnetIPPort) MaxPDULength() uint {
	return 1476
}

func getBroadcastAddress(ip net.IP, prefixLen int) net.IP {
	mask := net.CIDRMask(prefixLen, 32)
	result := make(net.IP, len(ip))
	for i := range ip {
		result[i] = ip[i] | ^mask[i]
	}
	return result
}

type BACnetIPDatalink struct {
	Conn  *net.UDPConn
	Ports []*BACnetIPPort
}

func (l *BACnetIPDatalink) AddPort(p *BACnetIPPort) {
	l.Ports = append(l.Ports, p)
}

func (l *BACnetIPDatalink) Start() error {
	logger.Trace("Starting datalink")
	if l.Conn == nil {
		return fmt.Errorf("UDP connection not configured")
	}
	if l.Ports == nil {
		return fmt.Errorf("no port is configured")
	}
	go l.listen()
	return nil
}

func (l *BACnetIPDatalink) getPortFromDest(dst net.IP) (*BACnetIPPort, bool) {
	for _, p := range l.Ports {
		bcastMac := p.BroadcastMac().(*BACnetIPMAC)
		if bcastMac.IP.String() == dst.String() {
			return p, true
		}
		mac := p.Mac().(*BACnetIPMAC)
		if mac.IP.String() == dst.String() {
			return p, false
		}
	}
	return nil, false
}

func (l *BACnetIPDatalink) listen() {
	p := ipv4.NewPacketConn(l.Conn)
	if err := p.SetControlMessage(ipv4.FlagSrc|ipv4.FlagInterface|ipv4.FlagDst, true); err != nil {
		panic(err)
	}
mainloop:
	for {
		buff := make([]byte, 4096)
		logger.Trace("Waiting for UDP")
		n, ctrlMsg, src, err := p.ReadFrom(buff)
		if err != nil {
			logger.Error("error reading from udp socket: ", err)
		}
		if n > 0 {
			logger.Trace("Read ", n, " bytes with dest ", ctrlMsg.Dst)
			bvlc := BVLC{}
			buf := buff[:n]
			payload, err := bvlc.UnmarshalBinary(buf)
			if err != nil {
				// log error
				logger.Error("could not unmarshal bvlc header: ", err)
				continue
			}
			port, isBroadcast := l.getPortFromDest(ctrlMsg.Dst)
			if port == nil {
				logger.Trace("could not find a port matching destination ", ctrlMsg.Dst)
				continue
			}
			srcAddr, err := net.ResolveUDPAddr("udp", src.String())
			sadr := BACnetIPMAC{
				IP:   srcAddr.IP,
				Port: srcAddr.Port,
			}
			if err != nil {
				logger.Error("could not parse source address: ", err)
				continue
			}
			for _, p := range l.Ports {
				macbytes := p.Mac().GetBytes()
				ipAddr := net.IP{macbytes[0], macbytes[1], macbytes[2], macbytes[3]}
				udpPort := int(macbytes[4])<<8 | int(macbytes[5])
				if sadr.IP.Equal(ipAddr) && sadr.Port == udpPort {
					logger.Trace("UDP listener: dropping own datagram")
					continue mainloop
				}
			}
			dadr := BACnetIPMAC{
				IP:          ctrlMsg.Dst,
				Port:        int(port.Mac().GetBytes()[4])<<8 | int(port.Mac().GetBytes()[5]),
				isBroadcast: isBroadcast,
			}
			switch bvlc.Function {
			case BacFuncUnicast:
				port.npduHandler.HandleNPDU(&dadr, &sadr, payload)
			case BacFuncBroadcast:
				// TODO: check it is really what has to be done
				port.npduHandler.HandleNPDU(&dadr, &sadr, payload)
			default:
				logger.Error("unsupported BVLC function ", bvlc.Function)
			}
		} else {
			logger.Trace("empty read on port")
		}
	}
}

func (l *BACnetIPDatalink) Send(data []byte, destMAC []byte) error {
	var function Function
	if destMAC != nil {
		function = BacFuncUnicast
	} else {
		function = BacFuncBroadcast
	}
	bvlc := BVLC{
		Type:     TypeBacnetIP,
		Function: function,
		data:     data,
	}
	b, err := bvlc.MarshalBinary()
	if err != nil {
		return fmt.Errorf("could not marshal BVLC: %w", err)
	}
	// TODO: adapt for IPv6
	dest := BACnetIPMAC{}
	dest.FromBytes(destMAC)
	logger.Trace("BACnetIP datalink: Send to ", dest.String())

	dst := net.UDPAddr{IP: dest.IP, Port: dest.Port}

	n, err := l.Conn.WriteToUDP(b, &dst)
	if err != nil {
		return fmt.Errorf("could not send message: %w", err)
	}
	if n < len(b) {
		return fmt.Errorf("short write on UDP socket")
	}
	return nil
}

//go:generate stringer -type=BVLCType
type BVLCType byte

const TypeBacnetIP BVLCType = 0x81

//go:generate stringer -type=Function
type Function byte

const (
	BacFuncResult                          Function = 0x00
	BacFuncWriteBroadcastDistributionTable Function = 0x01
	BacFuncBroadcastDistributionTable      Function = 0x02
	BacFuncBroadcastDistributionTableAck   Function = 0x03
	BacFuncForwardedNPDU                   Function = 0x04
	BacFuncUnicast                         Function = 0x0A
	BacFuncBroadcast                       Function = 0x0B
)

type BVLC struct {
	Type     BVLCType
	Function Function
	data     []byte
}

func (bvlc *BVLC) MarshalBinary() ([]byte, error) {
	b := &bytes.Buffer{}
	b.WriteByte(byte(bvlc.Type))
	b.WriteByte(byte(bvlc.Function))
	len := uint16(4 + len(bvlc.data)) //len includes Type,Function and itself
	_ = binary.Write(b, binary.BigEndian, len)
	b.Write(bvlc.data)
	return b.Bytes(), nil
}

var ErrNotBAcnetIP = errors.New("packet isn't a bacnet/IP payload ")

func (bvlc *BVLC) UnmarshalBinary(data []byte) ([]byte, error) {
	buf := bytes.NewBuffer(data)
	bvlcType, err := buf.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("read bvlc type: %w", err)
	}
	bvlc.Type = BVLCType(bvlcType)
	if bvlc.Type != TypeBacnetIP {
		return nil, ErrNotBAcnetIP
	}
	bvlcFunc, err := buf.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("read bvlc func: %w", err)
	}
	var length uint16
	err = binary.Read(buf, binary.BigEndian, &length)
	if err != nil {
		return nil, fmt.Errorf("read bvlc length: %w", err)
	}
	remaining := buf.Bytes()

	bvlc.Function = Function(bvlcFunc)
	if len(remaining) != int(length)-4 {
		return nil, fmt.Errorf("incoherent Length field in BVCL. Advertized payload size is %d, real size  %d", length-4, len(remaining))
	}
	return remaining, nil
}
