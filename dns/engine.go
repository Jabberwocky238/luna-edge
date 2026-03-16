package dns

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
	mdns "github.com/miekg/dns"
)

// Engine 负责连接 repository 和 DNS 操作层。
//
// 它承担两类职责：
// - 查询解析：把 repository 中的 DNS 物化数据解析成查询结果
// - 记录操作：把 add/mod/del 这类操作落到 repository
type Engine struct {
	store     *dnsMemoryStore
	forwarder *Forwarder
	geoDriver GeoIPDriver
	k8sBridge *K8sBridge
	recordsMu sync.Mutex
	baseDNS   []metadata.DNSRecord
	k8sDNS    []metadata.DNSRecord
	udpServer *mdns.Server
	tcpServer *mdns.Server
	mu        sync.Mutex
}

type EngineOptions struct {
	Forwarder     ForwarderConfig
	GeoIPEnabled  bool
	GeoIPMMDBPath string
	K8sEnabled    bool
	K8sNamespace  string
}

type GeoIPDriver interface {
	ApplyGeoSort(addr net.Addr, records []metadata.DNSRecord)
	Close() error
}

// NewEngine 创建一个 DNS 执行引擎。
func NewEngine(opts EngineOptions) *Engine {
	forwarderCfg := opts.Forwarder
	if len(forwarderCfg.Servers) == 0 && forwarderCfg.Timeout == 0 && !forwarderCfg.Enabled {
		forwarderCfg = DefaultForwarderConfig()
	}
	engine := &Engine{
		store:     newDNSMemoryStore(),
		forwarder: NewForwarder(forwarderCfg),
	}
	engine.initGeoIP(opts)
	engine.initK8sBridge(opts)
	return engine
}

func (e *Engine) RefreshQuestion(fqdn string, recordType metadata.DNSRecordType) {
	if e == nil || e.store == nil {
		return
	}
	e.store.RefreshQuestion(DNSQuestion{
		FQDN:       fqdn,
		RecordType: recordType,
	})
}

func (e *Engine) RefreshAll() {
	if e == nil || e.store == nil {
		return
	}
	e.store.Clear()
}

func (e *Engine) RestoreRecords(records []metadata.DNSRecord) {
	if e == nil || e.store == nil {
		return
	}
	e.recordsMu.Lock()
	e.baseDNS = cloneDNSRecords(records)
	merged := append(cloneDNSRecords(e.baseDNS), cloneDNSRecords(e.k8sDNS)...)
	e.recordsMu.Unlock()
	e.store.Restore(merged)
}

// Listen 启动 DNS 监听。
//
// 它会同时启动 UDP 和 TCP 两个 DNS 服务端，并把查询转发到 Engine.Lookup。
func (e *Engine) Listen(listenAddr string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.udpServer != nil || e.tcpServer != nil {
		return fmt.Errorf("dns engine is already listening")
	}
	if strings.TrimSpace(listenAddr) == "" {
		return fmt.Errorf("listen address is required")
	}

	handler := mdns.HandlerFunc(func(w mdns.ResponseWriter, r *mdns.Msg) {
		e.serveDNS(w, r)
	})

	e.udpServer = &mdns.Server{
		Addr:    listenAddr,
		Net:     "udp",
		Handler: handler,
	}
	e.tcpServer = &mdns.Server{
		Addr:    listenAddr,
		Net:     "tcp",
		Handler: handler,
	}

	go func() {
		_ = e.udpServer.ListenAndServe()
	}()
	go func() {
		_ = e.tcpServer.ListenAndServe()
	}()
	if e.k8sBridge != nil {
		e.k8sBridge.Listen()
	}

	return nil
}

// Stop 停止已经启动的 DNS 监听。
func (e *Engine) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	var errs []string
	if e.udpServer != nil {
		if err := e.udpServer.Shutdown(); err != nil {
			errs = append(errs, err.Error())
		}
		e.udpServer = nil
	}
	if e.tcpServer != nil {
		if err := e.tcpServer.Shutdown(); err != nil {
			errs = append(errs, err.Error())
		}
		e.tcpServer = nil
	}

	if len(errs) > 0 {
		return fmt.Errorf("dns stop failed: %s", strings.Join(errs, "; "))
	}
	if e.k8sBridge != nil {
		if err := e.k8sBridge.Stop(); err != nil {
			return err
		}
		e.k8sBridge = nil
	}
	if e.geoDriver != nil {
		if err := e.geoDriver.Close(); err != nil {
			return err
		}
		e.geoDriver = nil
	}
	return nil
}

func (e *Engine) initK8sBridge(opts EngineOptions) {
	if e == nil || !opts.K8sEnabled {
		return
	}
	bridge, err := NewK8sBridge(opts.K8sNamespace)
	if err != nil {
		return
	}
	bridge.SetOnChange(func(records []metadata.DNSRecord) {
		e.replaceK8sRecords(records)
	})
	if err := bridge.LoadInitial(context.Background()); err != nil {
		_ = bridge.Stop()
		return
	}
	e.k8sBridge = bridge
}

func (e *Engine) replaceK8sRecords(records []metadata.DNSRecord) {
	if e == nil || e.store == nil {
		return
	}
	e.recordsMu.Lock()
	e.k8sDNS = cloneDNSRecords(records)
	merged := append(cloneDNSRecords(e.baseDNS), cloneDNSRecords(e.k8sDNS)...)
	e.recordsMu.Unlock()
	e.store.Restore(merged)
}

func (e *Engine) serveDNS(w mdns.ResponseWriter, req *mdns.Msg) {
	resp := new(mdns.Msg)
	resp.SetReply(req)

	if len(req.Question) == 0 {
		_ = w.WriteMsg(resp)
		return
	}

	for _, q := range req.Question {
		result, err := e.Lookup(context.Background(), DNSQuestion{
			FQDN:       q.Name,
			RecordType: metadata.DNSRecordType(mdns.TypeToString[q.Qtype]),
		})
		if err != nil {
			resp.Rcode = mdns.RcodeServerFailure
			_ = w.WriteMsg(resp)
			return
		}
		if !result.Found {
			continue
		}
		if e.geoDriver != nil {
			e.geoDriver.ApplyGeoSort(w.RemoteAddr(), result.Records)
		}
		for _, record := range result.Records {
			rr, err := toRR(record)
			if err != nil {
				resp.Rcode = mdns.RcodeServerFailure
				_ = w.WriteMsg(resp)
				return
			}
			resp.Answer = append(resp.Answer, rr...)
		}
	}
	if len(resp.Answer) == 0 {
		resp.Rcode = mdns.RcodeNameError
	}
	_ = w.WriteMsg(resp)
}

func toRR(record metadata.DNSRecord) ([]mdns.RR, error) {
	header := mdns.RR_Header{
		Name:   normalizeFQDN(record.FQDN),
		Rrtype: mdns.StringToType[string(normalizeRecordType(record.RecordType))],
		Class:  mdns.ClassINET,
		Ttl:    record.TTLSeconds,
	}

	values := splitValues(record.ValuesJSON)
	rrs := make([]mdns.RR, 0, len(values))
	for _, value := range values {
		var rr mdns.RR
		switch normalizeRecordType(record.RecordType) {
		case metadata.DNSTypeA:
			ip := net.ParseIP(value).To4()
			if ip == nil {
				return nil, fmt.Errorf("invalid A record value %q", value)
			}
			rr = &mdns.A{Hdr: header, A: ip}
		case metadata.DNSTypeAAAA:
			ip := net.ParseIP(value)
			if ip == nil {
				return nil, fmt.Errorf("invalid AAAA record value %q", value)
			}
			rr = &mdns.AAAA{Hdr: header, AAAA: ip}
		case metadata.DNSTypeCNAME:
			rr = &mdns.CNAME{Hdr: header, Target: normalizeFQDN(value)}
		case metadata.DNSTypeTXT:
			rr = &mdns.TXT{Hdr: header, Txt: []string{value}}
		case metadata.DNSTypeNS:
			rr = &mdns.NS{Hdr: header, Ns: normalizeFQDN(value)}
		case metadata.DNSTypeMX:
			parts := strings.SplitN(value, " ", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid MX record value %q", value)
			}
			preference, err := strconv.Atoi(strings.TrimSpace(parts[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid MX preference %q", parts[0])
			}
			rr = &mdns.MX{Hdr: header, Preference: uint16(preference), Mx: normalizeFQDN(parts[1])}
		case metadata.DNSTypeSRV:
			parts := strings.Fields(value)
			if len(parts) != 4 {
				return nil, fmt.Errorf("invalid SRV record value %q", value)
			}
			priority, err := strconv.Atoi(parts[0])
			if err != nil {
				return nil, err
			}
			weight, err := strconv.Atoi(parts[1])
			if err != nil {
				return nil, err
			}
			port, err := strconv.Atoi(parts[2])
			if err != nil {
				return nil, err
			}
			rr = &mdns.SRV{
				Hdr:      header,
				Priority: uint16(priority),
				Weight:   uint16(weight),
				Port:     uint16(port),
				Target:   normalizeFQDN(parts[3]),
			}
		case metadata.DNSTypeCAA:
			parts := strings.SplitN(value, " ", 3)
			if len(parts) != 3 {
				return nil, fmt.Errorf("invalid CAA record value %q", value)
			}
			flagValue, err := strconv.Atoi(strings.TrimSpace(parts[0]))
			if err != nil {
				return nil, err
			}
			rr = &mdns.CAA{
				Hdr:   header,
				Flag:  uint8(flagValue),
				Tag:   strings.TrimSpace(parts[1]),
				Value: strings.Trim(strings.TrimSpace(parts[2]), `"`),
			}
		default:
			return nil, fmt.Errorf("unsupported record type %q", record.RecordType)
		}
		rrs = append(rrs, rr)
	}
	return rrs, nil
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
