// Command cov_subscriber subscribes to COV notifications on a BACnet device's
// IntegerValue object (instance 1) and logs each update.
//
// When both processes run on the same machine they must use different ports, which
// prevents broadcast-based WhoIs discovery from working (each device's broadcast
// target port equals its own listen port). Use --target-addr ip:port to skip
// discovery and address the publisher directly.
//
// Typical usage on a single machine:
//
//	go run ./main/complex_device/                              # listens on :47808
//	go run ./main/cov_subscriber/ --target-addr 127.0.0.1:47808
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/REQUEA/bacnet"
	"github.com/REQUEA/bacnet/applicationlayer"
	"github.com/REQUEA/bacnet/internal/encoding"
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
	ipStr := flag.String("ip", "127.0.0.1", "local IP address for BACnet/IP")
	port := flag.Int("port", 47809, "local UDP port (must differ from the target device)")
	prefix := flag.Int("prefix", 8, "subnet prefix length")
	instance := flag.Uint("instance", 2001, "this device's instance number")
	targetAddrStr := flag.String("target-addr", "", "target device address as ip:port (skips WhoIs discovery)")
	targetInstance := flag.Uint("target", 1002, "target device instance (used for WhoIs when --target-addr is absent)")
	lifetime := flag.Uint("lifetime", 300, "COV subscription lifetime in seconds")
	flag.Parse()

	log := logrus.New()
	log.SetLevel(logrus.InfoLevel)
	logger.SetLogger(&logAdapter{log})

	// Minimal local device (needed by the stack; not publishing anything).
	id := bacnet.BACnetObjectIdentifier(uint32(bacnet.BacnetDevice)<<22 | uint32(*instance)) //nolint:gosec
	devObj := objectmodel.NewDeviceObject(
		id, "COVSubscriber", bacnet.DeviceStatusOperational,
		"REQUEA", 0, "COVSubscriber", "1.0", "1.0",
		[]bacnet.BACnetServicesSupported{
			bacnet.ServicesSupportedReadProperty,
			bacnet.ServicesSupportedUnconfirmedCovNotification,
			bacnet.ServicesSupportedConfirmedCovNotification,
		},
		[]bacnet.BACnetObjectTypesSupported{
			bacnet.ObjectTypesSupportedDevice,
		},
		1476, bacnet.SegmentationSupportNone,
		3000, 3, 0, 1,
	)
	device := objectmodel.NewDevice(devObj)

	// Open UDP socket.
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: *port})
	if err != nil {
		log.Fatalf("listen UDP :%d: %v", *port, err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Error("close connection: ", err)
		}
	}()

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

	ae := applicationlayer.NewApplicationEntity()
	ne.SetAPDUHandler(ae)
	ae.SetNetworkEntity(ne)

	sh := servicelayer.NewServiceHandler(ae, device)
	sh.RegisterCOVServices()

	// Register callback — called for every incoming COV notification.
	sh.SetCOVNotificationCallback(func(n servicelayer.COVNotificationRequest) {
		objType, objInst := n.MonitoredObjectIdentifier()
		for _, pv := range n.ListOfValues() {
			propID := pv.PropertyIdentifier()
			raw := pv.Value()
			decoded := decodeValue(raw)
			log.Infof("COV update | obj=%s(%d) prop=%s value=%s",
				bacnet.ObjectType(objType), objInst, propID, decoded)
		}
	})

	if err := datalink.Start(); err != nil {
		log.Fatalf("start datalink: %v", err)
	}
	log.Infof("subscriber %d listening on %s:%d", *instance, localIP, *port)

	// Resolve the target address: direct (--target-addr) or via WhoIs discovery.
	var targetAddr *bacnet.BACnetAddress
	if *targetAddrStr != "" {
		targetAddr, err = parseAddr(*targetAddrStr)
		if err != nil {
			log.Fatalf("invalid --target-addr %q: %v", *targetAddrStr, err)
		}
		log.Infof("using direct address %v", targetAddr)
	} else {
		log.Info("discovering target device via WhoIs (both devices must share the same port for this to work)")
		targetAddr, err = discoverDevice(sh, *targetInstance)
		if err != nil {
			log.Fatalf("discover device %d: %v", *targetInstance, err)
		}
		log.Infof("found device %d at %v", *targetInstance, targetAddr)
	}

	// Subscribe to COV on IntegerValue instance 1 using unconfirmed notifications.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const processID = 1
	err = sh.SubscribeCOV(ctx, targetAddr,
		processID,
		uint16(bacnet.IntegerValue), 1,
		false,             // unconfirmed notifications
		uint32(*lifetime), //nolint:gosec
	)
	if err != nil {
		log.Fatalf("SubscribeCOV: %v", err)
	}
	log.Infof("subscribed to COV on IntegerValue:1 (lifetime %ds) — waiting for updates…", *lifetime)

	// Wait for SIGINT/SIGTERM, then cancel the subscription cleanly.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Info("cancelling COV subscription and shutting down")
	cancelCtx, cancelCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCancel()
	if err := sh.CancelCOV(cancelCtx, targetAddr, processID, uint16(bacnet.IntegerValue), 1); err != nil {
		log.Warnf("CancelCOV: %v", err)
	}
}

// parseAddr builds a unicast BACnetAddress from an "ip:port" string.
func parseAddr(s string) (*bacnet.BACnetAddress, error) {
	host, portStr, err := net.SplitHostPort(s)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host).To4()
	if ip == nil {
		return nil, fmt.Errorf("invalid IPv4 address %q", host)
	}
	p, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid port %q: %w", portStr, err)
	}
	mac := &linklayer.BACnetIPMAC{IP: ip, Port: p}
	return &bacnet.BACnetAddress{Mac: mac}, nil
}

// discoverDevice sends WhoIs repeatedly and waits up to 10 s for an IAm from targetInstance.
// Note: requires both devices to be on the same BACnet/IP port.
func discoverDevice(sh *servicelayer.ServiceHandler, targetInstance uint) (*bacnet.BACnetAddress, error) {
	inst := uint32(targetInstance) //nolint:gosec
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := sh.WhoIs(&inst, &inst); err != nil {
			return nil, fmt.Errorf("WhoIs: %w", err)
		}
		time.Sleep(500 * time.Millisecond)
		if rd := sh.GetRemoteDevice(inst); rd != nil {
			return rd.Address(), nil
		}
	}
	return nil, fmt.Errorf("device %d not found after 10s", targetInstance)
}

// decodeValue tries to decode raw application-tagged bytes into a human-readable string.
func decodeValue(raw []byte) string {
	if len(raw) == 0 {
		return "<empty>"
	}
	tagNum := (raw[0] >> 4) & 0x0f
	switch tagNum {
	case 0x03: // signed integer
		var i encoding.Integer
		if _, err := i.Unmarshal(raw); err == nil {
			return fmt.Sprintf("%d", i.Value())
		}
	case 0x02: // unsigned integer
		var u encoding.Unsigned
		if _, err := u.Unmarshal(raw); err == nil {
			return fmt.Sprintf("%d", u.Value())
		}
	case 0x04: // real
		var r encoding.Real
		if _, err := r.Unmarshal(raw); err == nil {
			return fmt.Sprintf("%g", r.Value())
		}
	case 0x07: // character string
		var cs encoding.CharacterString
		if _, err := cs.Unmarshal(raw); err == nil {
			return fmt.Sprintf("%q", cs.Value())
		}
	case 0x08: // bit string
		return fmt.Sprintf("bits(%x)", raw)
	case 0x09: // enumerated
		var e encoding.Enumerated
		if _, err := e.Unmarshal(raw); err == nil {
			return fmt.Sprintf("enum(%d)", e.Value())
		}
	}
	return fmt.Sprintf("raw(%x)", raw)
}
