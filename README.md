# Bacnet
[![Go Reference](https://pkg.go.dev/badge/github.com/REQUEA/bacnet.svg)](https://pkg.go.dev/github.com/REQUEA/bacnet)

bacnet is a minimal Bacnet over IP implementation in pure go with zero dependencies. 

# Status 
This library is still experimental. No API compatibility promise is made. 

# Features
- [x] Who Is
- [x] Read Property
- [x] Write Property


# License
This library is heavily based on the gobacnet library from @alextran
which is itself based on the BACnet-Stack library originally written
by Steve Karg and therefore is released under the same license as his
project.  This includes the exception which allows for this library to
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
