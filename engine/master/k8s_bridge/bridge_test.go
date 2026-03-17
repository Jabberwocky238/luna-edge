package k8s_bridge

import (
	"context"
	"testing"
)

type stubChildBridge struct {
	loadCount   int
	listenCount int
	stopCount   int
}

func (s *stubChildBridge) LoadInitial(context.Context) error {
	s.loadCount++
	return nil
}

func (s *stubChildBridge) Listen() {
	s.listenCount++
}

func (s *stubChildBridge) Stop() error {
	s.stopCount++
	return nil
}

func TestBridgeAggregatesChildrenLifecycle(t *testing.T) {
	dns := &stubChildBridge{}
	ingress := &stubChildBridge{}
	gateway := &stubChildBridge{}

	bridge := &Bridge{
		DNS:      nil,
		Ingress:  ingress,
		Gateway:  gateway,
		children: []childBridge{dns, ingress, gateway},
	}

	if err := bridge.LoadInitial(context.Background()); err != nil {
		t.Fatalf("load initial: %v", err)
	}
	bridge.Listen()
	if err := bridge.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}

	for name, child := range map[string]*stubChildBridge{
		"dns":     dns,
		"ingress": ingress,
		"gateway": gateway,
	} {
		if child.loadCount != 1 || child.listenCount != 1 || child.stopCount != 1 {
			t.Fatalf("unexpected lifecycle for %s: %+v", name, child)
		}
	}
}
