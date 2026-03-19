package k8s_bridge

import (
	"context"

	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

type Config struct {
	Namespace     string
	IngressClass  string
	Enabled       bool
	KubeClient    kubernetes.Interface
	DynamicClient dynamic.Interface
}

// Bridge 聚合 master 侧所有 Kubernetes 监听桥。
// 当前先接入 DNS，Ingress/Gateway 预留到同一生命周期入口。
type Bridge struct {
	DNS            *DNSBridge
	Ingress        *IngressBridge
	Gateway        *GatewayBridge
	OnDnsChange    func(ctx context.Context, dnsID string) error
	OnDomainChange func(ctx context.Context, fqdn string) error
}

func New(cfg Config, repo repository.Repository, onDnsChange func(ctx context.Context, records []metadata.DNSRecord) error, onDomainChange func(ctx context.Context, fqdn string) error) (*Bridge, error) {
	bridge := &Bridge{}
	if cfg.Enabled {
		var dnsBridge *DNSBridge
		var ingressBridge *IngressBridge
		var gatewayBridge *GatewayBridge
		var err error
		if cfg.DynamicClient != nil {
			dnsBridge = NewDNSBridgeWithClient(cfg.Namespace, cfg.DynamicClient, repo, onDnsChange)
			ingressBridge = NewIngressBridgeWithClient(cfg.Namespace, cfg.IngressClass, cfg.KubeClient, repo, onDomainChange)
			gatewayBridge = NewGatewayBridgeWithClient(cfg.Namespace, cfg.DynamicClient, cfg.KubeClient, repo, onDomainChange)
		} else {
			dnsBridge, err = NewDNSBridge(cfg.Namespace, repo, onDnsChange)
			if err != nil {
				return nil, err
			}
			ingressBridge, err = NewIngressBridge(cfg.Namespace, cfg.IngressClass, repo, onDomainChange)
			if err != nil {
				return nil, err
			}
			gatewayBridge, err = NewGatewayBridge(cfg.Namespace, repo, onDomainChange)
			if err != nil {
				return nil, err
			}
		}
		bridge.DNS = dnsBridge
		bridge.Ingress = ingressBridge
		bridge.Gateway = gatewayBridge
		return bridge, nil
	} else {
		return nil, nil
	}
}

func (b *Bridge) LoadInitial(ctx context.Context) error {
	if b == nil {
		return nil
	}
	if b.DNS != nil {
		if err := b.DNS.LoadInitial(ctx); err != nil {
			return err
		}
	}
	if b.Ingress != nil {
		if err := b.Ingress.LoadInitial(ctx); err != nil {
			return err
		}
	}
	if b.Gateway != nil {
		if err := b.Gateway.LoadInitial(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bridge) Listen(ctx context.Context) {
	if b == nil {
		return
	}
	if b.DNS != nil {
		b.DNS.Listen(ctx)
	}
	if b.Ingress != nil {
		b.Ingress.Listen(ctx)
	}
	if b.Gateway != nil {
		b.Gateway.Listen(ctx)
	}
}

func (b *Bridge) Stop() error {
	if b.DNS != nil {
		b.DNS.Stop()
	}
	if b.Ingress != nil {
		b.Ingress.Stop()
	}
	if b.Gateway != nil {
		b.Gateway.Stop()
	}
	return nil
}
