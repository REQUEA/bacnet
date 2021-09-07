package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/bacip"

	"github.com/sirupsen/logrus"
)

func main() {
	networkInterface := "en0"
	if len(os.Args) > 1 {
		networkInterface = os.Args[1]
	}
	c, err := bacip.NewClient(networkInterface, 47808)
	if err != nil {
		log.Fatal("newclient: ", err)
	}
	c.Logger = logrus.New()
	fmt.Printf("%+v\n", c)
	devices, err := c.WhoIs(bacip.WhoIs{}, time.Second)
	if err != nil {
		log.Fatal("whois: ", err)
	}
	fmt.Printf("%+v\n", devices)
	for _, device := range devices {
		err = listObjects(c, device)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func listObjects(c *bacip.Client, device bacnet.Device) error {
	prop := bacnet.PropertyIdentifier{Type: bacnet.ObjectList, ArrayIndex: new(uint32)}
	*prop.ArrayIndex = 0
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
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

	rp = bacip.ReadProperty{
		ObjectID: object,
		Property: bacnet.PropertyIdentifier{
			Type: bacnet.Units,
		},
	}

	ctx, cancel = context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	d, err = c.ReadProperty(ctx, device, rp)
	if err != nil {
		return err
	}
	unit := bacnet.Unit(d.(uint32))
	fmt.Printf("%v %v \n", value, unit)
	return nil
}
