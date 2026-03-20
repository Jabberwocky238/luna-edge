package dns

import (
	"context"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
	mdns "github.com/miekg/dns"
)

const dnsColorPrefix = "\033[1;36m[DNS]\033[0m "

func dnsLogf(format string, args ...any) {
	log.Printf(dnsColorPrefix+format, args...)
}

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
	ctx       context.Context
	k8sLoaded bool
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

func (e *Engine) BindContext(ctx context.Context) error {
	if e == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	e.ctx = ctx
	if e.k8sBridge != nil && !e.k8sLoaded {
		if err := e.k8sBridge.LoadInitial(ctx); err != nil {
			return err
		}
		e.k8sLoaded = true
	}
	return nil
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

	dnsLogf("starting dns listeners addr=%q udp=%t tcp=%t k8s_bridge=%t forward_enabled=%t forward_servers=%v", listenAddr, true, true, e.k8sBridge != nil, e.forwarder != nil && e.forwarder.config.Enabled, forwardServers(e.forwarder))

	go func() {
		dnsLogf("udp listener serving addr=%q", listenAddr)
		if err := e.udpServer.ListenAndServe(); err != nil {
			dnsLogf("udp listener stopped addr=%q err=%v", listenAddr, err)
		}
	}()
	go func() {
		dnsLogf("tcp listener serving addr=%q", listenAddr)
		if err := e.tcpServer.ListenAndServe(); err != nil {
			dnsLogf("tcp listener stopped addr=%q err=%v", listenAddr, err)
		}
	}()
	if e.k8sBridge != nil {
		e.k8sBridge.Listen(e.runtimeContext())
	}

	return nil
}

// Stop 停止已经启动的 DNS 监听。
func (e *Engine) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	dnsLogf("stopping dns listeners")

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
		dnsLogf("received dns request without question proto=%s remote=%q", dnsNetworkOf(w), dnsRemoteAddr(w))
		_ = w.WriteMsg(resp)
		return
	}

	dnsLogf("received dns query id=%d proto=%s remote=%q questions=%s", req.Id, dnsNetworkOf(w), dnsRemoteAddr(w), dnsQuestionsSummary(req.Question))

	for _, q := range req.Question {
		result, err := e.Lookup(e.runtimeContext(), DNSQuestion{
			FQDN:       q.Name,
			RecordType: metadata.DNSRecordType(mdns.TypeToString[q.Qtype]),
		})
		if err != nil {
			dnsLogf("lookup failed id=%d proto=%s remote=%q question=%s/%s err=%v", req.Id, dnsNetworkOf(w), dnsRemoteAddr(w), normalizeFQDN(q.Name), mdns.TypeToString[q.Qtype], err)
			resp.Rcode = mdns.RcodeServerFailure
			_ = w.WriteMsg(resp)
			return
		}
		if !result.Found {
			dnsLogf("lookup miss id=%d proto=%s remote=%q question=%s/%s", req.Id, dnsNetworkOf(w), dnsRemoteAddr(w), normalizeFQDN(q.Name), mdns.TypeToString[q.Qtype])
			continue
		}
		if e.geoDriver != nil {
			e.geoDriver.ApplyGeoSort(w.RemoteAddr(), result.Records)
		}
		dnsLogf("lookup hit id=%d proto=%s remote=%q question=%s/%s answers=%d", req.Id, dnsNetworkOf(w), dnsRemoteAddr(w), normalizeFQDN(q.Name), mdns.TypeToString[q.Qtype], len(result.Records))
		for _, record := range result.Records {
			rr, err := toRR(record)
			if err != nil {
				dnsLogf("record rendering failed id=%d proto=%s remote=%q record=%s/%s err=%v", req.Id, dnsNetworkOf(w), dnsRemoteAddr(w), record.FQDN, record.RecordType, err)
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
	dnsLogf("sending dns response id=%d proto=%s remote=%q rcode=%s answers=%d", req.Id, dnsNetworkOf(w), dnsRemoteAddr(w), mdns.RcodeToString[resp.Rcode], len(resp.Answer))
	_ = w.WriteMsg(resp)
}

func (e *Engine) runtimeContext() context.Context {
	if e != nil && e.ctx != nil {
		return e.ctx
	}
	return context.Background()
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

func dnsNetworkOf(w mdns.ResponseWriter) string {
	if w == nil || w.RemoteAddr() == nil {
		return "unknown"
	}
	return w.RemoteAddr().Network()
}

func dnsRemoteAddr(w mdns.ResponseWriter) string {
	if w == nil || w.RemoteAddr() == nil {
		return ""
	}
	return w.RemoteAddr().String()
}

func dnsQuestionsSummary(questions []mdns.Question) string {
	if len(questions) == 0 {
		return ""
	}
	parts := make([]string, 0, len(questions))
	for _, q := range questions {
		parts = append(parts, normalizeFQDN(q.Name)+"/"+mdns.TypeToString[q.Qtype])
	}
	return strings.Join(parts, ",")
}

func forwardServers(f *Forwarder) []string {
	if f == nil {
		return nil
	}
	out := make([]string, 0, len(f.config.Servers))
	for _, server := range f.config.Servers {
		server = strings.TrimSpace(server)
		if server == "" {
			continue
		}
		out = append(out, server)
	}
	return out
}
