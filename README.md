# BACnet

[![Go Reference](https://pkg.go.dev/badge/github.com/REQUEA/bacnet.svg)](https://pkg.go.dev/github.com/REQUEA/bacnet)

Pure Go BACnet/IP and BACnet/SC (Secure Connect) implementation with minimal dependencies.

# Status
This library is still experimental. No API compatibility promise is made.

# Features

### Confirmed Services
- [x] ReadProperty
- [x] WriteProperty
- [x] ReadPropertyMultiple
- [x] WritePropertyMultiple (atomic, all-or-nothing)
- [x] ReadRange (ByPosition, BySequenceNumber, ByTime)
- [x] SubscribeCOV / SubscribeCOVProperty
- [x] ConfirmedCOVNotification

### Unconfirmed Services
- [x] WhoIs / IAm (device discovery)
- [x] UnconfirmedCOVNotification

### Protocol Features
- [x] Segmentation (request and response, with windowing and retransmission)
- [x] APDU attribute consistency checks (BACnet 135-2024 Clause 13)
- [x] Change-of-Value (COV) subscriptions with configurable increment and expiry
- [x] Multi-network routing (NPDU forwarding across subnets)
- [x] BACnet/SC (Secure Connect) over WebSocket with optional TLS
- [x] BACnet/SC Hub Function (broadcast/unicast forwarding per ASHRAE 135 Annex AB.5.3)
- [x] BACnet/SC Node Switch (hub-connected and direct peer topologies)
- [x] BACnet/SC gateway bridging BACnet/IP and BACnet/SC networks

# Architecture

Layered stack from bottom to top:

| Layer | Package | Description |
|-------|---------|-------------|
| Link | `linklayer/` | BACnet/IP (UDP) and BACnet/SC (WebSocket) transports |
| Network | `networklayer/` | NPDU routing, multi-network forwarding |
| Application | `applicationlayer/` | APDU state machines, segmentation, transaction management |
| Service | `servicelayer/` | Service handlers and high-level client API |
| Object Model | `objectmodel/` | Device and object type definitions with self-marshaling properties |
| Encoding | `internal/encoding/` | BACnet TLV encoding for all primitive and complex types |

# Building

```sh
# Build all binaries to bin/
make build

# Run tests
make test

# Lint
make lint
```

# Examples

## BACnet/IP Device

A minimal BACnet/IP device that responds to WhoIs, ReadProperty, and WriteProperty:

```sh
go run ./main/basic_device -ip 192.168.1.10 -instance 1234 -name "MyDevice"
```

Flags: `-ip`, `-port`, `-prefix`, `-instance`, `-name`

## BACnet/IP Router

Routes NPDU traffic between two BACnet/IP networks:

```sh
go run ./main/basic_router
```

## BACnet/SC Device

A BACnet/SC device connecting to a hub over WebSocket:

```sh
go run ./main/basic_sc_device -hub ws://hub.example.com:9001 -instance 1234 -name "SCDevice"
```

Flags: `-hub`, `-failover-hub`, `-listen`, `-instance`, `-name`, `-insecure` (skip TLS verification)

## BACnet/SC Hub

A standalone BACnet/SC Hub that forwards messages between connected devices:

```sh
# Plain WebSocket
go run ./main/basic_sc_hub -listen :9001

# With TLS (wss://)
go run ./main/basic_sc_hub -listen :9001 -cert cert.pem -key key.pem
```

## BACnet/SC Gateway

Bridges BACnet/IP and BACnet/SC networks:

```sh
go run ./main/sc_hub_gateway
```

## Complex Device

A more complete BACnet/IP device with multiple object types, COV support, ReadRange, and WritePropertyMultiple:

```sh
go run ./main/complex_device eth0
```

## COV Subscriber

Demonstrates client-side COV subscription and notification handling:

```sh
go run ./main/cov_subscriber
```

# License

This library is heavily based on the gobacnet library from @alextran
which is itself based on the BACnet-Stack library originally written
by Steve Karg and therefore is released under the same license as his
project. This includes the exception which allows for this library to
be linked by proprietary code without that code becoming GPL. This
exception was taken from the original BACnet stack.

The exception is as follows:
```
    "As a special exception, if other files instantiate
     templates or use macros or inline functions from
     this file, or you compile this file and link it
     with other works to produce a work based on this file,
     this file does not by itself cause the resulting work
     to be covered by the GNU General Public License.
     However the source code for this file must still be
     made available in accordance with section (3) of the
     GNU General Public License."
```
