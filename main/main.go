package main

import (
	"bacnet"
	"bacnet/types"
	"context"
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
	c2, err := bacnet.NewClient("en0", 47808)
	if err != nil {
		log.Fatal("newclient: ", err)
	}
	c2.Logger = logrus.New()
	fmt.Printf("%+v\n", c2)
	data := bacnet.WhoIs{
		Low:  new(uint32),
		High: new(uint32),
	}
	*data.Low = 0
	*data.High = 65535
	d2, err := c2.WhoIs(data, time.Second)
	if err != nil {
		log.Fatal("whois: ", err)
	}
	fmt.Printf("%+v\n", d2)
	prop := types.PropertyIdentifier{Type: types.ObjectList, ArrayIndex: new(uint32)}
	*prop.ArrayIndex = 0
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	d, err := c2.ReadProperty(ctx, d2[0], bacnet.ReadProperty{
		ObjectID: d2[0].ObjectID,
		Property: prop,
	})
	cancel()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d %+v\n", 0, d) // output for debug

	// for i := 1; i < 343; i++ {
	// 	*prop.ArrayIndex = uint32(i)
	// 	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	// 	d, err := c2.ReadProperty(ctx, d2[0], bacnet.ReadProperty{
	// 		ObjectID: d2[0].ObjectID,
	// 		Property: prop,
	// 	})
	// 	cancel()
	// 	if err != nil {
	// 		log.Fatal(err)
	// 	}
	// 	fmt.Printf("%d %+v:\t", i, d) // output for debug
	// 	objID := d.(types.ObjectID)
	// 	ctx, cancel = context.WithTimeout(context.Background(), 1*time.Second)
	// 	data1, err := c2.ReadProperty(ctx, d2[0], bacnet.ReadProperty{
	// 		ObjectID: objID,
	// 		Property: types.PropertyIdentifier{
	// 			Type: types.ObjectName,
	// 		},
	// 	})
	// 	cancel()
	// 	if err != nil {
	// 		log.Fatal(err)
	// 	}
	// 	fmt.Printf("%+v\t\t", data1) // output for debug
	// 	ctx, cancel = context.WithTimeout(context.Background(), 1*time.Second)
	// 	data2, err := c2.ReadProperty(ctx, d2[0], bacnet.ReadProperty{
	// 		ObjectID: objID,
	// 		Property: types.PropertyIdentifier{
	// 			Type: types.Description,
	// 		},
	// 	})
	// 	cancel()
	// 	if err != nil {
	// 		log.Fatal(err)
	// 	}
	// 	fmt.Printf("%+v\n", data2) // output for debug

	// }

	rp := bacnet.ReadProperty{
		ObjectID: types.ObjectID{
			Type:     types.AnalogValue,
			Instance: 8121,
		},
		Property: types.PropertyIdentifier{
			//Type: uint32(types.PROP_PRESENT_VALUE),
			Type: types.Units,
		},
	}
	ctx, cancel = context.WithTimeout(context.Background(), 1*time.Second)
	d, err = c2.ReadProperty(ctx, d2[0], rp)
	if err != nil {
		log.Fatal(err)
	}
	cancel()
	r := types.Unit(d.(uint32))
	fmt.Printf("%+v (%T)\n", d, d) // output for debug
	fmt.Printf("%+v (%T)\n", r, r) // output for debug
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
