package main

import (
	"net"

	"github.com/REQUEA/bacnet/bacip"
	"github.com/sirupsen/logrus"
)

type LogRUsAdapter struct {
	logger *logrus.Logger
}

func (l *LogRUsAdapter) Info(fields ...any) {
	l.logger.Info(fields...)
}

func (l *LogRUsAdapter) Error(fields ...any) {
	l.logger.Error(fields...)
}

func (l *LogRUsAdapter) Trace(fields ...any) {
	l.logger.Trace(fields...)
}

func main() {
	log := logrus.New()
	log.SetLevel(logrus.TraceLevel)
	bacip.SetLogger(log)

	udpAddr1, err := net.ResolveUDPAddr("udp", ":47808")
	if err != nil {
		log.Fatal("could not create UDP addr1: ", err)
	}
	conn1, err := net.ListenUDP("udp", udpAddr1)
	if err != nil {
		log.Fatal("could not create UDP connection 1: ", err)
	}

	datalink1 := bacip.BACnetIPDatalink{
		Conn: conn1,
	}

	addr1 := net.IP{10, 10, 1, 3}
	port1 := bacip.NewBACnetIPPort(1, 10, addr1, 24, 47808)
	if port1 == nil {
		log.Fatal("could not create port 1")
	}
	port1.SetDatalink(&datalink1)
	datalink1.AddPort(port1)

	addr2 := net.IP{10, 10, 2, 3}
	port2 := bacip.NewBACnetIPPort(2, 20, addr2, 25, 47808)
	if port2 == nil {
		log.Fatal("could not create port 2")
	}
	port2.SetDatalink(&datalink1)
	datalink1.AddPort(port2)

	router := bacip.NewRouterNetworkEntity().
		AddPort(port1).
		AddPort(port2)

	port1.SetNetworkEntity(router)
	port2.SetNetworkEntity(router)

	router.Start()
	datalink1.Start()

	select {}

}
