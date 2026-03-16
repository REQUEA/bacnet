// Command basic_sc_device runs a minimal BACnet/SC device that connects to a hub,
// responds to WhoIs and ReadProperty requests, and logs discovered peers.
//
// Usage:
//
//	basic_sc_device -hub wss://hub.example.com:47814/sc -instance 2001 -name MyDevice
//
// For development without a real hub, omit -hub; the device starts without hub connectivity.
// To skip TLS certificate verification (dev/test mode), use -insecure.
package main

import (
	"crypto/tls"
	"flag"
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
func (a *logAdapter) Trace(args ...any)                     { a.l.Trace(args...) }
func (a *logAdapter) Infof(format string, args ...any)  { a.l.Infof(format, args...) }
func (a *logAdapter) Errorf(format string, args ...any) { a.l.Errorf(format, args...) }
func (a *logAdapter) Tracef(format string, args ...any) { a.l.Tracef(format, args...) }

func main() {
	hubURL  := flag.String("hub", "", "BACnet/SC hub WebSocket URL (wss://host:port/path)")
	failHub := flag.String("failover-hub", "", "Failover hub URL (optional)")
	listen  := flag.String("listen", "", "Local address for direct connections, e.g. :9999 (optional)")
	instance := flag.Uint("instance", 2001, "Device instance number")
	name     := flag.String("name", "BasicSCDevice", "Device object name")
	insecure := flag.Bool("insecure", false, "Skip TLS certificate verification (dev mode)")
	flag.Parse()

	log := logrus.New()
	log.SetLevel(logrus.TraceLevel)
	logger.SetLogger(&logAdapter{log})

	// Build the local device object.
	id := bacnet.BACnetObjectIdentifier(uint32(bacnet.BacnetDevice)<<22 | uint32(*instance)) //nolint:gosec
	devObj := objectmodel.NewDeviceObject(
		id, *name, bacnet.DeviceStatusOperational,
		"REQUEA", 0, *name, "1.0", "1.0",
		[]bacnet.BACnetServicesSupported{
			bacnet.ServicesSupportedReadProperty,
			bacnet.ServicesSupportedWriteProperty,
			bacnet.ServicesSupportedReadPropertyMultiple,
		},
		[]bacnet.BACnetObjectTypesSupported{
			bacnet.ObjectTypesSupportedDevice,
			bacnet.ObjectTypesSupportedAnalogInput,
		},
		65535, bacnet.SegmentationSupportNone,
		3000, 3, 0, 1,
	)
	device := objectmodel.NewDevice(devObj)

	// Configure TLS.
	var tlsCfg *tls.Config
	if *insecure {
		tlsCfg = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	// When tlsCfg is nil, BACnetSCDatalink defaults to InsecureSkipVerify automatically.

	// Build the BACnet/SC datalink.
	scCfg := linklayer.BACnetSCConfig{
		TLSConfig:        tlsCfg,
		PrimaryHubURL:    *hubURL,
		FailoverHubURL:   *failHub,
		DirectConnectURL: *listen,
	}
	dl, err := linklayer.NewBACnetSCDatalink(scCfg)
	if err != nil {
		log.Fatalf("create BACnet/SC datalink: %v", err)
	}

	// Create a port and register it.
	scPort := linklayer.NewBACnetSCPort(dl)

	// Wire up the network layer.
	nlPort := networklayer.NewPort(1, 10, scPort)
	ne := networklayer.NewNonRouterNetworkEntity(nlPort)
	scPort.SetNPDUHandler(nlPort)
	nlPort.SetNetworkEntity(ne)

	// Build the application layer.
	ae := applicationlayer.NewApplicationEntity()
	ne.SetAPDUHandler(ae)
	ae.SetNetworkEntity(ne)

	// Attach the service handler.
	db := objectmodel.NewObjectDatabase(ae.RemoteDeviceCache())
	if err := db.AddDevice(device); err != nil {
		log.Fatalf("AddDevice: %v", err)
	}

	// Add an analog input.
	roomTemp := objectmodel.NewAnalogInputObject(1, "Room Temperature", bacnet.DegreesCelsius)
	roomTemp.SetPresentValue(21.5)
	if err := db.AddObject(device, roomTemp); err != nil {
		log.Fatalf("AddObject: %v", err)
	}

	sh := servicelayer.NewServiceHandler(ae, db)

	// Start the datalink.
	if err := dl.Start(); err != nil {
		log.Fatalf("start BACnet/SC datalink: %v", err)
	}
	vmac := dl.VMAC()
	log.Infof("device %d (%s) started — VMAC %s", *instance, *name, vmac.String())

	// Announce presence.
	if err := sh.WhoIs(nil, nil); err != nil {
		log.Errorf("WhoIs: %v", err)
	}

	// Wait for SIGINT or SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Info("shutting down")
	if err := dl.Stop(); err != nil {
		log.Error("stop datalink: ", err)
	}
}
