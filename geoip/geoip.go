package geoip

import (
	"math"
	"net"
	"sort"
	"strings"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"github.com/oschwald/geoip2-golang"
)

const earthRadiusKm = 6371.0

type Coordinates struct {
	Latitude  float64
	Longitude float64
}

type IPLookup interface {
	Lookup(ip net.IP) (*Coordinates, error)
}

type Reader struct {
	db *geoip2.Reader
}

func NewReader(mmdbPath string) (*Reader, error) {
	db, err := geoip2.Open(mmdbPath)
	if err != nil {
		return nil, err
	}
	return &Reader{db: db}, nil
}

func (r *Reader) Close() error {
	if r.db == nil {
		return nil
	}
	return r.db.Close()
}

func (r *Reader) Lookup(ip net.IP) (*Coordinates, error) {
	city, err := r.db.City(ip)
	if err != nil {
		return nil, err
	}
	return &Coordinates{
		Latitude:  city.Location.Latitude,
		Longitude: city.Location.Longitude,
	}, nil
}

func Haversine(a, b Coordinates) float64 {
	lat1 := degreesToRadians(a.Latitude)
	lat2 := degreesToRadians(b.Latitude)
	dLat := degreesToRadians(b.Latitude - a.Latitude)
	dLon := degreesToRadians(b.Longitude - a.Longitude)

	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(h), math.Sqrt(1-h))
	return earthRadiusKm * c
}

func SortRecordValuesByDistance(record *metadata.DNSRecord, client Coordinates, lookup IPLookup) {
	if record == nil {
		return
	}
	if record.RecordType != "A" && record.RecordType != "AAAA" {
		return
	}
	values := splitValues(record.ValuesJSON)
	if len(values) <= 1 {
		return
	}
	type ipDist struct {
		value string
		dist  float64
	}
	entries := make([]ipDist, 0, len(values))
	for _, value := range values {
		ip := net.ParseIP(value)
		if ip == nil {
			entries = append(entries, ipDist{value: value, dist: math.MaxFloat64})
			continue
		}
		coords, err := lookup.Lookup(ip)
		if err != nil || coords == nil {
			entries = append(entries, ipDist{value: value, dist: math.MaxFloat64})
			continue
		}
		entries = append(entries, ipDist{value: value, dist: Haversine(client, *coords)})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].dist < entries[j].dist
	})
	sorted := make([]string, 0, len(entries))
	for _, entry := range entries {
		sorted = append(sorted, entry.value)
	}
	record.ValuesJSON = strings.Join(sorted, ",")
}

func degreesToRadians(deg float64) float64 {
	return deg * math.Pi / 180.0
}

func splitValues(values string) []string {
	raw := strings.TrimSpace(values)
	if raw == "" {
		return nil
	}
	if strings.Contains(raw, "[") {
		raw = strings.TrimPrefix(raw, "[")
		raw = strings.TrimSuffix(raw, "]")
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(strings.Trim(part, `"`))
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}
