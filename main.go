package main

import (
	"fmt"
	"log"
	"time"
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
	c2, err := NewClient("en0", 47808)
	if err != nil {
		log.Fatal("newclient: ", err)
	}
	fmt.Printf("%+v\n", c2)
	data := WhoIs{
		low:  new(uint),
		high: new(uint),
	}
	*data.low = 0
	*data.high = 65535
	d2, err := c2.WhoIs(data)
	if err != nil {
		log.Fatal("whois: ", err)
	}
	fmt.Printf("%+v\n", d2)
	time.Sleep(time.Second)
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
