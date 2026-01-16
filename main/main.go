package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/bacip"
)

func main() {
	c, err := bacip.NewClient("192.168.3.6/24", 0, bacip.NoOpLogger{})
	if err != nil {
		log.Fatal("newclient: ", err)
	}
	d := bacnet.Device{
		ID: bacnet.ObjectID{
			Type:     bacnet.BacnetDevice,
			Instance: 1234,
		},
		Addr: *bacnet.AddressFromUDP(net.UDPAddr{
			IP:   net.ParseIP("192.168.3.6"),
			Port: 47808,
		}),
	}
	//min := uint32(0)
	//max := uint32(bacnet.MaxInstance)
	//ds, err := c.WhoIs(bacip.WhoIs{
	//	Low:  &min,
	//	High: &max,
	//}, 10*time.Second)
	//fmt.Printf("WhoIs: %+v\n", ds)
	//listObjects(c, d)
	e := writeValue(c, d, bacnet.ObjectID{
		Type:     bacnet.AnalogOutput,
		Instance: 1,
	}, float32(3.2))
	if e != nil {
		fmt.Printf("Error: %v\n", e)
		return
	}
	readValue(c, d, bacnet.ObjectID{
		Type:     bacnet.MultiStateInput,
		Instance: 1,
	})
}

func listObjects(c *bacip.Client, device bacnet.Device) error {
	prop := bacnet.PropertyIdentifier{Type: bacnet.ObjectList, ArrayIndex: new(uint32)}
	*prop.ArrayIndex = 0
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	d, err := c.ReadProperty(ctx, device, bacip.ReadProperty{
		ObjectID: device.ID,
		Property: prop,
	})
	cancel()
	if err != nil {
		return err
	}
	for i := 1; i < int(d.(uint32)); i++ {
		*prop.ArrayIndex = uint32(i)
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		d, err := c.ReadProperty(ctx, device, bacip.ReadProperty{
			ObjectID: device.ID,
			Property: prop,
		})
		cancel()
		if err != nil {
			return err
		}
		fmt.Printf("%d %+v:\t", i, d) // output for debug
		objID := d.(bacnet.ObjectID)
		ctx, cancel = context.WithTimeout(context.Background(), 1*time.Second)
		data1, err := c.ReadProperty(ctx, device, bacip.ReadProperty{
			ObjectID: objID,
			Property: bacnet.PropertyIdentifier{
				Type: bacnet.ObjectName,
			},
		})
		cancel()
		if err != nil {
			return err
		}
		fmt.Printf("%+v\t\t", data1) // output for debug
		ctx, cancel = context.WithTimeout(context.Background(), 1*time.Second)
		data2, err := c.ReadProperty(ctx, device, bacip.ReadProperty{
			ObjectID: objID,
			Property: bacnet.PropertyIdentifier{
				Type: bacnet.Description,
			},
		})
		cancel()
		if err != nil {
			return err
		}
		fmt.Printf("%+v\t", data2)
		err = readValue(c, device, objID)
		var e bacip.ApduError
		if err != nil {
			if errors.As(err, &e) { //Don't print error, device just don't have value
				fmt.Println()
			} else {
				fmt.Println(err)
			}
		}
	}
	return nil
}

func readValue(c *bacip.Client, device bacnet.Device, object bacnet.ObjectID) error {
	rp := bacip.ReadProperty{
		ObjectID: object,
		Property: bacnet.PropertyIdentifier{
			Type: bacnet.PresentValue,
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	d, err := c.ReadProperty(ctx, device, rp)
	if err != nil {
		return err
	}
	value := d
	fmt.Printf("%v\n", value)
	return nil
}

func writeValue(c *bacip.Client, device bacnet.Device, object bacnet.ObjectID, value any) error {
	wp := bacip.WriteProperty{
		ObjectID: object,
		Property: bacnet.PropertyIdentifier{
			Type: bacnet.PresentValue,
		},
		PropertyValue: bacnet.PropertyValue{
			Value: value,
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return c.WriteProperty(ctx, device, wp)
}
