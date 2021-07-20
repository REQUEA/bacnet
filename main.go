package main

import (
	"fmt"
	"github.com/alexbeltran/gobacnet"
	"github.com/alexbeltran/gobacnet/property"
	"github.com/alexbeltran/gobacnet/types"
)

func main() {
	c, err := gobacnet.NewClient("en0", 47808)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Printf("%+v\n",c)
	d, err := c.WhoIs(0,65535)
	if err != nil {
		fmt.Println(err)
	}
	//fmt.Printf("%+v\n",d)
	//var selectObjet = types.Device{}
	for _, objet := range d {
		fmt.Printf("%+v\n",objet)
		rp := types.ReadMultipleProperty{
			Objects: []types.Object{
				{
					ID: types.ObjectID{
						Type:     types.DeviceType,
						Instance: 30185,
					},
					Properties: []types.Property{
						{
							Type:       property.ObjectName,
							ArrayIndex: types.ArrayAll,
						},
						//{
						//	Type:       property.ObjectList,
						//	ArrayIndex: types.ArrayAll,
						//},
					},
				},
				{ID: types.ObjectID{
					Type:     types.AnalogInput,
					Instance: 9011,
				},
					Properties: []types.Property{
						{
							Type:       property.ObjectName,
							ArrayIndex: types.ArrayAll,
						},
						{
							Type:       property.PresentValue,
							ArrayIndex: types.ArrayAll,
						},
						{
							Type:       property.Units,
							ArrayIndex: types.ArrayAll,
						},
					},
				},
				{ID: types.ObjectID{
					Type:     types.AnalogInput,
					Instance: 9013,
				},
					Properties: []types.Property{
						{
							Type:       property.ObjectName,
							ArrayIndex: types.ArrayAll,
						},
						{
							Type:       property.PresentValue,
							ArrayIndex: types.ArrayAll,
						},
						{
							Type:       property.Units,
							ArrayIndex: types.ArrayAll,
						},
					},
				},
			},
		}
		rp2, err := c.ReadMultiProperty(objet,rp)
		if err != nil {
			fmt.Println(err)
		} else {
			fmt.Printf("%+v\n", rp2)
		}
	}
}