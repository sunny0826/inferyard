package remote

import (
	"fmt"
	"net"
	"testing"
)

func TestListenHubDefaultAndFallback(t *testing.T) {
	preferred, err := listenHub("")
	if err != nil {
		t.Fatal(err)
	}
	if port := listenerPort(preferred.Addr().String()); port != DefaultHubPort {
		_ = preferred.Close()
		if port == 0 {
			t.Fatal("fallback listener has no port")
		}
		return
	}
	_ = preferred.Close()

	blocker, err := net.Listen("tcp", fmt.Sprintf(":%d", DefaultHubPort))
	if err != nil {
		t.Skipf("cannot occupy default hub port: %v", err)
	}
	defer blocker.Close()
	fallback, err := listenHub("")
	if err != nil {
		t.Fatal(err)
	}
	defer fallback.Close()
	if port := listenerPort(fallback.Addr().String()); port == 0 || port == DefaultHubPort {
		t.Fatalf("fallback listener address = %s", fallback.Addr())
	}
}

func TestListenHubKeepsExplicitDynamicAddress(t *testing.T) {
	listener, err := listenHub("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	host, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil || host != "127.0.0.1" || port == "0" {
		t.Fatalf("dynamic listener address = %s", listener.Addr())
	}
}
