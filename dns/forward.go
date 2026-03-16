package dns

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jabberwocky238/luna-edge/repository/metadata"
	mdns "github.com/miekg/dns"
)

var errForwardUnavailable = errors.New("dns forward unavailable")

type ForwarderConfig struct {
	Enabled bool
	Servers []string
	Timeout time.Duration
}

func DefaultForwarderConfig() ForwarderConfig {
	return ForwarderConfig{
		Enabled: false,
		Servers: []string{"1.1.1.1:53"},
		Timeout: 5 * time.Second,
	}
}

type Forwarder struct {
	config ForwarderConfig
	client *mdns.Client
}

func NewForwarder(cfg ForwarderConfig) *Forwarder {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	return &Forwarder{
		config: cfg,
		client: &mdns.Client{
			Net:     "udp",
			Timeout: cfg.Timeout,
		},
	}
}

func (f *Forwarder) Forward(ctx context.Context, req *mdns.Msg) (*mdns.Msg, error) {
	if f == nil || !f.config.Enabled || len(f.config.Servers) == 0 {
		return nil, errForwardUnavailable
	}
	if req == nil {
		return nil, fmt.Errorf("dns request is nil")
	}
	query := req.Copy()
	query.Response = false
	query.Answer = nil
	query.Ns = nil
	query.Extra = nil
	query.Rcode = mdns.RcodeSuccess
	query.RecursionDesired = true

	for _, server := range f.config.Servers {
		server = strings.TrimSpace(server)
		if server == "" {
			continue
		}
		resp, _, err := f.client.ExchangeContext(ctx, query, server)
		if err != nil {
			continue
		}
		if resp == nil {
			continue
		}
		return resp, nil
	}
	return nil, errForwardUnavailable
}

func (f *Forwarder) Lookup(ctx context.Context, question DNSQuestion) (*DNSAnswerSet, error) {
	question = normalizeQuestion(question)
	req := new(mdns.Msg)
	req.SetQuestion(question.FQDN, mdns.StringToType[string(question.RecordType)])
	resp, err := f.Forward(ctx, req)
	if err != nil {
		return nil, err
	}
	records := make([]metadata.DNSRecord, 0, len(resp.Answer))
	for i, answer := range resp.Answer {
		record, ok := rrToRecord(question, i, answer)
		if !ok {
			continue
		}
		records = append(records, record)
	}
	return &DNSAnswerSet{
		Question: question,
		Found:    len(records) > 0,
		Records:  records,
	}, nil
}

func rrToRecord(question DNSQuestion, index int, rr mdns.RR) (metadata.DNSRecord, bool) {
	header := rr.Header()
	record := metadata.DNSRecord{
		ID:         fmt.Sprintf("forward:%s:%s:%d", question.FQDN, question.RecordType, index),
		FQDN:       normalizeFQDN(header.Name),
		RecordType: question.RecordType,
		TTLSeconds: header.Ttl,
		Enabled:    true,
	}
	switch value := rr.(type) {
	case *mdns.A:
		record.ValuesJSON = value.A.String()
	case *mdns.AAAA:
		record.ValuesJSON = value.AAAA.String()
	case *mdns.CNAME:
		record.ValuesJSON = normalizeFQDN(value.Target)
	case *mdns.TXT:
		record.ValuesJSON = strings.Join(value.Txt, ",")
	case *mdns.NS:
		record.ValuesJSON = normalizeFQDN(value.Ns)
	case *mdns.MX:
		record.ValuesJSON = fmt.Sprintf("%d %s", value.Preference, normalizeFQDN(value.Mx))
	case *mdns.SRV:
		record.ValuesJSON = fmt.Sprintf("%d %d %d %s", value.Priority, value.Weight, value.Port, normalizeFQDN(value.Target))
	case *mdns.CAA:
		record.ValuesJSON = fmt.Sprintf("%d %s %s", value.Flag, value.Tag, value.Value)
	default:
		return metadata.DNSRecord{}, false
	}
	return record, true
}
