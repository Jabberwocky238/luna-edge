package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	enginepkg "github.com/jabberwocky238/luna-edge/engine"

	slave "github.com/jabberwocky238/luna-edge/engine/slave"
)

func main() {
	var cfg slave.Config
	var cacheRoot string
	flag.StringVar(&cfg.NodeID, "node-id", envOr("LUNA_NODE_ID", ""), "slave node id")
	flag.StringVar(&cfg.MasterAddress, "master-address", envOr("LUNA_MASTER_ADDRESS", "127.0.0.1:50051"), "master replication address")
	flag.StringVar(&cfg.MasterManageURL, "master-manage-url", envOr("LUNA_MASTER_MANAGE_URL", "http://127.0.0.1:8080"), "master manage http base url")
	flag.BoolVar(&cfg.SubscribeSnapshot, "subscribe-snapshot", envOr("LUNA_SUBSCRIBE_SNAPSHOT", "1") != "0", "request initial snapshot")
	flag.DurationVar(&cfg.RetryMinBackoff, "retry-min-backoff", envDuration("LUNA_RETRY_MIN_BACKOFF", time.Second), "minimum retry backoff")
	flag.DurationVar(&cfg.RetryMaxBackoff, "retry-max-backoff", envDuration("LUNA_RETRY_MAX_BACKOFF", 30*time.Second), "maximum retry backoff")
	flag.StringVar(&cfg.DNSListenAddr, "dns-listen", envOr("LUNA_DNS_LISTEN", ""), "dns listen address")
	flag.BoolVar(&cfg.DNSForwardEnabled, "dns-forward-enabled", envOr("LUNA_DNS_FORWARD_ENABLED", "0") == "1", "enable upstream dns forward on local miss")
	flag.Func("dns-forward-servers", "comma-separated upstream dns servers", func(value string) error {
		cfg.DNSForwardServers = splitCSV(value)
		return nil
	})
	flag.DurationVar(&cfg.DNSForwardTimeout, "dns-forward-timeout", envDuration("LUNA_DNS_FORWARD_TIMEOUT", 5*time.Second), "upstream dns forward timeout")
	flag.BoolVar(&cfg.DNSGeoIPEnabled, "dns-geoip-enabled", envOr("LUNA_DNS_GEOIP_ENABLED", "0") == "1", "enable geoip distance-based sorting for A/AAAA dns records")
	flag.StringVar(&cfg.DNSGeoIPMMDBPath, "dns-geoip-mmdb-path", envOr("LUNA_DNS_GEOIP_MMDB_PATH", ""), "path to maxmind city mmdb file for dns geoip sorting")
	flag.BoolVar(&cfg.DNSK8sEnabled, "dns-k8s-enabled", envOr("LUNA_DNS_K8S_ENABLED", "0") == "1", "enable kubernetes DnsDomainRecord CRD bridge")
	flag.StringVar(&cfg.DNSK8sNamespace, "dns-k8s-namespace", envOr("LUNA_DNS_K8S_NAMESPACE", enginepkg.POD_NAMESPACE), "kubernetes namespace watched for DnsDomainRecord CRDs")
	flag.StringVar(&cfg.IngressK8sNS, "ingress-k8s-namespace", envOr("LUNA_INGRESS_K8S_NAMESPACE", ""), "kubernetes ingress namespace")
	flag.StringVar(&cfg.IngressK8sClass, "ingress-k8s-class", envOr("LUNA_INGRESS_K8S_CLASS", "luna-edge"), "kubernetes ingress class handled by luna-edge")
	flag.IntVar(&cfg.IngressLRUSize, "ingress-lru-size", envInt("LUNA_INGRESS_LRU_SIZE", 4096), "ingress tls cert LRU size")
	flag.StringVar(&cfg.HealthListenAddr, "health-listen", envOr("LUNA_HEALTH_LISTEN", ":50050"), "health HTTP listen address")
	flag.StringVar(&cacheRoot, "cache-root", envOr("LUNA_CACHE_ROOT", ""), "slave cache root")
	flag.Parse()
	if strings.TrimSpace(cacheRoot) == "" {
		log.Fatal("cache root is required, set --cache-root or LUNA_CACHE_ROOT")
	}
	cfg.IngressHTTPAddr = ":80"
	cfg.IngressTLSAddr = ":443"
	cfg.IngressK8sEnabled = true
	if len(cfg.DNSForwardServers) == 0 {
		cfg.DNSForwardServers = splitCSV(envOr("LUNA_DNS_FORWARD_SERVERS", "1.1.1.1:53"))
	}

	store, err := slave.NewLocalStore(cacheRoot)
	if err != nil {
		log.Fatalf("create slave store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			log.Printf("close slave store: %v", err)
		}
	}()

	engine, err := slave.New(cfg, cacheRoot, store, store)
	if err != nil {
		log.Fatalf("create slave: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("slave started: node=%s master=%s", cfg.NodeID, cfg.MasterAddress)
	if err := engine.Start(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("run slave: %v", err)
	}
	if err := engine.Stop(context.Background()); err != nil {
		log.Fatalf("stop slave: %v", err)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if value := os.Getenv(key); value != "" {
		if n, err := strconv.Atoi(value); err == nil {
			return n
		}
	}
	return fallback
}

func splitCSV(value string) []string {
	raw := strings.Split(value, ",")
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}
