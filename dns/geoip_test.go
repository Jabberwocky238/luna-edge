package dns

import (
	"net"
	"testing"

	"github.com/jabberwocky238/luna-edge/geoip"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

type geoLookupStub struct {
	coords map[string]*geoip.Coordinates
}

func (s geoLookupStub) Lookup(ip net.IP) (*geoip.Coordinates, error) {
	if ip == nil {
		return nil, nil
	}
	return s.coords[ip.String()], nil
}

func TestApplyGeoSortReordersAValuesByClientDistance(t *testing.T) {
	engine := NewEngine(EngineOptions{})
	engine.geoLookup = geoLookupStub{
		coords: map[string]*geoip.Coordinates{
			"203.0.113.10": {Latitude: 40.7128, Longitude: -74.0060},
			"10.0.0.1":     {Latitude: 43.6532, Longitude: -79.3832},
			"10.0.0.2":     {Latitude: 51.5074, Longitude: -0.1278},
			"10.0.0.3":     {Latitude: 35.6762, Longitude: 139.6503},
		},
	}
	records := []metadata.DNSRecord{{
		RecordType: "A",
		ValuesJSON: "10.0.0.3,10.0.0.2,10.0.0.1",
	}}

	engine.applyGeoSort(&net.UDPAddr{IP: net.ParseIP("203.0.113.10"), Port: 53000}, records)

	if records[0].ValuesJSON != "10.0.0.1,10.0.0.2,10.0.0.3" {
		t.Fatalf("unexpected geo-sorted values: %q", records[0].ValuesJSON)
	}
}

func TestApplyGeoSortSkipsWhenNoClientIP(t *testing.T) {
	engine := NewEngine(EngineOptions{})
	engine.geoLookup = geoLookupStub{}
	records := []metadata.DNSRecord{{
		RecordType: "A",
		ValuesJSON: "10.0.0.3,10.0.0.2,10.0.0.1",
	}}

	engine.applyGeoSort(nil, records)

	if records[0].ValuesJSON != "10.0.0.3,10.0.0.2,10.0.0.1" {
		t.Fatalf("expected original order without client ip, got %q", records[0].ValuesJSON)
	}
}
