package dns

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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
