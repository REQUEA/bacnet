// Command sc_hub_gateway runs a BACnet process that:
//   - Hosts an embedded BACnet/SC Hub (other SC devices can connect)
//   - Bridges BACnet/SC and BACnet/IP networks via a RouterNetworkEntity
//   - Hosts two local devices (TemperatureSensor 2001, HumiditySensor 2002) with
//     AnalogValue objects whose present values are updated every 5 seconds
//
// Architecture:
//
//	[Embedded BACnet/SC Hub] ← HTTP/WS server on -hub-listen
//	         ↑
//	  ws://127.0.0.1:47814/  (loopback — same process)
//	         |
//	[BACnetSCDatalink] → scPort (portID=1, net=1) ─┐
//	                                               ├─→ RouterNetworkEntity → ApplicationEntity → ServiceHandler
//	[BACnetIPDatalink] → ipPort (portID=2, net=2) ─┘                                              ├── device1 (2001)
//	                                                                                              └── device2 (2002)
package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
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

func (a *logAdapter) Info(args ...any)                  { a.l.Info(args...) }
func (a *logAdapter) Error(args ...any)                 { a.l.Error(args...) }
func (a *logAdapter) Trace(args ...any)                 { a.l.Trace(args...) }
func (a *logAdapter) Infof(format string, args ...any)  { a.l.Infof(format, args...) }
func (a *logAdapter) Errorf(format string, args ...any) { a.l.Errorf(format, args...) }
func (a *logAdapter) Tracef(format string, args ...any) { a.l.Tracef(format, args...) }

func main() {
	hubListen := flag.String("hub-listen", ":47814", "BACnet/SC hub listen address")
	scHub := flag.String("sc-hub", "", "SC hub URL to connect to (default: embedded hub)")
	ip := flag.String("ip", "127.0.0.1", "Local IP for BACnet/IP")
	port := flag.Int("port", 47808, "UDP port for BACnet/IP")
	prefix := flag.Int("prefix", 8, "Subnet prefix length")
	insecure := flag.Bool("insecure", false, "Skip TLS certificate verification")
	flag.Parse()

	log := logrus.New()
	log.SetLevel(logrus.TraceLevel)
	logger.SetLogger(&logAdapter{log})

	// --- Build device 0: router device (instance 2000) ---
	devObj0 := objectmodel.NewDeviceObject(
		bacnet.BACnetObjectIdentifier(uint32(bacnet.BacnetDevice)<<22|2000),
		"Bacnet Router", bacnet.DeviceStatusOperational,
		"REQUEA", 0, "Bacnet Router", "1.0", "1.0",
		[]bacnet.BACnetServicesSupported{
			bacnet.ServicesSupportedReadProperty,
			bacnet.ServicesSupportedWriteProperty,
			bacnet.ServicesSupportedReadPropertyMultiple,
		},
		[]bacnet.BACnetObjectTypesSupported{
			bacnet.ObjectTypesSupportedDevice,
		},
		65535, bacnet.SegmentationSupportNone,
		3000, 3, 0, 1,
	)
	device0 := objectmodel.NewDevice(devObj0)

	// --- Build device 1: TemperatureSensor (instance 2001) ---
	devObj1 := objectmodel.NewDeviceObject(
		bacnet.BACnetObjectIdentifier(uint32(bacnet.BacnetDevice)<<22|2001),
		"TemperatureSensor", bacnet.DeviceStatusOperational,
		"REQUEA", 0, "TemperatureSensor", "1.0", "1.0",
		[]bacnet.BACnetServicesSupported{
			bacnet.ServicesSupportedReadProperty,
			bacnet.ServicesSupportedWriteProperty,
			bacnet.ServicesSupportedReadPropertyMultiple,
		},
		[]bacnet.BACnetObjectTypesSupported{
			bacnet.ObjectTypesSupportedDevice,
			bacnet.ObjectTypesSupportedAnalogValue,
		},
		65535, bacnet.SegmentationSupportNone,
		3000, 3, 0, 1,
	)
	device1 := objectmodel.NewDevice(devObj1)

	temp := objectmodel.NewAnalogValueObject(1, "Indoor Temperature", bacnet.DegreesCelsius)
	temp.SetPresentValue(20.0)

	// --- Build device 2: HumiditySensor (instance 2002) ---
	devObj2 := objectmodel.NewDeviceObject(
		bacnet.BACnetObjectIdentifier(uint32(bacnet.BacnetDevice)<<22|2002),
		"HumiditySensor", bacnet.DeviceStatusOperational,
		"REQUEA", 0, "HumiditySensor", "1.0", "1.0",
		[]bacnet.BACnetServicesSupported{
			bacnet.ServicesSupportedReadProperty,
			bacnet.ServicesSupportedWriteProperty,
			bacnet.ServicesSupportedReadPropertyMultiple,
		},
		[]bacnet.BACnetObjectTypesSupported{
			bacnet.ObjectTypesSupportedDevice,
			bacnet.ObjectTypesSupportedAnalogValue,
		},
		65535, bacnet.SegmentationSupportNone,
		3000, 3, 0, 1,
	)
	device2 := objectmodel.NewDevice(devObj2)

	humidity := objectmodel.NewAnalogValueObject(2, "Outdoor Humidity", bacnet.PercentRelativeHumidity)
	humidity.SetPresentValue(55.0)

	// --- Start embedded BACnet/SC Hub ---
	hub, err := linklayer.NewBACnetSCHub(linklayer.BACnetSCHubConfig{
		ListenAddr: *hubListen,
	})
	if err != nil {
		log.Fatalf("create SC hub: %v", err)
	}
	if err := hub.Start(); err != nil {
		log.Fatalf("start SC hub: %v", err)
	}
	log.Infof("BACnet/SC hub listening on %s", *hubListen)
	log.Infof("Waiting a few seconds for the hub to start...")
	time.Sleep(3 * time.Second)

	// Derive the SC hub URL when not explicitly given.
	scHubURL := *scHub
	if scHubURL == "" {
		host := *hubListen
		if host[0] == ':' {
			host = "127.0.0.1" + host
		}
		scHubURL = fmt.Sprintf("ws://%s/", host)
	}

	// --- Build BACnet/SC stack ---
	var tlsCfg *tls.Config
	if *insecure {
		tlsCfg = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	scCfg := linklayer.BACnetSCConfig{
		TLSConfig:     tlsCfg,
		PrimaryHubURL: scHubURL,
	}
	dl, err := linklayer.NewBACnetSCDatalink(scCfg)
	if err != nil {
		log.Fatalf("create BACnet/SC datalink: %v", err)
	}
	scPort := linklayer.NewBACnetSCPort(dl)
	nlPort1 := networklayer.NewPort(1, 1, scPort)

	// --- Build BACnet/IP stack ---
	localIP := net.ParseIP(*ip).To4()
	if localIP == nil {
		log.Fatalf("invalid IP address: %s", *ip)
	}
	udpAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("resolve UDP addr: %v", err)
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		log.Fatalf("listen UDP: %v", err)
	}
	ipDatalink := linklayer.BACnetIPDatalink{Conn: conn}
	ipPort := linklayer.NewBACnetIPPort(localIP, *prefix, *port)
	ipPort.SetDatalink(&ipDatalink)
	ipDatalink.AddPort(ipPort)
	nlPort2 := networklayer.NewPort(2, 2, ipPort)

	// --- Start datalinks ---
	if err := dl.Start(); err != nil {
		log.Fatalf("start BACnet/SC datalink: %v", err)
	}
	if err := ipDatalink.Start(); err != nil {
		log.Fatalf("start BACnet/IP datalink: %v", err)
	}
	log.Infof("gateway started — SC net=1 hub=%s, IP net=2 %s:%d", scHubURL, *ip, *port)

	// --- Wire up the router ---
	router := networklayer.NewRouterNetworkEntity().
		AddPort(nlPort1).
		AddPort(nlPort2)

	scPort.SetNPDUHandler(nlPort1)
	nlPort1.SetNetworkEntity(router)
	ipPort.SetNPDUHandler(nlPort2)
	nlPort2.SetNetworkEntity(router)

	ae := applicationlayer.NewApplicationEntity()
	ae.SetNetworkEntity(router)
	router.SetAPDUHandler(ae)

	router.Start()

	// --- Service layer ---
	db := objectmodel.NewObjectDatabase(ae.RemoteDeviceCache())
	for _, dev := range []*objectmodel.Device{device0, device1, device2} {
		if err := db.AddDevice(dev); err != nil {
			log.Fatalf("AddDevice: %v", err)
		}
	}
	if err := db.AddObject(device1, temp); err != nil {
		log.Fatalf("AddObject temp: %v", err)
	}
	if err := db.AddObject(device2, humidity); err != nil {
		log.Fatalf("AddObject humidity: %v", err)
	}
	sh := servicelayer.NewServiceHandler(ae, db)
	sh.RegisterCOVServices()

	// Announce presence.
	if err := sh.WhoIs(nil, nil); err != nil {
		log.Errorf("WhoIs: %v", err)
	}

	// --- Periodic value updates ---
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			temp.SetPresentValue(temp.GetPresentValue() + 0.1)
			sh.CheckAndNotifyCOV(temp, bacnet.PresentValue)

			newH := humidity.GetPresentValue() + 1.0
			if newH > 100.0 {
				newH = 0.0
			}
			humidity.SetPresentValue(newH)
			sh.CheckAndNotifyCOV(humidity, bacnet.PresentValue)
		}
	}()

	// --- Wait for signal ---
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Info("shutting down")
	if err := hub.Stop(); err != nil {
		log.Errorf("stop hub: %v", err)
	}
	if err := dl.Stop(); err != nil {
		log.Errorf("stop SC datalink: %v", err)
	}
	if err := conn.Close(); err != nil {
		log.Errorf("close UDP: %v", err)
	}
}
