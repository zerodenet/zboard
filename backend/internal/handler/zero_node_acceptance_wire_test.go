package handler

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/google/uuid"
)

// Minimal VLESS TCP client matching the protocol-owned Zero request contract:
// version, UUID, option length, CONNECT, port, IPv4 target. The target is the
// node's own echo service; no external destination is accepted by this helper.
func openRealZeroEcho(secret string) (net.Conn, error) {
	return openRealZeroEchoAt(secret, os.Getenv("ZBOARD_TEST_ZERO_PROXY_ADDR"))
}

func openRealZeroEchoAt(secret, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host != "zero-acceptance-node" || (port != "8443" && port != "8444") {
		return nil, fmt.Errorf("echo requires an isolated acceptance listener")
	}
	id, err := uuid.Parse(secret)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return nil, err
	}
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	payload := []byte("zboard-isolated-zero-echo")
	request := append([]byte{0}, id[:]...)
	request = append(request, 0, 1, byte(18080>>8), byte(18080&255), 1, 127, 0, 0, 1)
	request = append(request, payload...)
	if _, err = conn.Write(request); err != nil {
		conn.Close()
		return nil, err
	}
	var header [2]byte
	if _, err = io.ReadFull(conn, header[:]); err != nil {
		conn.Close()
		return nil, err
	}
	if header[0] != 0 {
		conn.Close()
		return nil, fmt.Errorf("unexpected VLESS response version")
	}
	if _, err = io.CopyN(io.Discard, conn, int64(header[1])); err != nil {
		conn.Close()
		return nil, err
	}
	echoed := make([]byte, len(payload))
	if _, err = io.ReadFull(conn, echoed); err != nil {
		conn.Close()
		return nil, err
	}
	if !bytes.Equal(payload, echoed) {
		conn.Close()
		return nil, fmt.Errorf("proxy did not return the echo payload")
	}
	conn.SetDeadline(time.Time{})
	return conn, nil
}

func realZeroEcho(secret string) error {
	conn, err := openRealZeroEcho(secret)
	if err != nil {
		return err
	}
	return conn.Close()
}
