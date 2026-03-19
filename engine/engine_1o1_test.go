package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	enginepkg "github.com/jabberwocky238/luna-edge/engine"
	masterpkg "github.com/jabberwocky238/luna-edge/engine/master"
	masterk8s "github.com/jabberwocky238/luna-edge/engine/master/k8s_bridge"
	slavepkg "github.com/jabberwocky238/luna-edge/engine/slave"
	"github.com/jabberwocky238/luna-edge/replication"
	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/jabberwocky238/luna-edge/repository/connection"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func TestEngine1o1ReplicationAndCatchUp(t *testing.T) {
	t.Run("fakekube", func(t *testing.T) {
		tc := newEngineTestCluster(t, 1)
		defer tc.Close()

		tc.StartSlave(t, 0)
		tc.UpsertIngress(t, "demo.example.com", "svc-a", 8080, 1)
		tc.RequireEventually(t, func() error {
			return tc.AssertAllSlavesHaveRoute("demo.example.com", "svc-a", 8080)
		})

		tc.StopSlave(t, 0)
		tc.UpsertIngress(t, "demo.example.com", "svc-b", 8081, 2)
		tc.UpsertIngress(t, "demo.example.com", "svc-c", 8082, 3)

		tc.StartSlave(t, 0)
		tc.RequireEventually(t, func() error {
			return tc.AssertAllSlavesHaveRoute("demo.example.com", "svc-c", 8082)
		})
	})

	t.Run("certificates", func(t *testing.T) {
		tc := newEngineTestCluster(t, 1)
		defer tc.Close()

		tc.UpsertDomainBase(t, "certs.example.com")
		tc.StartSlave(t, 0)

		tc.UpsertCertificateAndBroadcast(t, "certs.example.com", 1)
		tc.RequireEventually(t, func() error {
			return tc.AssertAllSlavesHaveCertificate("certs.example.com", 1)
		})

		tc.StopSlave(t, 0)
		tc.UpsertCertificateAndBroadcast(t, "certs.example.com", 2)
		tc.UpsertCertificateAndBroadcast(t, "certs.example.com", 3)

		tc.StartSlave(t, 0)
		tc.RequireEventually(t, func() error {
			return tc.AssertAllSlavesHaveCertificate("certs.example.com", 3)
		})
	})

	t.Run("dns_records", func(t *testing.T) {
		tc := newEngineTestCluster(t, 1)
		defer tc.Close()

		tc.StartSlave(t, 0)
		tc.UpsertDNSAndBroadcast(t, metadata.DNSRecord{
			ID:           "dns-1",
			FQDN:         "one.example.com",
			RecordType:   metadata.DNSTypeA,
			RoutingClass: metadata.RoutingClassFirst,
			TTLSeconds:   60,
			ValuesJSON:   `["1.1.1.1"]`,
			Enabled:      true,
		})
		tc.RequireEventually(t, func() error {
			return tc.AssertAllSlavesHaveDNSRecord("one.example.com", `["1.1.1.1"]`)
		})

		tc.StopSlave(t, 0)
		tc.UpsertDNSAndBroadcast(t, metadata.DNSRecord{
			ID:           "dns-2",
			FQDN:         "two.example.com",
			RecordType:   metadata.DNSTypeA,
			RoutingClass: metadata.RoutingClassFirst,
			TTLSeconds:   60,
			ValuesJSON:   `["2.2.2.2"]`,
			Enabled:      true,
		})
		tc.UpsertDNSAndBroadcast(t, metadata.DNSRecord{
			ID:           "dns-3",
			FQDN:         "three.example.com",
			RecordType:   metadata.DNSTypeA,
			RoutingClass: metadata.RoutingClassFirst,
			TTLSeconds:   60,
			ValuesJSON:   `["3.3.3.3"]`,
			Enabled:      true,
		})

		tc.StartSlave(t, 0)
		tc.RequireEventually(t, func() error {
			if err := tc.AssertAllSlavesHaveDNSRecord("one.example.com", `["1.1.1.1"]`); err != nil {
				return err
			}
			if err := tc.AssertAllSlavesHaveDNSRecord("two.example.com", `["2.2.2.2"]`); err != nil {
				return err
			}
			return tc.AssertAllSlavesHaveDNSRecord("three.example.com", `["3.3.3.3"]`)
		})
	})
}

type engineTestCluster struct {
	root       string
	master     *masterpkg.Engine
	masterCtx  context.Context
	cancel     context.CancelFunc
	masterDone chan error
	bridgeRepo repository.Factory
	kubeClient *kubefake.Clientset
	dynClient  *dynamicfake.FakeDynamicClient
	slaves     []*slaveNode
}

type slaveNode struct {
	cacheRoot string
	engine    *slavepkg.Engine
	cancel    context.CancelFunc
	doneCh    chan error
}

type memoryBundleProvider struct {
	mu      sync.RWMutex
	bundles map[string]*replication.CertificateBundle
}

func newEngineTestCluster(t *testing.T, slaveCount int) *engineTestCluster {
	t.Helper()

	root := t.TempDir()
	dbPath := filepath.Join(root, "master.db")
	replicationAddr := reserveTCPAddr(t)
	masterEngine, err := masterpkg.New("masternode", &masterpkg.Config{
		StorageDriver:         connection.DriverSQLite,
		SQLitePath:            dbPath,
		AutoMigrate:           true,
		K8sBridgeEnabled:      true,
		K8sNamespace:          "default",
		K8sIngressClass:       "luna-edge",
		ReplicationListenAddr: replicationAddr,
	})
	if err != nil {
		t.Fatalf("new master engine: %v", err)
	}

	bridgeFactory := repository.NewFactory(connection.Config{
		Driver:      connection.DriverSQLite,
		Path:        dbPath,
		AutoMigrate: true,
	})
	if err := bridgeFactory.Start(); err != nil {
		t.Fatalf("start bridge repo: %v", err)
	}
	bundles := &memoryBundleProvider{bundles: map[string]*replication.CertificateBundle{}}
	k8sBridge, kubeClient, dynClient := newFakeK8sBridge(t, bridgeFactory.Repository(), masterEngine)
	masterEngine.Bundles = bundles
	masterEngine.K8sBridge = k8sBridge

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- masterEngine.Start(ctx)
	}()
	waitForTCP(t, replicationAddr)
	waitForMasterRepo(t, masterEngine)

	tc := &engineTestCluster{
		root:       root,
		master:     masterEngine,
		masterCtx:  ctx,
		cancel:     cancel,
		masterDone: doneCh,
		bridgeRepo: bridgeFactory,
		kubeClient: kubeClient,
		dynClient:  dynClient,
	}
	for i := 0; i < slaveCount; i++ {
		tc.slaves = append(tc.slaves, &slaveNode{
			cacheRoot: filepath.Join(root, fmt.Sprintf("slave-%d", i)),
		})
	}
	return tc
}

func newFakeK8sBridge(t *testing.T, repo repository.Repository, masterEngine *masterpkg.Engine) (*masterk8s.Bridge, *kubefake.Clientset, *dynamicfake.FakeDynamicClient) {
	t.Helper()

	kubeClient := kubefake.NewSimpleClientset()
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}:        "GatewayList",
		{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}:      "HTTPRouteList",
		{Group: "gateway.networking.k8s.io", Version: "v1alpha2", Resource: "tlsroutes"}: "TLSRouteList",
	})
	onDomainChange := func(ctx context.Context, fqdn string) error {
		return masterEngine.BoardcastDomainEndpointProjection(ctx, fqdn)
	}
	ingressBridge := masterk8s.NewIngressBridgeWithClient("default", "luna-edge", kubeClient, repo, onDomainChange)
	gatewayBridge := masterk8s.NewGatewayBridgeWithClient("default", dynamicClient, repo, onDomainChange)
	return &masterk8s.Bridge{
		Ingress:        ingressBridge,
		Gateway:        gatewayBridge,
		OnDomainChange: onDomainChange,
	}, kubeClient, dynamicClient
}

func (tc *engineTestCluster) Close() {
	for i := range tc.slaves {
		tc.stopSlave(i)
	}
	if tc.cancel != nil {
		tc.cancel()
	}
	if tc.masterDone != nil {
		err := <-tc.masterDone
		if err != nil && !errors.Is(err, context.Canceled) {
			panic(err)
		}
	}
	if tc.bridgeRepo != nil {
		_ = tc.bridgeRepo.Close()
	}
}

func (tc *engineTestCluster) StartSlave(t *testing.T, idx int) {
	t.Helper()
	node := tc.slaves[idx]
	if node.engine != nil {
		return
	}
	eng, err := slavepkg.New("slavenode"+strconv.Itoa(idx), &slavepkg.Config{
		CacheRoot:       node.cacheRoot,
		MasterAddress:   tc.master.Config.ReplicationListenAddr,
		RetryMinBackoff: 20 * time.Millisecond,
		RetryMaxBackoff: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new slave %d: %v", idx, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- eng.Start(ctx)
	}()
	node.engine = eng
	node.cancel = cancel
	node.doneCh = doneCh
}

func (tc *engineTestCluster) StopSlave(t *testing.T, idx int) {
	t.Helper()
	tc.stopSlave(idx)
}

func (tc *engineTestCluster) stopSlave(idx int) {
	node := tc.slaves[idx]
	if node.engine == nil {
		return
	}
	node.cancel()
	err := <-node.doneCh
	if err != nil && !errors.Is(err, context.Canceled) {
		panic(err)
	}
	node.engine = nil
	node.cancel = nil
	node.doneCh = nil
}

func (tc *engineTestCluster) UpsertIngress(t *testing.T, host, service string, port int32, generation int64) {
	t.Helper()
	className := "luna-edge"
	pathType := networkingv1.PathTypePrefix
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  "default",
			Name:       "demo",
			Generation: generation,
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &className,
			TLS:              []networkingv1.IngressTLS{{Hosts: []string{host}}},
			Rules: []networkingv1.IngressRule{{
				Host: host,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Path:     "/",
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: service,
									Port: networkingv1.ServiceBackendPort{Number: port},
								},
							},
						}},
					},
				},
			}},
		},
	}
	current, err := tc.kubeClient.NetworkingV1().Ingresses("default").Get(context.Background(), "demo", metav1.GetOptions{})
	if err == nil && current != nil {
		ing.ResourceVersion = current.ResourceVersion
		if _, err := tc.kubeClient.NetworkingV1().Ingresses("default").Update(context.Background(), ing, metav1.UpdateOptions{}); err != nil {
			t.Fatalf("update ingress: %v", err)
		}
		return
	}
	if _, err := tc.kubeClient.NetworkingV1().Ingresses("default").Create(context.Background(), ing, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create ingress: %v", err)
	}
}

func (tc *engineTestCluster) UpsertGatewayHTTPRoute(t *testing.T, host, service string, port int64, generation int64) {
	t.Helper()
	ctx := context.Background()
	gateway := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       "Gateway",
		"metadata": map[string]interface{}{
			"name":       "demo-gateway",
			"namespace":  "default",
			"generation": generation,
		},
		"spec": map[string]interface{}{
			"gatewayClassName": "luna-edge",
			"listeners": []interface{}{
				map[string]interface{}{
					"name":     "https",
					"hostname": host,
					"port":     int64(443),
					"protocol": "HTTPS",
				},
			},
		},
	}}
	httpRoute := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       "HTTPRoute",
		"metadata": map[string]interface{}{
			"name":       "demo-route",
			"namespace":  "default",
			"generation": generation,
		},
		"spec": map[string]interface{}{
			"hostnames": []interface{}{host},
			"parentRefs": []interface{}{
				map[string]interface{}{
					"name": "demo-gateway",
				},
			},
			"rules": []interface{}{
				map[string]interface{}{
					"matches": []interface{}{
						map[string]interface{}{
							"path": map[string]interface{}{
								"type":  "PathPrefix",
								"value": "/",
							},
						},
					},
					"backendRefs": []interface{}{
						map[string]interface{}{
							"name": service,
							"port": port,
						},
					},
				},
			},
		},
	}}
	gatewayGVR := schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}
	httpRouteGVR := schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}
	if current, err := tc.dynClient.Resource(gatewayGVR).Namespace("default").Get(ctx, "demo-gateway", metav1.GetOptions{}); err == nil && current != nil {
		gateway.SetResourceVersion(current.GetResourceVersion())
		if _, err := tc.dynClient.Resource(gatewayGVR).Namespace("default").Update(ctx, gateway, metav1.UpdateOptions{}); err != nil {
			t.Fatalf("update gateway: %v", err)
		}
	} else if _, err := tc.dynClient.Resource(gatewayGVR).Namespace("default").Create(ctx, gateway, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create gateway: %v", err)
	}
	if current, err := tc.dynClient.Resource(httpRouteGVR).Namespace("default").Get(ctx, "demo-route", metav1.GetOptions{}); err == nil && current != nil {
		httpRoute.SetResourceVersion(current.GetResourceVersion())
		if _, err := tc.dynClient.Resource(httpRouteGVR).Namespace("default").Update(ctx, httpRoute, metav1.UpdateOptions{}); err != nil {
			t.Fatalf("update httproute: %v", err)
		}
	} else if _, err := tc.dynClient.Resource(httpRouteGVR).Namespace("default").Create(ctx, httpRoute, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create httproute: %v", err)
	}
}

func (tc *engineTestCluster) UpsertDomainBase(t *testing.T, hostname string) {
	t.Helper()
	repo := tc.master.Repo
	domain := &metadata.DomainEndpoint{
		ID:          "domain:" + hostname,
		Hostname:    hostname,
		NeedCert:    true,
		BackendType: metadata.BackendTypeL7HTTPS,
	}
	backend := &metadata.ServiceBackendRef{
		ID:               "backend:" + hostname,
		ServiceNamespace: "default",
		ServiceName:      "svc-" + hostname,
		ServicePort:      8443,
	}
	route := &metadata.HTTPRoute{
		ID:               "route:" + hostname,
		DomainEndpointID: domain.ID,
		Path:             "/",
		Priority:         1,
		BackendRefID:     backend.ID,
	}
	if err := repo.DomainEndpoints().UpsertResource(context.Background(), domain); err != nil {
		t.Fatalf("upsert domain: %v", err)
	}
	if err := repo.ServiceBindingRefs().UpsertResource(context.Background(), backend); err != nil {
		t.Fatalf("upsert backend: %v", err)
	}
	if err := repo.HTTPRoutes().UpsertResource(context.Background(), route); err != nil {
		t.Fatalf("upsert route: %v", err)
	}
	if err := tc.master.BoardcastDomainEndpointProjection(context.Background(), hostname); err != nil {
		t.Fatalf("broadcast domain base: %v", err)
	}
}

func (tc *engineTestCluster) UpsertCertificateAndBroadcast(t *testing.T, hostname string, revision uint64) {
	t.Helper()
	repo := tc.master.Repo
	domain, err := repo.GetDomainEndpointByHostname(context.Background(), hostname)
	if err != nil {
		t.Fatalf("get domain by hostname: %v", err)
	}
	cert := &metadata.CertificateRevision{
		ID:               fmt.Sprintf("cert:%s:%d", hostname, revision),
		DomainEndpointID: domain.ID,
		Revision:         revision,
		Provider:         metadata.ProviderLetsEncrypt,
		ChallengeType:    metadata.ChallengeTypeHTTP01,
		ArtifactBucket:   "test",
		ArtifactPrefix:   fmt.Sprintf("certs/%s/%d", hostname, revision),
		SHA256Crt:        fmt.Sprintf("crt-%d", revision),
		SHA256Key:        fmt.Sprintf("key-%d", revision),
		NotBefore:        time.Now().UTC().Add(-time.Hour),
		NotAfter:         time.Now().UTC().Add(24 * time.Hour),
	}
	if err := repo.CertificateRevisions().UpsertResource(context.Background(), cert); err != nil {
		t.Fatalf("upsert certificate: %v", err)
	}
	if err := tc.master.Bundles.PutCertificateBundle(context.Background(), &replication.CertificateBundle{
		Hostname:     hostname,
		Revision:     revision,
		TLSCrt:       []byte(fmt.Sprintf("crt-%d", revision)),
		TLSKey:       []byte(fmt.Sprintf("key-%d", revision)),
		MetadataJSON: []byte(fmt.Sprintf(`{"hostname":"%s","revision":%d}`, hostname, revision)),
	}); err != nil {
		t.Fatalf("put certificate bundle: %v", err)
	}
	if err := tc.master.BoardcastDomainEndpointProjection(context.Background(), hostname); err != nil {
		t.Fatalf("broadcast certificate projection: %v", err)
	}
}

func (tc *engineTestCluster) UpsertDNSAndBroadcast(t *testing.T, record metadata.DNSRecord) {
	t.Helper()
	if err := tc.master.Repo.DNSRecords().UpsertResource(context.Background(), &record); err != nil {
		t.Fatalf("upsert dns record: %v", err)
	}
	if err := tc.master.BoardcastDNSRecord(context.Background(), record.ID); err != nil {
		t.Fatalf("broadcast dns record: %v", err)
	}
}

func (tc *engineTestCluster) RequireEventually(t *testing.T, check func() error) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := check(); err == nil {
			return
		} else {
			lastErr = err
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal(lastErr)
}

func (tc *engineTestCluster) AssertAllSlavesHaveRoute(hostname, service string, port uint32) error {
	for i := range tc.slaves {
		store, err := tc.openStore(i)
		if err != nil {
			return err
		}
		entry, err := store.GetDomainEntryByHostname(context.Background(), hostname)
		_ = store.Close()
		if err != nil {
			return fmt.Errorf("slave %d get route: %w", i, err)
		}
		if len(entry.HTTPRoutes) != 1 || entry.HTTPRoutes[0].BackendRef == nil {
			return fmt.Errorf("slave %d route projection invalid", i)
		}
		if entry.HTTPRoutes[0].BackendRef.ServiceName != service || entry.HTTPRoutes[0].BackendRef.ServicePort != port {
			return fmt.Errorf("slave %d route backend=%s:%d want=%s:%d", i, entry.HTTPRoutes[0].BackendRef.ServiceName, entry.HTTPRoutes[0].BackendRef.ServicePort, service, port)
		}
	}
	return nil
}

func (tc *engineTestCluster) AssertAllSlavesHaveCertificate(hostname string, revision uint64) error {
	for i := range tc.slaves {
		store, err := tc.openStore(i)
		if err != nil {
			return err
		}
		entry, err := store.GetDomainEntryByHostname(context.Background(), hostname)
		if err != nil {
			_ = store.Close()
			return fmt.Errorf("slave %d get certificate projection: %w", i, err)
		}
		if entry.Cert == nil || entry.Cert.Revision != revision {
			_ = store.Close()
			return fmt.Errorf("slave %d certificate revision=%v want=%d", i, entry.Cert, revision)
		}
		ok, err := store.CheckCertificateBundle(context.Background(), hostname, revision)
		_ = store.Close()
		if err != nil {
			return fmt.Errorf("slave %d check bundle: %w", i, err)
		}
		if !ok {
			return fmt.Errorf("slave %d bundle missing hostname=%s revision=%d", i, hostname, revision)
		}
	}
	return nil
}

func (tc *engineTestCluster) AssertAllSlavesHaveDNSRecord(hostname, wantValues string) error {
	for i := range tc.slaves {
		store, err := tc.openStore(i)
		if err != nil {
			return err
		}
		records, err := store.GetDNSRecordsByHostname(context.Background(), hostname)
		_ = store.Close()
		if err != nil {
			return fmt.Errorf("slave %d get dns record: %w", i, err)
		}
		if len(records) != 1 {
			return fmt.Errorf("slave %d dns count=%d want=1", i, len(records))
		}
		if !jsonValuesEqual(records[0].ValuesJSON, wantValues) {
			return fmt.Errorf("slave %d dns values=%s want=%s", i, records[0].ValuesJSON, wantValues)
		}
	}
	return nil
}

func (tc *engineTestCluster) openStore(idx int) (*slavepkg.LocalStore, error) {
	srcRoot := tc.slaves[idx].cacheRoot
	inspectRoot, err := os.MkdirTemp(tc.root, fmt.Sprintf("inspect-slave-%d-", idx))
	if err != nil {
		return nil, err
	}
	if err := copyDir(srcRoot, inspectRoot); err != nil {
		return nil, err
	}
	store, err := slavepkg.NewLocalStore(inspectRoot, nil, make(chan []metadata.DNSRecord, 1))
	if err != nil {
		return nil, err
	}
	if err := store.Start(); err != nil {
		return nil, err
	}
	return store, nil
}

func copyDir(srcRoot, dstRoot string) error {
	return filepath.Walk(srcRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dstRoot, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()
		dstFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer dstFile.Close()
		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}

func (m *memoryBundleProvider) FetchCertificateBundle(_ context.Context, hostname string, revision uint64) (*replication.CertificateBundle, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item := m.bundles[fmt.Sprintf("%s#%d", hostname, revision)]
	if item == nil {
		return nil, fmt.Errorf("bundle not found hostname=%s revision=%d", hostname, revision)
	}
	out := *item
	out.TLSCrt = append([]byte(nil), item.TLSCrt...)
	out.TLSKey = append([]byte(nil), item.TLSKey...)
	out.MetadataJSON = append([]byte(nil), item.MetadataJSON...)
	return &out, nil
}

func (m *memoryBundleProvider) PutCertificateBundle(_ context.Context, bundle *replication.CertificateBundle) error {
	if bundle == nil {
		return fmt.Errorf("bundle is nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := *bundle
	out.TLSCrt = append([]byte(nil), bundle.TLSCrt...)
	out.TLSKey = append([]byte(nil), bundle.TLSKey...)
	out.MetadataJSON = append([]byte(nil), bundle.MetadataJSON...)
	m.bundles[fmt.Sprintf("%s#%d", bundle.Hostname, bundle.Revision)] = &out
	return nil
}

func reserveTCPAddr(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve tcp addr: %v", err)
	}
	addr := lis.Addr().String()
	_ = lis.Close()
	return addr
}

func waitForTCP(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("master tcp listener not ready: %s", addr)
}

func waitForMasterRepo(t *testing.T, masterEngine *masterpkg.Engine) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if masterEngine.Repo != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("master repository not ready")
}

func jsonValuesEqual(a, b string) bool {
	var av []string
	var bv []string
	if err := json.Unmarshal([]byte(a), &av); err != nil {
		return false
	}
	if err := json.Unmarshal([]byte(b), &bv); err != nil {
		return false
	}
	if len(av) != len(bv) {
		return false
	}
	for i := range av {
		if av[i] != bv[i] {
			return false
		}
	}
	return true
}

func init() {
	_ = os.Setenv("POD_IP", "127.0.0.1")
	_ = os.Setenv("POD_NAMESPACE", "default")
	_ = os.Setenv("POD_NAME", "master-test")
	enginepkg.POD_IP = "127.0.0.1"
	enginepkg.POD_NAMESPACE = "default"
	enginepkg.POD_NAME = "master-test"
}
