// Command complex_device runs a BACnet/IP device that demonstrates segmentation
// (via a long CharacterStringValue) and COV notifications (via a periodically
// updated IntegerValue counter).
package main

import (
	"flag"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

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
func (a *logAdapter) Trace(args ...any)                     { a.l.Trace(args...) }
func (a *logAdapter) Infof(format string, args ...any)  { a.l.Infof(format, args...) }
func (a *logAdapter) Errorf(format string, args ...any) { a.l.Errorf(format, args...) }
func (a *logAdapter) Tracef(format string, args ...any) { a.l.Tracef(format, args...) }

// loremIpsum returns a string of approximately n characters built from a
// repeated Lorem Ipsum paragraph. Used to produce a value long enough to
// require segmentation when returned as a ReadProperty response.
func loremIpsum(n int) string {
	const para = "Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod " +
		"tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, " +
		"quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. " +
		"Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu " +
		"fugiat nulla pariatur. Excepteur sint occaecat cupidatat non proident, sunt in " +
		"culpa qui officia deserunt mollit anim id est laborum. "
	var sb strings.Builder
	for sb.Len() < n {
		sb.WriteString(para)
	}
	return sb.String()[:n]
}

func main() {
	ipStr := flag.String("ip", "127.0.0.1", "local IP address for BACnet/IP")
	port := flag.Int("port", 47808, "UDP port")
	prefix := flag.Int("prefix", 8, "subnet prefix length (e.g. 24 for /24)")
	instance := flag.Uint("instance", 1002, "device instance number")
	name := flag.String("name", "ComplexDevice", "device object name")
	flag.Parse()

	log := logrus.New()
	log.SetLevel(logrus.TraceLevel)
	logger.SetLogger(&logAdapter{log})

	// Build the local device object with segmentation and COV support.
	id := bacnet.BACnetObjectIdentifier(uint32(bacnet.BacnetDevice)<<22 | uint32(*instance)) //nolint:gosec
	devObj := objectmodel.NewDeviceObject(
		id, *name, bacnet.DeviceStatusOperational,
		"REQUEA", 0, *name, "1.0", "1.0",
		[]bacnet.BACnetServicesSupported{
			bacnet.ServicesSupportedReadProperty,
			bacnet.ServicesSupportedWriteProperty,
			bacnet.ServicesSupportedReadPropertyMultiple,
			bacnet.ServicesSupportedSubscribeCov,
			bacnet.ServicesSupportedConfirmedCovNotification,
			bacnet.ServicesSupportedUnconfirmedCovNotification,
		},
		[]bacnet.BACnetObjectTypesSupported{
			bacnet.ObjectTypesSupportedDevice,
			bacnet.ObjectTypesSupportedAnalogInput,
			bacnet.ObjectTypesSupportedCharacterstringValue,
			bacnet.ObjectTypesSupportedIntegerValue,
		},
		1476, bacnet.SegmentationSupportBoth,
		3000, 3, 0, 1,
	)
	device := objectmodel.NewDevice(devObj)

	// IntegerValue counter — updated every 5 s to trigger COV notifications.
	counter := objectmodel.NewIntegerValueObject(1, "Counter", 0, bacnet.NoUnits)
	device.AddObject(counter)

	// CharacterStringValue with a 2000-character value to exercise segmentation.
	longDesc := objectmodel.NewCharacterStringValueObject(1, "Long Description", loremIpsum(2000))
	device.AddObject(longDesc)

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

	// Attach the service handler and opt into COV publish/subscribe.
	sh := servicelayer.NewServiceHandler(ae, device)
	sh.RegisterCOVServices()

	// Log incoming COV notifications (e.g. from other devices on the network).
	sh.SetCOVNotificationCallback(func(_ servicelayer.COVNotificationRequest) {
		log.Info("COV notification received")
	})

	// Start receiving.
	if err := datalink.Start(); err != nil {
		log.Fatalf("start datalink: %v", err)
	}
	log.Infof("device %d (%s) listening on %s:%d", *instance, *name, localIP, *port)

	// Announce presence and discover peers.
	if err := sh.WhoIs(nil, nil); err != nil {
		log.Errorf("WhoIs: %v", err)
	}

	// Increment counter every 5 s and publish COV to all subscribers.
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			counter.SetPresentValue(counter.GetPresentValue() + 1)
			sh.CheckAndNotifyCOV(counter, bacnet.PresentValue)
			log.Infof("counter incremented to %d", counter.GetPresentValue())
		}
	}()

	// Wait for SIGINT or SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Info("shutting down")
	if err := conn.Close(); err != nil {
		log.Error("close connection: ", err)
	}
}
