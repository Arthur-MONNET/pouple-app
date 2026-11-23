package remote

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"
)

// stunServers are asked, in this order, for the Mac's public address. This is the only call the core makes to the
// outside for the phone access, and only while the external host is "auto": one UDP packet of 20 bytes, which tells the
// server nothing but the source address it already sees.
var stunServers = []string{"stun.l.google.com:19302", "stun1.l.google.com:19302"}

const (
	stunMagicCookie   = 0x2112A442
	stunBindingReq    = 0x0001
	stunBindingOK     = 0x0101
	stunMappedAddress = 0x0001
	stunXORMapped     = 0x0020
	stunHeaderLength  = 20
	// stunAttempt is how long one Binding request waits for its answer before it is sent again; UDP loses packets.
	stunAttempt  = time.Second
	stunAttempts = 2
)

// PublicAddress finds the Mac's public IPv4 address. It is injectable so nothing in the tests leaves the machine.
type PublicAddress func(ctx context.Context) (netip.Addr, error)

// STUNPublicAddress asks the servers in turn (a Binding request, RFC 5389) and returns the first public IPv4 address
// one of them reports.
func STUNPublicAddress(servers []string) PublicAddress {
	return func(ctx context.Context) (netip.Addr, error) {
		var failures []error
		for _, server := range servers {
			addr, err := stunQuery(ctx, server)
			if err == nil {
				return addr, nil
			}
			failures = append(failures, err)
			if ctx.Err() != nil {
				break
			}
		}
		return netip.Addr{}, fmt.Errorf("no STUN server answered: %w", errors.Join(failures...))
	}
}

// stunQuery sends a Binding request to a server over UDP/IPv4 and reads the mapped address of the answer.
func stunQuery(ctx context.Context, server string) (netip.Addr, error) {
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "udp4", server)
	if err != nil {
		return netip.Addr{}, err
	}
	defer conn.Close()
	done := make(chan struct{})
	defer close(done)
	go func() { // a cancelled context ends a read in progress
		select {
		case <-ctx.Done():
			_ = conn.SetDeadline(time.Now())
		case <-done:
		}
	}()

	request := make([]byte, stunHeaderLength)
	binary.BigEndian.PutUint16(request[0:2], stunBindingReq)
	binary.BigEndian.PutUint32(request[4:8], stunMagicCookie)
	if _, err := rand.Read(request[8:20]); err != nil {
		return netip.Addr{}, err
	}
	buffer := make([]byte, 1500)
	for range stunAttempts {
		if _, err := conn.Write(request); err != nil {
			return netip.Addr{}, err
		}
		_ = conn.SetReadDeadline(time.Now().Add(stunAttempt))
		for {
			n, err := conn.Read(buffer)
			if err != nil {
				var timeout net.Error
				if errors.As(err, &timeout) && timeout.Timeout() && ctx.Err() == nil {
					break // send again
				}
				return netip.Addr{}, err
			}
			addr, err := parseBindingResponse(buffer[:n], request[8:20])
			if err != nil {
				continue // not our answer: keep waiting for the deadline
			}
			return addr, nil
		}
	}
	return netip.Addr{}, errors.New("STUN server did not answer")
}

// parseBindingResponse reads the public IPv4 address of a Binding success response for the transaction, from its
// XOR-MAPPED-ADDRESS (or, from an old server, MAPPED-ADDRESS).
func parseBindingResponse(packet, transaction []byte) (netip.Addr, error) {
	if len(packet) < stunHeaderLength || binary.BigEndian.Uint16(packet[0:2]) != stunBindingOK ||
		binary.BigEndian.Uint32(packet[4:8]) != stunMagicCookie || !bytes.Equal(packet[8:20], transaction) {
		return netip.Addr{}, errors.New("not a Binding response for this request")
	}
	length := int(binary.BigEndian.Uint16(packet[2:4]))
	if length%4 != 0 || stunHeaderLength+length > len(packet) {
		return netip.Addr{}, errors.New("truncated STUN response")
	}
	body := packet[stunHeaderLength : stunHeaderLength+length]
	var mapped netip.Addr
	for len(body) >= 4 {
		kind, size := binary.BigEndian.Uint16(body[0:2]), int(binary.BigEndian.Uint16(body[2:4]))
		if 4+size > len(body) {
			break
		}
		value := body[4 : 4+size]
		switch kind {
		case stunXORMapped, stunMappedAddress:
			if size >= 8 && value[1] == 0x01 { // family IPv4
				ip := [4]byte{value[4], value[5], value[6], value[7]}
				if kind == stunXORMapped {
					var cookie [4]byte
					binary.BigEndian.PutUint32(cookie[:], stunMagicCookie)
					for i := range ip {
						ip[i] ^= cookie[i]
					}
				}
				addr := netip.AddrFrom4(ip)
				if kind == stunXORMapped {
					return checkedPublic(addr)
				}
				mapped = addr
			}
		}
		padded := (size + 3) &^ 3
		if padded > len(body)-4 {
			break
		}
		body = body[4+padded:]
	}
	if mapped.IsValid() {
		return checkedPublic(mapped)
	}
	return netip.Addr{}, errors.New("no IPv4 address in the STUN response")
}

// checkedPublic refuses an address a phone on the internet cannot use.
func checkedPublic(addr netip.Addr) (netip.Addr, error) {
	if !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return netip.Addr{}, fmt.Errorf("%s is not a public address", addr)
	}
	return addr, nil
}
