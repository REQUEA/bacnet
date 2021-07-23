package main

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"

	"github.com/alexbeltran/gobacnet/types"
)

type Client struct {
	ipAdress         net.IP
	broadcastAddress net.IP
	udpPort          int
	udp              *net.UDPConn
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
	c.udp = conn
	go c.listen()
	return c, nil
}

// listen for incoming bacnet packets.
func (c *Client) listen() {
	//Todo: allow close client
	for {
		b := make([]byte, 2048)
		i, addr, err := c.udp.ReadFromUDP(b)
		if err != nil {
			//Todo; do better, use logger
			panic(err)
		}
		go func() {
			err := c.handleMessage(addr, b[:i])
			if err != nil {
				panic(err)
			}
		}()
	}
}

func (c *Client) handleMessage(src *net.UDPAddr, b []byte) error {
	fmt.Printf("Received packet %s from addr %v \n", hex.EncodeToString(b), src)
	var bvlc BVLC
	err := bvlc.UnmarshalBinary(b)
	if err != nil {
		return err
	}
	fmt.Printf("%+v\n", bvlc)
	if bvlc.NPDU.Destination != nil {
		fmt.Printf("dest: %+v\n", *bvlc.NPDU.Destination)
	}
	if bvlc.NPDU.ADPU != nil {
		fmt.Printf("apdu %+v\n", *bvlc.NPDU.ADPU)
	}
	return nil
}

func (c *Client) WhoIs(data WhoIs) ([]types.Device, error) {
	npdu := NPDU{
		Version:               BacnetVersion1,
		IsNetworkLayerMessage: false,
		ExpectingReply:        false,
		Priority:              Normal,
		Destination:           nil,
		Source:                nil,
		ADPU: &APDU{
			DataType:    UnconfirmedServiceRequest,
			ServiceType: ServiceUnconfirmedWhoIs,
			Payload:     &data,
		},
	}

	_, err := c.broadcast(npdu)
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func (c *Client) send(dest types.Address, data []byte) (int, error) {
	return 0, nil
	// var header types.BVLC

	// // Set packet type
	// header.Type = types.BVLCTypeBacnetIP

	// if dest.IsBroadcast() || dest.IsSubBroadcast() {
	// 	// SET BROADCAST FLAG
	// 	header.Function = types.BacFuncBroadcast
	// } else {
	// 	// SET UNICAST FLAG
	// 	header.Function = types.BacFuncUnicast
	// }
	// mtuHeaderLength := 4
	// header.Length = uint16(mtuHeaderLength + len(data))
	// header.Data = data
	// e := encoding.NewEncoder()
	// err := e.BVLC(header)
	// if err != nil {
	// 	return 0, err
	// }

	// // Get IP Address
	// d, err := dest.UDPAddr()
	// if err != nil {
	// 	return 0, err
	// }

	// // use default udp type, src = local address (nil)
	// return c.listener.WriteTo(e.Bytes(), &d)
}

func (c *Client) broadcast(npdu NPDU) (int, error) {
	bytes, err := BVLC{
		Type:     BVLCTypeBacnetIP,
		Function: BacFuncBroadcast,
		NPDU:     npdu,
	}.MarshalBinary()
	if err != nil {
		return 0, err
	}
	fmt.Println("Broadcast")
	return c.udp.WriteToUDP(bytes, &net.UDPAddr{
		IP:   c.broadcastAddress,
		Port: DefaultUDPPort,
	})
}
