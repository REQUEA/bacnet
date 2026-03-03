// Command basic_router connects two BACnet/IP networks and forwards NPDUs between them.
package main

import (
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/REQUEA/bacnet/applicationlayer"
	"github.com/REQUEA/bacnet/linklayer"
	"github.com/REQUEA/bacnet/logger"
	"github.com/REQUEA/bacnet/networklayer"
	"github.com/sirupsen/logrus"
)

type logAdapter struct{ l *logrus.Logger }

func (a *logAdapter) Info(args ...any)  { a.l.Info(args...) }
func (a *logAdapter) Error(args ...any) { a.l.Error(args...) }
func (a *logAdapter) Trace(args ...any) { a.l.Trace(args...) }

func main() {
	log := logrus.New()
	log.SetLevel(logrus.TraceLevel)
	logger.SetLogger(&logAdapter{log})

	udpAddr1, err := net.ResolveUDPAddr("udp", ":47808")
	if err != nil {
		log.Fatal("could not create UDP addr: ", err)
	}
	conn1, err := net.ListenUDP("udp", udpAddr1)
	if err != nil {
		log.Fatal("could not create UDP connection: ", err)
	}

	datalink1 := linklayer.BACnetIPDatalink{Conn: conn1}

	// Network 10 — interface 10.10.1.3/24
	addr1 := net.IP{10, 10, 1, 3}
	ipPort1 := linklayer.NewBACnetIPPort(addr1, 24, 47808)
	ipPort1.SetDatalink(&datalink1)
	datalink1.AddPort(ipPort1)
	nlPort1 := networklayer.NewPort(1, 10, ipPort1)

	// Network 20 — interface 10.10.2.3/24
	addr2 := net.IP{10, 10, 2, 3}
	ipPort2 := linklayer.NewBACnetIPPort(addr2, 24, 47808)
	ipPort2.SetDatalink(&datalink1)
	datalink1.AddPort(ipPort2)
	nlPort2 := networklayer.NewPort(2, 20, ipPort2)

	router := networklayer.NewRouterNetworkEntity().
		AddPort(nlPort1).
		AddPort(nlPort2)

	// Wire the NPDU handler so the datalink can deliver to the network layer.
	ipPort1.SetNPDUHandler(nlPort1)
	nlPort1.SetNetworkEntity(router)
	ipPort2.SetNPDUHandler(nlPort2)
	nlPort2.SetNetworkEntity(router)

	ae := applicationlayer.NewApplicationEntity()
	ae.SetNetworkEntity(router)
	router.SetAPDUHandler(ae)

	router.Start()
	if err := datalink1.Start(); err != nil {
		log.Fatal("could not start datalink: ", err)
	}
	log.Info("BACnet router started (networks 10 ↔ 20)")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Info("shutting down")
	conn1.Close()
}
