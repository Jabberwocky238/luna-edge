package dns

import (
	"fmt"
	"math"
	"net"
	"testing"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

type geoLookupStub struct {
	coords map[string]*Coordinates
}

func (s *geoLookupStub) lookup(ip net.IP) (*Coordinates, error) {
	if ip == nil {
		return nil, nil
	}
	c, ok := s.coords[ip.String()]
	if !ok {
		return nil, fmt.Errorf("IP not found: %s", ip)
	}
	return c, nil
}

func (s *geoLookupStub) ApplyGeoSort(addr net.Addr, records []metadata.DNSRecord) {
	if len(records) == 0 || addr == nil {
		return
	}
	clientIP := remoteAddrIP(addr)
	if clientIP == nil {
		return
	}
	clientCoords, err := s.lookup(clientIP)
	if err != nil || clientCoords == nil {
		return
	}
	for i := range records {
		sortRecordValuesByDistance(&records[i], *clientCoords, s)
	}
}

func (s *geoLookupStub) Close() error { return nil }

func TestHaversineKnownDistance(t *testing.T) {
	a := Coordinates{Latitude: 40.7128, Longitude: -74.0060}
	b := Coordinates{Latitude: 51.5074, Longitude: -0.1278}
	if got := Haversine(a, b); math.Abs(got-5570) > 20 {
		t.Fatalf("unexpected distance: %.2f", got)
	}
}

func TestSortRecordValuesByDistance(t *testing.T) {
	lookup := &geoLookupStub{
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
	sortRecordValuesByDistance(record, client, lookup)
	if record.ValuesJSON != "10.0.0.1,10.0.0.2,10.0.0.3" {
		t.Fatalf("unexpected sorted order: %q", record.ValuesJSON)
	}
}

func TestApplyGeoSortReordersAValuesByClientDistance(t *testing.T) {
	driver := &geoLookupStub{
		coords: map[string]*Coordinates{
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

	driver.ApplyGeoSort(&net.UDPAddr{IP: net.ParseIP("203.0.113.10"), Port: 53000}, records)

	if records[0].ValuesJSON != "10.0.0.1,10.0.0.2,10.0.0.3" {
		t.Fatalf("unexpected geo-sorted values: %q", records[0].ValuesJSON)
	}
}

func TestApplyGeoSortSkipsWhenNoClientIP(t *testing.T) {
	driver := &geoLookupStub{}
	records := []metadata.DNSRecord{{
		RecordType: "A",
		ValuesJSON: "10.0.0.3,10.0.0.2,10.0.0.1",
	}}

	driver.ApplyGeoSort(nil, records)

	if records[0].ValuesJSON != "10.0.0.3,10.0.0.2,10.0.0.1" {
		t.Fatalf("expected original order without client ip, got %q", records[0].ValuesJSON)
	}
}
