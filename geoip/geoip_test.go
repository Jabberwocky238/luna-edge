package geoip

import (
	"fmt"
	"math"
	"net"
	"testing"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

type mockLookup struct {
	coords map[string]*Coordinates
}

func (m *mockLookup) Lookup(ip net.IP) (*Coordinates, error) {
	c, ok := m.coords[ip.String()]
	if !ok {
		return nil, fmt.Errorf("IP not found: %s", ip)
	}
	return c, nil
}

func TestHaversineKnownDistance(t *testing.T) {
	a := Coordinates{Latitude: 40.7128, Longitude: -74.0060}
	b := Coordinates{Latitude: 51.5074, Longitude: -0.1278}
	if got := Haversine(a, b); math.Abs(got-5570) > 20 {
		t.Fatalf("unexpected distance: %.2f", got)
	}
}

func TestSortRecordValuesByDistance(t *testing.T) {
	lookup := &mockLookup{
		coords: map[string]*Coordinates{
			"10.0.0.1": {Latitude: 43.6532, Longitude: -79.3832},
			"10.0.0.2": {Latitude: 51.5074, Longitude: -0.1278},
			"10.0.0.3": {Latitude: 35.6762, Longitude: 139.6503},
		},
	}
	record := &metadata.DNSRecord{
		RecordType: "A",
		ValuesJSON: "10.0.0.3,10.0.0.2,10.0.0.1",
	}
	client := Coordinates{Latitude: 40.7128, Longitude: -74.0060}
	SortRecordValuesByDistance(record, client, lookup)
	if record.ValuesJSON != "10.0.0.1,10.0.0.2,10.0.0.3" {
		t.Fatalf("unexpected sorted order: %q", record.ValuesJSON)
	}
}
