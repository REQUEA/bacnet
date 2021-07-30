package bacnet

import (
	"bacnet/types"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

type Client struct {
	//Maybe change to bacnet address
	ipAdress         net.IP
	broadcastAddress net.IP
	udpPort          int
	udp              *net.UDPConn
	subscriptions    *Subscriptions
	transactions     *Transactions
	Logger           Logger
}

type Logger interface {
	Info(...interface{})
	Error(...interface{})
}

type NoOpLogger struct{}

func (NoOpLogger) Info(...interface{})  {}
func (NoOpLogger) Error(...interface{}) {}

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

//NewClient creates a new bacnet client. It binds on the given port
//and network interface (eth0 for example). If Port if 0, the default
//bacnet port is used
func NewClient(netInterface string, port int) (*Client, error) {
	c := &Client{subscriptions: &Subscriptions{}, transactions: NewTransactions(), Logger: NoOpLogger{}}
	i, err := net.InterfaceByName(netInterface)
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
		return nil, fmt.Errorf("interface %s has no addresses", netInterface)
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
	if c.ipAdress == nil {
		return nil, fmt.Errorf("no IPv4 address assigned to interface %s", netInterface)
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
			c.Logger.Error(err.Error())
		}
		go func() {
			defer func() {
				if r := recover(); r != nil {
					c.Logger.Error("panic in handle message: ", r)
				}
			}()
			err := c.handleMessage(addr, b[:i])
			if err != nil {
				c.Logger.Error("handle msg: ", err)
			}
		}()
	}
}

func (c *Client) handleMessage(src *net.UDPAddr, b []byte) error {
	var bvlc BVLC
	//Todo: support if we received and udp packet that is not bacnet
	err := bvlc.UnmarshalBinary(b)
	if err != nil {
		return err
	}
	//Todo: not race safe here: lock
	if c.subscriptions.f != nil {
		c.subscriptions.f(bvlc, *src)
	}

	apdu := bvlc.NPDU.ADPU
	if apdu != nil && (apdu.DataType == ComplexAck || apdu.DataType == Error) {
		invokeID := bvlc.NPDU.ADPU.InvokeID
		ch, ok := c.transactions.GetTransaction(invokeID)
		if !ok {
			return errors.New("no transaction found")
		}
		// Todo: can we block here ? Maybe pass context to cancel if needed
		ch <- bvlc
	}
	return nil
}

func (c *Client) WhoIs(data WhoIs, timeout time.Duration) ([]types.Device, error) {
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
	set := map[Iam]types.Address{}
	for {
		select {
		case <-timer.C:
			result := []types.Device{}
			for iam, addr := range set {
				result = append(result, types.Device{
					ID:           iam.ObjectID,
					MaxApdu:      iam.MaxApduLength,
					Segmentation: iam.SegmentationSupport,
					Vendor:       iam.VendorID,
					Addr:         addr,
				})
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
					addr := types.AddressFromUDP(r.src)
					set[*iam] = *addr
				}
			}
		}
	}
}

func (c *Client) ReadProperty(ctx context.Context, device types.Device, readProp ReadProperty) (interface{}, error) {
	invokeID := c.transactions.GetID()
	defer c.transactions.FreeID(invokeID)
	npdu := NPDU{
		Version:               Version1,
		IsNetworkLayerMessage: false,
		ExpectingReply:        true,
		Priority:              Normal,
		Destination:           &device.Addr,
		Source: types.AddressFromUDP(net.UDPAddr{
			IP:   c.ipAdress,
			Port: c.udpPort,
		}),
		HopCount: 255,
		ADPU: &APDU{
			DataType:    ConfirmedServiceRequest,
			ServiceType: ServiceConfirmedReadProperty,
			InvokeID:    invokeID,
			Payload:     &readProp,
		},
	}
	//Todo: pass context to cancel
	rChan := make(chan BVLC)
	c.transactions.SetTransaction(invokeID, rChan)
	defer c.transactions.StopTransaction(invokeID)
	_, err := c.send(npdu)
	if err != nil {
		return nil, err
	}
	select {
	case bvlc := <-rChan:
		//Todo: ensure response validity, ensure conversion cannot panic
		apdu := bvlc.NPDU.ADPU
		if apdu.DataType == Error {
			return nil, *apdu.Payload.(*ApduError)
		}
		if apdu.DataType == ComplexAck && apdu.ServiceType == ServiceConfirmedReadProperty {
			data := apdu.Payload.(*ReadProperty).Data
			return data, nil
		}
		return nil, errors.New("invalid answer")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

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
	addr := types.UDPFromAddress(*npdu.Destination)
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
