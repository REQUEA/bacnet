// Command basic_device runs a minimal BACnet/IP device that responds to WhoIs
// and ReadProperty requests and logs discovered peers.
package main

import (
	"flag"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/applicationlayer"
	"github.com/REQUEA/bacnet/linklayer"
	"github.com/REQUEA/bacnet/logger"
	"github.com/REQUEA/bacnet/networklayer"
	"github.com/REQUEA/bacnet/objectmodel"
	"github.com/REQUEA/bacnet/servicelayer"
	"github.com/sirupsen/logrus"
)

type logAdapter struct{ l *logrus.Logger }

func (a *logAdapter) Info(args ...any)  { a.l.Info(args...) }
func (a *logAdapter) Error(args ...any) { a.l.Error(args...) }
func (a *logAdapter) Trace(args ...any) { a.l.Trace(args...) }

func main() {
	ipStr := flag.String("ip", "127.0.0.1", "local IP address for BACnet/IP")
	port := flag.Int("port", 47808, "UDP port")
	prefix := flag.Int("prefix", 8, "subnet prefix length (e.g. 24 for /24)")
	instance := flag.Uint("instance", 1001, "device instance number")
	name := flag.String("name", "BasicDevice", "device object name")
	flag.Parse()

	log := logrus.New()
	log.SetLevel(logrus.TraceLevel)
	logger.SetLogger(&logAdapter{log})

	// Build the local device object.
	id := bacnet.BACnetObjectIdentifier(uint32(bacnet.BacnetDevice)<<22 | uint32(*instance))
	devObj := objectmodel.NewDeviceObject(
		id, *name, bacnet.DeviceStatusOperational,
		"REQUEA", 0, *name, "1.0", "1.0",
		[]bacnet.BACnetServicesSupported{
			bacnet.ServicesSupportedReadProperty,
			bacnet.ServicesSupportedWriteProperty,
			bacnet.ServicesSupportedReadPropertyMultiple,
		},
		[]bacnet.BACnetObjectTypesSupported{bacnet.ObjectTypesSupportedDevice},
		1476, bacnet.SegmentationSupportNone,
		3000, 3, 0, 1,
	)
	device := objectmodel.NewDevice(devObj)

	// Open UDP socket.
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: *port})
	if err != nil {
		log.Fatalf("listen UDP :%d: %v", *port, err)
	}

	// Build the link-layer / network-layer stack.
	localIP := net.ParseIP(*ipStr).To4()
	if localIP == nil {
		log.Fatalf("invalid IP address: %s", *ipStr)
	}
	datalink := &linklayer.BACnetIPDatalink{Conn: conn}
	ipPort := linklayer.NewBACnetIPPort(localIP, *prefix, *port)
	ipPort.SetDatalink(datalink)
	datalink.AddPort(ipPort)

	nlPort := networklayer.NewPort(1, 10, ipPort)
	ne := networklayer.NewNonRouterNetworkEntity(nlPort)
	ipPort.SetNPDUHandler(nlPort)
	nlPort.SetNetworkEntity(ne)

	// Build the application layer and wire everything together.
	ae := applicationlayer.NewApplicationEntity()
	ne.SetAPDUHandler(ae)
	ae.SetNetworkEntity(ne)

	// Attach the service handler (registers confirmed / unconfirmed service handlers).
	sh := servicelayer.NewServiceHandler(ae, device)

	// Start receiving.
	if err := datalink.Start(); err != nil {
		log.Fatalf("start datalink: %v", err)
	}
	log.Infof("device %d (%s) listening on %s:%d", *instance, *name, localIP, *port)

	// Announce presence and discover peers.
	if err := sh.WhoIs(nil, nil); err != nil {
		log.Errorf("WhoIs: %v", err)
	}

	// Wait for SIGINT or SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Info("shutting down")
	conn.Close()
}
