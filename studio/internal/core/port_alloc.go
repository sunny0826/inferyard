package core

import (
	"errors"
	"net"
	"strconv"
)

// DefaultBenchmarkPort is the default loopback port used for benchmark model services.
const DefaultBenchmarkPort = 48857

var reservePortFunc = listenLoopbackPort
var reservePortOnHostFunc = listenPortOnHost

func listenLoopbackPort(port int) (int, error) {
	return listenTCPPort("127.0.0.1", port)
}

func listenTCPPort(host string, port int) (int, error) {
	if host == "" {
		host = "127.0.0.1"
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok || addr.Port <= 0 {
		return 0, errors.New("unexpected listener address")
	}
	return addr.Port, nil
}

func listenPortOnHost(host string, port int) (int, error) {
	if host == "" || host == "127.0.0.1" || host == "localhost" {
		return reservePortFunc(port)
	}
	return listenTCPPort(host, port)
}

// ChooseFreeLoopbackPort returns preferred if it can be bound on 127.0.0.1,
// or falls back to an available ephemeral loopback port when preferred is occupied.
func ChooseFreeLoopbackPort(preferred int) (int, error) {
	return ChooseFreePortExcluding("127.0.0.1", preferred, nil)
}

// ChooseFreePortExcluding returns preferred if it is not in exclude and can be bound on host,
// otherwise allocates an ephemeral port on host not in exclude.
func ChooseFreePortExcluding(host string, preferred int, exclude map[int]bool) (int, error) {
	if host == "" {
		host = "127.0.0.1"
	}
	if preferred >= 1 && preferred <= 65535 && !exclude[preferred] {
		if port, err := reservePortOnHostFunc(host, preferred); err == nil && port > 0 && !exclude[port] {
			return port, nil
		}
	}
	for range 32 {
		port, err := reservePortOnHostFunc(host, 0)
		if err != nil {
			return 0, err
		}
		if port > 0 && !exclude[port] {
			return port, nil
		}
	}
	return 0, errors.New("no free port available")
}

func checkBindPortAvailable(host string, port int) error {
	if host == "" {
		host = "127.0.0.1"
	}
	_, err := reservePortOnHostFunc(host, port)
	return err
}
