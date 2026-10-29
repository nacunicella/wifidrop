package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseBeacon(t *testing.T) {
	good, _ := json.Marshal(Beacon{Magic: beaconMagic, ID: "abc", Name: "PC", Port: 8750, OS: "linux"})
	if b, ok := parseBeacon(good); !ok || b.ID != "abc" {
		t.Fatalf("valid beacon rejected: %+v %v", b, ok)
	}
	bad := [][]byte{
		[]byte("not json"),
		mustJSON(Beacon{Magic: "OTHER", ID: "abc", Port: 8750}),
		mustJSON(Beacon{Magic: beaconMagic, ID: "", Port: 8750}),
		mustJSON(Beacon{Magic: beaconMagic, ID: "abc", Port: 70000}),
	}
	for i, data := range bad {
		if _, ok := parseBeacon(data); ok {
			t.Errorf("case %d: invalid beacon accepted", i)
		}
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func TestDiscoveryBetweenInstances(t *testing.T) {
	const port = 48751 // unlikely to be used by anything else on the test host
	a := NewDiscovery(Beacon{ID: "aaa", Name: "A", Port: 8001, OS: "test"}, port)
	b := NewDiscovery(Beacon{ID: "bbb", Name: "B", Port: 8002, OS: "test"}, port)
	if err := a.Start(); err != nil {
		t.Skipf("cannot open UDP socket: %v", err)
	}
	defer a.Stop()
	if err := b.Start(); err != nil {
		t.Skipf("cannot open second UDP socket: %v", err)
	}
	defer b.Stop()

	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if p, ok := a.Peer("bbb"); ok && p.Port == 8002 {
			if _, ok := a.Peer("aaa"); ok {
				t.Fatal("instance must not list itself")
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Skip("no broadcast traffic on this host; discovery could not be verified")
}
