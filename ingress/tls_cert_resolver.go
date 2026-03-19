package ingress

import (
	"container/list"
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// DefaultIngressLRUSize 是 ingress 运行期 LRU 的默认大小。
const DefaultIngressLRUSize = 4096

// TLSCertResolver 定义 TLS 证书解析行为。
type TLSCertResolver interface {
	Start() error
	Load(hostname string) (*tls.Certificate, error)
}

// LunaTLSCertResolver 是默认完整实现。
//
// 它支持：
// - 内存 LRU 缓存
// - 从本地 cert root 读取证书
// - 文件系统 watcher 仅用于让缓存失效
// - 测试时直接把证书预热到内存
//
// 注意：
// - watcher 的唯一职责是让缓存失效
// - watcher 不负责预热缓存
// - watcher 不负责主动加载或重载证书
// - 证书只会在 Load 被调用时按需从磁盘读取
type LunaTLSCertResolver struct {
	certRoot string
	certs    *certLRU
	mu       sync.RWMutex
	watchMu  sync.Mutex
	watcher  *certWatcher
}

// NewLunaTLSCertResolver 创建默认 resolver。
func NewLunaTLSCertResolver(certRoot string, lruSize int) *LunaTLSCertResolver {
	resolver := &LunaTLSCertResolver{
		certRoot: certRoot,
		certs:    newCertLRU(resolveCertLRUSize(lruSize)),
	}
	return resolver
}

func (r *LunaTLSCertResolver) Start() error {
	return r.restartWatcher(r.certRoot)
}

func (r *LunaTLSCertResolver) Delete(hostname string) {
	if r == nil {
		return
	}
	r.certs.Delete(normalizeHost(hostname))
}

func (r *LunaTLSCertResolver) DeleteCandidates(hostname string) {
	if r == nil {
		return
	}
	for _, candidate := range certificateCandidates(hostname) {
		r.certs.Delete(candidate)
	}
}

func (r *LunaTLSCertResolver) Clear() {
	if r == nil {
		return
	}
	r.certs.Clear()
}

// Put 允许测试或其他调用方直接把证书预热到内存 LRU 中。
func (r *LunaTLSCertResolver) Put(hostname string, cert *tls.Certificate) error {
	hostname = sanitizeHostname(hostname)
	if hostname == "" {
		return fmt.Errorf("hostname is required")
	}
	if cert == nil {
		return fmt.Errorf("certificate is required")
	}
	r.certs.Add(hostname, cert)
	return nil
}

// Load 返回某个 hostname 当前可用的证书，优先命中内存 LRU，再从本地路径读取。
func (r *LunaTLSCertResolver) Load(hostname string) (*tls.Certificate, error) {
	hostname = sanitizeHostname(hostname)
	if hostname == "" {
		return nil, fmt.Errorf("hostname is required")
	}
	for _, candidate := range certificateCandidates(hostname) {
		if cert, ok := r.certs.Get(candidate); ok {
			return cert, nil
		}
		for _, pair := range r.lookupPaths(candidate) {
			if !fileExists(pair[0]) || !fileExists(pair[1]) {
				continue
			}
			cert, err := tls.LoadX509KeyPair(pair[0], pair[1])
			if err != nil {
				return nil, err
			}
			r.certs.Add(candidate, &cert)
			return &cert, nil
		}
	}
	return nil, fmt.Errorf("certificate not found for hostname %q", hostname)
}

func (r *LunaTLSCertResolver) lookupPaths(hostname string) [][2]string {
	dirname := certificateDirectoryName(hostname)
	r.mu.RLock()
	certRoot := r.certRoot
	r.mu.RUnlock()
	return [][2]string{
		{
			filepath.Join(certRoot, dirname, "tls.crt"),
			filepath.Join(certRoot, dirname, "tls.key"),
		},
	}
}

func certificateCandidates(hostname string) []string {
	parts := strings.Split(sanitizeHostname(hostname), ".")
	if len(parts) == 0 || parts[0] == "" {
		return nil
	}
	candidates := []string{strings.Join(parts, ".")}
	for i := 1; i < len(parts)-1; i++ {
		candidates = append(candidates, "*."+strings.Join(parts[i:], "."))
	}
	return candidates
}

func certificateDirectoryName(hostname string) string {
	hostname = sanitizeHostname(hostname)
	if !strings.HasPrefix(hostname, "*.") {
		return hostname
	}
	return filepath.Join(".wildcard", strings.TrimPrefix(hostname, "*."))
}

// CertificateDirectoryName returns the filesystem-safe directory name for a hostname.
func CertificateDirectoryName(hostname string) string {
	return certificateDirectoryName(hostname)
}

func hostnameFromCertificatePath(certRoot, changedPath string) string {
	certRoot = filepath.Clean(certRoot)
	changedPath = filepath.Clean(changedPath)

	rel, err := filepath.Rel(certRoot, changedPath)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) == 0 {
		return ""
	}
	if parts[0] == ".wildcard" {
		if len(parts) < 2 {
			return ""
		}
		return sanitizeHostname("*." + parts[1])
	}
	return sanitizeHostname(parts[0])
}

func (r *LunaTLSCertResolver) restartWatcher(certRoot string) error {
	r.watchMu.Lock()
	defer r.watchMu.Unlock()

	if r.watcher != nil {
		r.watcher.close()
		r.watcher = nil
	}
	if strings.TrimSpace(certRoot) == "" {
		return errors.New("certificate root is required to start watcher")
	}
	info, err := os.Stat(certRoot)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("certificate root %q is not a directory", certRoot)
	}

	watcher, err := newCertWatcher(certRoot, r.invalidateCacheForPaths)
	if err != nil {
		return err
	}
	r.watcher = watcher
	return nil
}

// invalidateCacheForPaths 仅做定向缓存失效。
//
// 它不会扫描证书目录，也不会主动加载任何证书；
// watcher 收到文件系统事件后，只删除受影响 hostname 的缓存条目，
// 让后续 Load 针对这些 hostname 重新走磁盘读取。
func (r *LunaTLSCertResolver) invalidateCacheForPaths(paths []string) {
	if r == nil {
		return
	}
	r.mu.RLock()
	certRoot := r.certRoot
	r.mu.RUnlock()
	for _, path := range paths {
		hostname := hostnameFromCertificatePath(certRoot, path)
		if hostname == "" {
			continue
		}
		r.certs.Delete(hostname)
	}
}

func (r *LunaTLSCertResolver) loadExactCertificate(hostname string) (*tls.Certificate, error) {
	for _, pair := range r.lookupPaths(hostname) {
		if !fileExists(pair[0]) || !fileExists(pair[1]) {
			continue
		}
		cert, err := tls.LoadX509KeyPair(pair[0], pair[1])
		if err != nil {
			return nil, err
		}
		return &cert, nil
	}
	return nil, fmt.Errorf("certificate not found for hostname %q", hostname)
}

func hasCertificatePair(dir string) bool {
	crtInfo, err := os.Stat(filepath.Join(dir, "tls.crt"))
	if err != nil || crtInfo.IsDir() {
		return false
	}
	keyInfo, err := os.Stat(filepath.Join(dir, "tls.key"))
	if err != nil || keyInfo.IsDir() {
		return false
	}
	return true
}

func collectWatchDirectories(certRoot string) map[string]struct{} {
	paths := make(map[string]struct{})
	if strings.TrimSpace(certRoot) == "" {
		return paths
	}
	paths[certRoot] = struct{}{}

	entries, err := os.ReadDir(certRoot)
	if err != nil {
		return paths
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(certRoot, entry.Name())
		paths[dir] = struct{}{}
		if entry.Name() != ".wildcard" {
			continue
		}
		wildcards, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, wildcard := range wildcards {
			if wildcard.IsDir() {
				paths[filepath.Join(dir, wildcard.Name())] = struct{}{}
			}
		}
	}
	return paths
}

type certLRU struct {
	capacity int
	mu       sync.Mutex
	ll       *list.List
	entries  map[string]*list.Element
}

type certEntry struct {
	key   string
	value *tls.Certificate
}

func newCertLRU(capacity int) *certLRU {
	return &certLRU{
		capacity: capacity,
		ll:       list.New(),
		entries:  make(map[string]*list.Element, capacity),
	}
}

func (c *certLRU) Get(key string) (*tls.Certificate, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	elem, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	c.ll.MoveToFront(elem)
	return elem.Value.(*certEntry).value, true
}

func (c *certLRU) Add(key string, value *tls.Certificate) {
	if value == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.entries[key]; ok {
		c.ll.MoveToFront(elem)
		elem.Value.(*certEntry).value = value
		return
	}
	elem := c.ll.PushFront(&certEntry{key: key, value: value})
	c.entries[key] = elem
	if c.ll.Len() > c.capacity {
		oldest := c.ll.Back()
		if oldest != nil {
			c.ll.Remove(oldest)
			delete(c.entries, oldest.Value.(*certEntry).key)
		}
	}
}

func (c *certLRU) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	elem, ok := c.entries[key]
	if !ok {
		return
	}
	c.ll.Remove(elem)
	delete(c.entries, key)
}

func (c *certLRU) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ll = list.New()
	c.entries = make(map[string]*list.Element, c.capacity)
}
