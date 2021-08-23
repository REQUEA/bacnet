package main

import (
	"bacnet"
	"bacnet/bacip"
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/sirupsen/logrus"
)

func main() {
	// c, err := gobacnet.NewClient("en0", 47808)
	// if err != nil {
	// 	fmt.Println(err)
	// }
	// fmt.Printf("%+v\n", c)
	// d, err := c.WhoIs(0, 65535)
	// if err != nil {
	// 	fmt.Println(err)
	// }
	// fmt.Printf("%+v\n", d)
	// c.Close()
	c, err := bacip.NewClient("en0", 47808)
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
	err = listObjects(c, devices[0])
	// err = readValue(c, devices[0], types.ObjectID{
	// 	Type:     types.Schedule,
	// 	Instance: 1,
	// })
	if err != nil {
		log.Fatal(err)
	}
	// err = readValue(c, devices[0], types.ObjectID{
	// 	Type:     types.AnalogOutput,
	// 	Instance: 1,
	// })
	// if err != nil {
	// 	log.Fatal(err)
	// }
	//var selectObjet = types.Device{}
	// for _, objet := range d {
	// 	fmt.Printf("%+v\n",objet)
	// 	rp := types.ReadMultipleProperty{
	// 		Objects: []types.Object{
	// 			{
	// 				ID: types.ObjectID{
	// 					Type:     types.DeviceType,
	// 					Instance: 30185,
	// 				},
	// 				Properties: []types.Property{
	// 					{
	// 						Type:       property.ObjectName,
	// 						ArrayIndex: types.ArrayAll,
	// 					},
	// 					//{
	// 					//	Type:       property.ObjectList,
	// 					//	ArrayIndex: types.ArrayAll,
	// 					//},
	// 				},
	// 			},
	// 			{ID: types.ObjectID{
	// 				Type:     types.AnalogInput,
	// 				Instance: 9011,
	// 			},
	// 				Properties: []types.Property{
	// 					{
	// 						Type:       property.ObjectName,
	// 						ArrayIndex: types.ArrayAll,
	// 					},
	// 					{
	// 						Type:       property.PresentValue,
	// 						ArrayIndex: types.ArrayAll,
	// 					},
	// 					{
	// 						Type:       property.Units,
	// 						ArrayIndex: types.ArrayAll,
	// 					},
	// 				},
	// 			},
	// 			{ID: types.ObjectID{
	// 				Type:     types.AnalogInput,
	// 				Instance: 9013,
	// 			},
	// 				Properties: []types.Property{
	// 					{
	// 						Type:       property.ObjectName,
	// 						ArrayIndex: types.ArrayAll,
	// 					},
	// 					{
	// 						Type:       property.PresentValue,
	// 						ArrayIndex: types.ArrayAll,
	// 					},
	// 					{
	// 						Type:       property.Units,
	// 						ArrayIndex: types.ArrayAll,
	// 					},
	// 				},
	// 			},
	// 		},
	// 	}
	// 	rp2, err := c.ReadMultiProperty(objet,rp)
	// 	if err != nil {
	// 		fmt.Println(err)
	// 	} else {
	// 		fmt.Printf("%+v\n", rp2)
	// 	}
	// }
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
		fmt.Printf("%+v\t", data2) // output for debug
		err = readValue(c, device, objID)
		var e bacip.ApduError
		if err != nil {
			if errors.As(err, &e) { //Don't print error, device just don't have value
				fmt.Println()
			} else {
				fmt.Println(err) // output for debug
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
			//Type: uint32(types.PROP_PRESENT_VALUE),
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
