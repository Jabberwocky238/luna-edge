package dns

import (
	"net"
	"strings"

	"github.com/jabberwocky238/luna-edge/geoip"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func (e *Engine) initGeoIP(opts EngineOptions) {
	if !opts.GeoIPEnabled || strings.TrimSpace(opts.GeoIPMMDBPath) == "" {
		return
	}
	reader, err := geoip.NewReader(opts.GeoIPMMDBPath)
	if err != nil {
		return
	}
	e.geoLookup = reader
	e.geoCloser = reader.Close
}

func (e *Engine) applyGeoSort(addr net.Addr, records []metadata.DNSRecord) {
	if e.geoLookup == nil || len(records) == 0 || addr == nil {
		return
	}
	clientIP := remoteAddrIP(addr)
	if clientIP == nil {
		return
	}
	clientCoords, err := e.geoLookup.Lookup(clientIP)
	if err != nil || clientCoords == nil {
		return
	}
	for i := range records {
		geoip.SortRecordValuesByDistance(&records[i], *clientCoords, e.geoLookup)
	}
}

func remoteAddrIP(addr net.Addr) net.IP {
	if addr == nil {
		return nil
	}
	switch value := addr.(type) {
	case *net.UDPAddr:
		return value.IP
	case *net.TCPAddr:
		return value.IP
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return net.ParseIP(addr.String())
	}
	return net.ParseIP(host)
}
