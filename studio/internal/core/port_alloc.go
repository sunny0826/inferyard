package core

import (
	"errors"
	"net"
	"strconv"
)

// DefaultBenchmarkPort is the default loopback port used for benchmark model services.
const DefaultBenchmarkPort = 48857

var reservePortFunc = listenLoopbackPort

func listenLoopbackPort(port int) (int, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok || addr.Port <= 0 {
		return 0, errors.New("unexpected loopback listener address")
	}
	return addr.Port, nil
}

// ChooseFreeLoopbackPort returns preferred if it can be bound on 127.0.0.1,
// or falls back to an available ephemeral loopback port when preferred is occupied.
func ChooseFreeLoopbackPort(preferred int) (int, error) {
	return ChooseFreePortExcluding(preferred, nil)
}

// ChooseFreePortExcluding returns preferred if it is not in exclude and can be bound,
// otherwise allocates an ephemeral loopback port not in exclude.
func ChooseFreePortExcluding(preferred int, exclude map[int]bool) (int, error) {
	if preferred >= 1 && preferred <= 65535 && !exclude[preferred] {
		if port, err := reservePortFunc(preferred); err == nil && port > 0 && !exclude[port] {
			return port, nil
		}
	}
	for range 32 {
		port, err := reservePortFunc(0)
		if err != nil {
			return 0, err
		}
		if port > 0 && !exclude[port] {
			return port, nil
		}
	}
	return 0, errors.New("no free loopback port available")
}

func checkBindPortAvailable(host string, port int) error {
	if host == "" {
		host = "127.0.0.1"
	}
	if host == "127.0.0.1" || host == "localhost" {
		_, err := reservePortFunc(port)
		return err
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return err
	}
	_ = ln.Close()
	return nil
}
