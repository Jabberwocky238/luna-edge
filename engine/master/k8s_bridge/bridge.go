package k8s_bridge

import (
	"context"

	"github.com/jabberwocky238/luna-edge/repository"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

type Config struct {
	Namespace     string
	IngressClass  string
	EnableDNS     bool
	EnableIngress bool
	EnableGateway bool
	KubeClient    kubernetes.Interface
	DynamicClient dynamic.Interface
}

// Bridge 聚合 master 侧所有 Kubernetes 监听桥。
// 当前先接入 DNS，Ingress/Gateway 预留到同一生命周期入口。
type Bridge struct {
	DNS     *DNSBridge
	Ingress *IngressBridge
	Gateway *GatewayBridge
}

func New(cfg Config, repo repository.Repository, pub publisher) (*Bridge, error) {
	bridge := &Bridge{}
	if cfg.EnableDNS {
		var dnsBridge *DNSBridge
		var err error
		if cfg.DynamicClient != nil {
			dnsBridge = NewDNSBridgeWithClient(cfg.Namespace, cfg.DynamicClient, repo, pub)
		} else {
			dnsBridge, err = NewDNSBridge(cfg.Namespace, repo, pub)
			if err != nil {
				return nil, err
			}
		}
		bridge.DNS = dnsBridge
	}
	if cfg.EnableIngress {
		var ingressBridge *IngressBridge
		var err error
		if cfg.KubeClient != nil {
			ingressBridge = NewIngressBridgeWithClient(cfg.Namespace, cfg.IngressClass, cfg.KubeClient, repo, pub)
		} else {
			ingressBridge, err = NewIngressBridge(cfg.Namespace, cfg.IngressClass, repo, pub)
			if err != nil {
				return nil, err
			}
		}
		bridge.Ingress = ingressBridge
	}
	if cfg.EnableGateway {
		var gatewayBridge *GatewayBridge
		var err error
		if cfg.DynamicClient != nil {
			gatewayBridge = NewGatewayBridgeWithClient(cfg.Namespace, cfg.DynamicClient, repo, pub)
		} else {
			gatewayBridge, err = NewGatewayBridge(cfg.Namespace, repo, pub)
			if err != nil {
				return nil, err
			}
		}
		bridge.Gateway = gatewayBridge
	}
	if bridge.empty() {
		return nil, nil
	}
	return bridge, nil
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

func (b *Bridge) Listen() {
	if b == nil {
		return
	}
	if b.DNS != nil {
		b.DNS.Listen()
	}
	if b.Ingress != nil {
		b.Ingress.Listen()
	}
	if b.Gateway != nil {
		b.Gateway.Listen()
	}
}

func (b *Bridge) Stop() error {
	if b == nil {
		return nil
	}
	var firstErr error
	if b.DNS != nil {
		if err := b.DNS.Stop(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if b.Ingress != nil {
		if err := b.Ingress.Stop(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if b.Gateway != nil {
		if err := b.Gateway.Stop(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (b *Bridge) empty() bool {
	return b == nil || (b.DNS == nil && b.Ingress == nil && b.Gateway == nil)
}
