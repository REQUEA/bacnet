// Command basic_sc_hub runs a standalone BACnet/SC Hub Function.
// It accepts WebSocket connections from BACnet/SC devices and forwards
// EncapsulatedNPDU messages between them (broadcast and unicast).
//
// Usage:
//
//	basic_sc_hub -listen :47814
//	basic_sc_hub -listen :47814 -cert cert.pem -key key.pem
//
// When -cert and -key are both provided the hub uses TLS (wss://); otherwise
// it uses plain WebSocket (ws://).
package main

import (
	"crypto/tls"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/REQUEA/bacnet/linklayer"
	"github.com/REQUEA/bacnet/logger"
	"github.com/sirupsen/logrus"
)

type logAdapter struct{ l *logrus.Logger }

func (a *logAdapter) Info(args ...any)  { a.l.Info(args...) }
func (a *logAdapter) Error(args ...any) { a.l.Error(args...) }
func (a *logAdapter) Trace(args ...any) { a.l.Trace(args...) }

func main() {
	listenAddr := flag.String("listen", ":47814", "Hub listen address (host:port)")
	certFile := flag.String("cert", "", "TLS certificate file (PEM, optional)")
	keyFile := flag.String("key", "", "TLS private key file (PEM, optional)")
	flag.Parse()

	log := logrus.New()
	log.SetLevel(logrus.TraceLevel)
	logger.SetLogger(&logAdapter{log})

	var tlsCfg *tls.Config
	if *certFile != "" && *keyFile != "" {
		cert, err := tls.LoadX509KeyPair(*certFile, *keyFile)
		if err != nil {
			log.Fatalf("load TLS certificate: %v", err)
		}
		tlsCfg = &tls.Config{Certificates: []tls.Certificate{cert}}
	}

	hub, err := linklayer.NewBACnetSCHub(linklayer.BACnetSCHubConfig{
		ListenAddr: *listenAddr,
		TLSConfig:  tlsCfg,
	})
	if err != nil {
		log.Fatalf("create hub: %v", err)
	}
	if err := hub.Start(); err != nil {
		log.Fatalf("start hub: %v", err)
	}

	proto := "ws"
	if tlsCfg != nil {
		proto = "wss"
	}
	log.Infof("BACnet/SC hub listening on %s://%s", proto, *listenAddr)
	log.Infof("connect devices with: -hub %s://localhost%s", proto, *listenAddr)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Info("shutting down")
	if err := hub.Stop(); err != nil {
		log.Error("stop hub: ", err)
	}
}
