package bacnet

import (
	"bacnet/internal/types"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

type Client struct {
	ipAdress         net.IP
	broadcastAddress net.IP
	udpPort          int
	udp              *net.UDPConn
	subscriptions    *Subscriptions
	transactions     *Transactions
}

type Subscriptions struct {
	sync.Mutex
	f func(BVLC, net.UDPAddr)
}

const DefaultUDPPort = 47808

func broadcastAddr(n *net.IPNet) (net.IP, error) {
	if n.IP.To4() == nil {
		return net.IP{}, errors.New("does not support IPv6 addresses")
	}
	ip := make(net.IP, len(n.IP.To4()))
	binary.BigEndian.PutUint32(ip, binary.BigEndian.Uint32(n.IP.To4())|^binary.BigEndian.Uint32(net.IP(n.Mask).To4()))
	return ip, nil
}
func NewClient(inter string, port int) (*Client, error) {
	c := &Client{subscriptions: &Subscriptions{}, transactions: NewTransactions()}
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
			//c.ipAdress = ip
			c.broadcastAddress = broadcast
			break
		}
	}

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{
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
		//Todo: Ensure this can never panic and bring down application
		go func() {
			err := c.handleMessage(addr, b[:i])
			if err != nil {
				panic(err)
			}
		}()
	}
}

func (c *Client) handleMessage(src *net.UDPAddr, b []byte) error {
	//fmt.Printf("Received packet %s from addr %v \n", hex.EncodeToString(b), src)
	var bvlc BVLC
	err := bvlc.UnmarshalBinary(b)
	if err != nil {
		return err
	}
	//Todo: not race safe here: lock
	if c.subscriptions.f != nil {
		c.subscriptions.f(bvlc, *src)
	}

	//Todo : check if nil
	if bvlc.NPDU.ADPU.DataType == ComplexAck {
		//todo: allow failure
		invokeID := bvlc.NPDU.ADPU.InvokeID
		ch, ok := c.transactions.GetTransaction(invokeID)
		if !ok {
			panic("no transaction found")
		}
		ch <- bvlc
	}
	return nil
}

type IamAddress struct {
	Iam
	Address Address
}

//should we return a device object ?
func (c *Client) WhoIs(data WhoIs, timeout time.Duration) ([]IamAddress, error) {
	npdu := NPDU{
		Version:               Version1,
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

	rChan := make(chan struct {
		bvlc BVLC
		src  net.UDPAddr
	})
	c.subscriptions.Lock()
	//TODO:  add errgroup ?, ensure all f are done and not blocked
	c.subscriptions.f = func(bvlc BVLC, src net.UDPAddr) {
		rChan <- struct {
			bvlc BVLC
			src  net.UDPAddr
		}{
			bvlc: bvlc,
			src:  src,
		}
	}
	c.subscriptions.Unlock()
	defer func() {
		c.subscriptions.f = nil
	}()
	_, err := c.broadcast(npdu)
	if err != nil {
		return nil, err
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	//Use a set to deduplicate results
	set := map[Iam]Address{}
	for {
		select {
		case <-timer.C:
			result := []IamAddress{}
			for iam, addr := range set {
				result = append(result, IamAddress{Iam: iam, Address: addr})
			}
			return result, nil
		case r := <-rChan:
			//clean/filter  network answers here
			apdu := r.bvlc.NPDU.ADPU
			if apdu != nil {
				if apdu.DataType == UnconfirmedServiceRequest &&
					apdu.ServiceType == ServiceUnconfirmedIAm {
					iam, ok := apdu.Payload.(*Iam)
					if !ok {
						return nil, fmt.Errorf("unexpected payload type %T", apdu.Payload)
					}
					addr := AddressFromUDP(r.src)
					set[*iam] = *addr
				}
			}
		}
	}
}

func (c *Client) ReadProperty(device IamAddress, property types.PropertyIdentifier) error {
	invokeID := c.transactions.GetID()
	defer c.transactions.FreeID(invokeID)
	npdu := NPDU{
		Version:               Version1,
		IsNetworkLayerMessage: false,
		ExpectingReply:        true,
		Priority:              Normal,
		Destination:           &device.Address,
		Source: AddressFromUDP(net.UDPAddr{
			IP:   c.ipAdress,
			Port: c.udpPort,
		}),
		HopCount: 255,
		ADPU: &APDU{
			DataType:    ConfirmedServiceRequest,
			ServiceType: ServiceConfirmedReadProperty,
			InvokeID:    invokeID,
			Payload: &ReadPropertyReq{
				ObjectID: device.ObjectID,
				Property: property,
			},
		},
	}
	//Todo: pass context to cancel
	rChan := make(chan BVLC)
	c.transactions.SetTransaction(invokeID, rChan)
	defer c.transactions.StopTransaction(invokeID)
	_, err := c.send(npdu)
	if err != nil {
		return err
	}
	bvlc := <-rChan
	fmt.Printf("Got answer: %+v\n", bvlc.NPDU.ADPU.Payload)
	return nil
}

//Todo: unify these two by observing dest addr ?
func (c *Client) send(npdu NPDU) (int, error) {
	bytes, err := BVLC{
		Type:     TypeBacnetIP,
		Function: BacFuncUnicast,
		NPDU:     npdu,
	}.MarshalBinary()
	if err != nil {
		return 0, err
	}
	if npdu.Destination == nil {
		return 0, fmt.Errorf("destination bacnet address should be not nil to send unicast")
	}
	addr := UDPFromAddress(*npdu.Destination)
	return c.udp.WriteToUDP(bytes, &addr)

}

func (c *Client) broadcast(npdu NPDU) (int, error) {
	bytes, err := BVLC{
		Type:     TypeBacnetIP,
		Function: BacFuncBroadcast,
		NPDU:     npdu,
	}.MarshalBinary()
	if err != nil {
		return 0, err
	}
	return c.udp.WriteToUDP(bytes, &net.UDPAddr{
		IP:   c.broadcastAddress,
		Port: DefaultUDPPort,
	})
}
