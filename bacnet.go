package main

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
	"net"

	"github.com/alexbeltran/gobacnet/encoding"
	"github.com/alexbeltran/gobacnet/types"
)

type Client struct {
	ipAdress         net.IP
	broadcastAddress net.IP
	udpPort          int
	listener         *net.UDPConn
}

const DefaultUDPPort = 47808

func broadcastAddr(n *net.IPNet) (net.IP, error) { // works when the n is a prefix, otherwise...
	if n.IP.To4() == nil {
		return net.IP{}, errors.New("does not support IPv6 addresses.")
	}
	ip := make(net.IP, len(n.IP.To4()))
	binary.BigEndian.PutUint32(ip, binary.BigEndian.Uint32(n.IP.To4())|^binary.BigEndian.Uint32(net.IP(n.Mask).To4()))
	return ip, nil
}
func NewClient(inter string, port int) (*Client, error) {
	c := &Client{}
	i, err := net.InterfaceByName(inter)
	if err != nil {
		return nil, err
	}
	if port == 0 {
		port = DefaultUDPPort
	}
	c.udpPort = port
	addrs, err := i.Addrs()
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("interface %s has no addresses", inter)
	}
	for _, adr := range addrs {
		ip, ipnet, err := net.ParseCIDR(adr.String())
		if err != nil {
			return nil, err
		}
		// To4 is nil when type is ip6
		if ip.To4() != nil {
			broadcast, err := broadcastAddr(ipnet)
			if err != nil {
				return nil, err
			}
			c.ipAdress = ip
			c.broadcastAddress = broadcast
			break
		}
	}

	conn, err := net.ListenUDP("udp", &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: c.udpPort,
	})
	if err != nil {
		return nil, err
	}
	c.listener = conn
	go c.listen()
	return c, nil
}

// listen for incoming bacnet packets.
func (c *Client) listen() error {
	var err error = nil
	r := rand.Int()
	// While connection is opened
	for err == nil {
		var (
			adr net.Addr
			i   int
		)

		b := make([]byte, 2048)
		i, adr, err = c.listener.ReadFrom(b)
		if err != nil {
			panic(err)
		}
		fmt.Printf("Received packet %s from addr %v  %d\n", hex.EncodeToString(b[:i]), adr, r)
	}
	return nil
}

func (c *Client) WhoIs(low, high int) ([]types.Device, error) {
	dest := types.UDPToAddress(&net.UDPAddr{
		IP:   c.broadcastAddress,
		Port: DefaultUDPPort,
	})
	// src := types.UDPToAddress(&net.UDPAddr{
	// 	IP:   c.ipAdress,
	// 	Port: c.udpPort,
	// })

	dest.SetBroadcast(true)

	// enc := encoding.NewEncoder()
	// npdu := types.NPDU{
	// 	Version:               types.ProtocolVersion,
	// 	Destination:           &dest,
	// 	Source:                &src,
	// 	IsNetworkLayerMessage: false,

	// 	// We are not expecting a direct reply from a single destination
	// 	ExpectingReply: false,
	// 	Priority:       types.Normal,
	// 	HopCount:       types.DefaultHopCount,
	// }
	// enc.NPDU(npdu)

	// err := enc.WhoIs(int32(low), int32(high))
	// if err != nil {
	// 	return nil, err
	// }
	npdu := NPDU{
		Version:               BacnetVersion1,
		IsNetworkLayerMessage: false,
		ExpectingReply:        false,
		Priority:              Normal,
		Destination:           nil,
		Source:                nil,
		HopCount:              255,
		VendorID:              0,
		ADPU: &APDU{
			DataType:    UnconfirmedServiceRequest,
			ServiceType: ServiceUnconfirmedWhoIs,
			Payload:     &DataPayload{[]byte{0x09, 0x00, 0x1a, 0xff, 0xff}},
		},
	}
	bytes, err := npdu.MarshalBinary()
	if err != nil {
		return nil, err
	}
	_, err = c.send(dest, bytes)
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func (c *Client) send(dest types.Address, data []byte) (int, error) {
	var header types.BVLC

	// Set packet type
	header.Type = types.BVLCTypeBacnetIP

	if dest.IsBroadcast() || dest.IsSubBroadcast() {
		// SET BROADCAST FLAG
		header.Function = types.BacFuncBroadcast
	} else {
		// SET UNICAST FLAG
		header.Function = types.BacFuncUnicast
	}
	mtuHeaderLength := 4
	header.Length = uint16(mtuHeaderLength + len(data))
	header.Data = data
	e := encoding.NewEncoder()
	err := e.BVLC(header)
	if err != nil {
		return 0, err
	}

	// Get IP Address
	d, err := dest.UDPAddr()
	if err != nil {
		return 0, err
	}

	// use default udp type, src = local address (nil)
	return c.listener.WriteTo(e.Bytes(), &d)
}
