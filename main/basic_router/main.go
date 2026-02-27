package main

import (
	"net"

	"github.com/REQUEA/bacnet/linklayer"
	"github.com/REQUEA/bacnet/logger"
	"github.com/REQUEA/bacnet/networklayer"
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
	logger.SetLogger(log)

	udpAddr1, err := net.ResolveUDPAddr("udp", ":47808")
	if err != nil {
		log.Fatal("could not create UDP addr1: ", err)
	}
	conn1, err := net.ListenUDP("udp", udpAddr1)
	if err != nil {
		log.Fatal("could not create UDP connection 1: ", err)
	}

	datalink1 := linklayer.BACnetIPDatalink{
		Conn: conn1,
	}

	addr1 := net.IP{10, 10, 1, 3}
	ipPort1 := linklayer.NewBACnetIPPort(addr1, 24, 47808)
	if ipPort1 == nil {
		log.Fatal("could not create datalink port 1")
	}
	ipPort1.SetDatalink(&datalink1)
	datalink1.AddPort(ipPort1)
	nlPort1 := networklayer.NewPort(1, 10, ipPort1)
	if nlPort1 == nil {
		log.Fatal("could not create network layer port 1")
	}

	addr2 := net.IP{10, 10, 2, 3}
	ipPort2 := linklayer.NewBACnetIPPort(addr2, 25, 47808)
	if ipPort2 == nil {
		log.Fatal("could not create datalink port 2")
	}
	ipPort2.SetDatalink(&datalink1)
	datalink1.AddPort(ipPort2)
	nlPort2 := networklayer.NewPort(2, 20, ipPort2)
	if nlPort2 == nil {
		log.Fatal("could not create network layer port 2")
	}

	router := networklayer.NewRouterNetworkEntity().
		AddPort(nlPort1).
		AddPort(nlPort2)

	nlPort1.SetNetworkEntity(router)
	nlPort2.SetNetworkEntity(router)

	router.Start()
	datalink1.Start()

	select {}

}
