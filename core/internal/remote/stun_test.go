package remote

import (
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeSTUN is a Binding server on a loopback UDP port: nothing in these tests leaves the machine.
type fakeSTUN struct {
	addr net.Addr
	// answer builds the datagram sent back for a request; nil answers nothing.
	answer func(request []byte) [][]byte
	got    atomic.Int32
}

func startSTUN(t *testing.T, answer func(request []byte) [][]byte) *fakeSTUN {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Skip("no loopback UDP: " + err.Error())
	}
	f := &fakeSTUN{addr: conn.LocalAddr(), answer: answer}
	var wg sync.WaitGroup
	wg.Go(func() {
		buffer := make([]byte, 1500)
		for {
			n, from, err := conn.ReadFrom(buffer)
			if err != nil {
				return
			}
			f.got.Add(1)
			request := append([]byte(nil), buffer[:n]...)
			if f.answer != nil {
				for _, reply := range f.answer(request) {
					_, _ = conn.WriteTo(reply, from)
				}
			}
		}
	})
	t.Cleanup(func() { conn.Close(); wg.Wait() })
	return f
}

// bindingResponse builds a success response for the request's transaction with one address attribute.
func bindingResponse(request []byte, kind uint16, family byte, ip [4]byte, xor bool) []byte {
	value := make([]byte, 8)
	value[1] = family
	port := uint16(54321)
	cookie := [4]byte{0x21, 0x12, 0xA4, 0x42}
	if xor {
		port ^= 0x2112
		for i := range ip {
			ip[i] ^= cookie[i]
		}
	}
	binary.BigEndian.PutUint16(value[2:4], port)
	copy(value[4:8], ip[:])
	attribute := append([]byte{byte(kind >> 8), byte(kind), 0, 8}, value...)
	// A leading attribute the client does not know and that needs padding (3 bytes of value, 1 of padding).
	unknown := []byte{0x80, 0x22, 0, 3, 'a', 'b', 'c', 0}
	body := append(unknown, attribute...)
	response := make([]byte, 20, 20+len(body))
	binary.BigEndian.PutUint16(response[0:2], stunBindingOK)
	binary.BigEndian.PutUint16(response[2:4], uint16(len(body)))
	copy(response[4:], request[4:20])
	return append(response, body...)
}

func TestSTUNPublicAddressReadsTheXORMappedAddress(t *testing.T) {
	server := startSTUN(t, func(request []byte) [][]byte {
		return [][]byte{bindingResponse(request, stunXORMapped, 0x01, [4]byte{203, 0, 113, 7}, true)}
	})
	got, err := STUNPublicAddress([]string{server.addr.String()})(context.Background())
	if err != nil || got != netip.MustParseAddr("203.0.113.7") {
		t.Fatalf("got %v, %v", got, err)
	}
	if server.got.Load() != 1 {
		t.Fatalf("the server got %d requests", server.got.Load())
	}
}

func TestSTUNAcceptsAnOldServersPlainMappedAddress(t *testing.T) {
	server := startSTUN(t, func(request []byte) [][]byte {
		return [][]byte{bindingResponse(request, stunMappedAddress, 0x01, [4]byte{198, 51, 100, 20}, false)}
	})
	got, err := stunQuery(context.Background(), server.addr.String())
	if err != nil || got != netip.MustParseAddr("198.51.100.20") {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestSTUNIgnoresAnswersForAnotherTransactionAndResendsALostRequest(t *testing.T) {
	t.Parallel()
	var seen atomic.Int32
	server := startSTUN(t, func(request []byte) [][]byte {
		if seen.Add(1) == 1 {
			return nil // the first request is lost
		}
		forged := bindingResponse(request, stunXORMapped, 0x01, [4]byte{6, 6, 6, 6}, true)
		forged[10] ^= 0xFF // another transaction: an attacker's guess
		return [][]byte{forged, bindingResponse(request, stunXORMapped, 0x01, [4]byte{203, 0, 113, 9}, true)}
	})
	got, err := stunQuery(context.Background(), server.addr.String())
	if err != nil || got != netip.MustParseAddr("203.0.113.9") {
		t.Fatalf("got %v, %v", got, err)
	}
	if server.got.Load() != 2 {
		t.Fatalf("the request was sent %d times, want 2", server.got.Load())
	}
}

func TestSTUNFallsBackToTheNextServerAndFailsWhenNoneAnswers(t *testing.T) {
	t.Parallel()
	silent := startSTUN(t, nil)
	working := startSTUN(t, func(request []byte) [][]byte {
		return [][]byte{bindingResponse(request, stunXORMapped, 0x01, [4]byte{203, 0, 113, 77}, true)}
	})
	// Shorten nothing: the silent server costs its two attempts, the budget of a real detection.
	ctx, cancel := context.WithTimeout(context.Background(), detectTimeout)
	defer cancel()
	got, err := STUNPublicAddress([]string{silent.addr.String(), working.addr.String()})(ctx)
	if err != nil || got != netip.MustParseAddr("203.0.113.77") {
		t.Fatalf("fallback: %v, %v", got, err)
	}
	if silent.got.Load() != stunAttempts {
		t.Fatalf("the first server got %d requests, want %d", silent.got.Load(), stunAttempts)
	}

	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := STUNPublicAddress([]string{silent.addr.String()})(cancelled); err == nil {
		t.Fatal("a cancelled detection must fail")
	}
	if _, err := STUNPublicAddress(nil)(context.Background()); err == nil {
		t.Fatal("no server, no address")
	}
}

func TestParseBindingResponseRefusesWhatIsNotAPublicIPv4Answer(t *testing.T) {
	request := make([]byte, 20)
	binary.BigEndian.PutUint32(request[4:8], stunMagicCookie)
	copy(request[8:], "0123456789ab")
	cases := map[string][]byte{
		"private address":  bindingResponse(request, stunXORMapped, 0x01, [4]byte{192, 168, 1, 5}, true),
		"loopback":         bindingResponse(request, stunXORMapped, 0x01, [4]byte{127, 0, 0, 1}, true),
		"unspecified":      bindingResponse(request, stunXORMapped, 0x01, [4]byte{0, 0, 0, 0}, true),
		"ipv6 family":      bindingResponse(request, stunXORMapped, 0x02, [4]byte{1, 2, 3, 4}, true),
		"too short":        request[:10],
		"error response":   append([]byte{0x01, 0x11}, request[2:]...),
		"truncated length": bindingResponse(request, stunXORMapped, 0x01, [4]byte{203, 0, 113, 7}, true)[:24],
	}
	for name, packet := range cases {
		if got, err := parseBindingResponse(packet, request[8:20]); err == nil {
			t.Errorf("%s: accepted %v", name, got)
		}
	}
	good := bindingResponse(request, stunXORMapped, 0x01, [4]byte{203, 0, 113, 7}, true)
	if got, err := parseBindingResponse(good, request[8:20]); err != nil || got.String() != "203.0.113.7" {
		t.Fatalf("a good answer: %v %v", got, err)
	}
	if _, err := parseBindingResponse(good, []byte("another-tx!!")); err == nil {
		t.Fatal("another transaction accepted")
	}
}
