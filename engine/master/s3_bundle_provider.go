package master

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	enginepkg "github.com/jabberwocky238/luna-edge/engine"
	"github.com/jabberwocky238/luna-edge/repository"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type S3Config struct {
	Endpoint           string
	Region             string
	AccessKeyID        string
	SecretAccessKey    string
	SessionToken       string
	UsePathStyle       bool
	InsecureSkipVerify bool
	StartupProbeBucket string
	HTTPTimeout        time.Duration
}

type S3CertificateBundleProvider struct {
	repo   repository.Repository
	cfg    S3Config
	client *minio.Client
}

const defaultCertificateBucket = "lunaedge"

func NewS3CertificateBundleProvider(repo repository.Repository, cfg S3Config) (*S3CertificateBundleProvider, error) {
	if repo == nil {
		return nil, fmt.Errorf("repository is required")
	}
	cfg = normalizeS3Config(cfg)
	client, err := newMinioClient(cfg)
	if err != nil {
		return nil, err
	}
	return &S3CertificateBundleProvider{
		repo:   repo,
		cfg:    cfg,
		client: client,
	}, nil
}

func newMinioClient(cfg S3Config) (*minio.Client, error) {
	cfg = normalizeS3Config(cfg)
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, fmt.Errorf("s3 endpoint is required")
	}
	if strings.TrimSpace(cfg.Region) == "" {
		return nil, fmt.Errorf("s3 region is required")
	}
	if strings.TrimSpace(cfg.AccessKeyID) == "" {
		return nil, fmt.Errorf("s3 access key id is required")
	}
	if strings.TrimSpace(cfg.SecretAccessKey) == "" {
		return nil, fmt.Errorf("s3 secret access key is required")
	}
	endpoint, secure, err := parseMinioEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	return minio.New(endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken),
		Secure:       secure,
		Region:       cfg.Region,
		BucketLookup: bucketLookupType(cfg.UsePathStyle),
		Transport:    buildMinioTransport(secure, cfg.InsecureSkipVerify),
	})
}

func normalizeS3Config(cfg S3Config) S3Config {
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = 10 * time.Second
	}
	return cfg
}

func parseMinioEndpoint(raw string) (string, bool, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", false, fmt.Errorf("parse s3 endpoint: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", false, fmt.Errorf("s3 endpoint must be absolute")
	}
	if u.Path != "" && u.Path != "/" {
		return "", false, fmt.Errorf("s3 endpoint path is not supported: %q", u.Path)
	}
	switch u.Scheme {
	case "http":
		return u.Host, false, nil
	case "https":
		return u.Host, true, nil
	default:
		return "", false, fmt.Errorf("unsupported s3 endpoint scheme %q", u.Scheme)
	}
}

func bucketLookupType(usePathStyle bool) minio.BucketLookupType {
	if usePathStyle {
		return minio.BucketLookupPath
	}
	return minio.BucketLookupAuto
}

func buildMinioTransport(secure, insecureSkipVerify bool) http.RoundTripper {
	transport, err := minio.DefaultTransport(secure)
	if err != nil {
		return nil
	}
	cloned := transport.Clone()
	if cloned.TLSClientConfig != nil {
		cloned.TLSClientConfig.InsecureSkipVerify = insecureSkipVerify
	}
	return cloned
}

func (p *S3CertificateBundleProvider) FetchCertificateBundle(ctx context.Context, hostname string, revision uint64) (*enginepkg.CertificateBundle, error) {
	location, err := p.certificateLocation(hostname, revision)
	if err != nil {
		return nil, err
	}
	bundle := &enginepkg.CertificateBundle{
		Hostname: hostname,
		Revision: revision,
	}
	if bundle.TLSCrt, err = p.readObject(ctx, location.bucket, objectKey(location.revisionPrefix, "tls.crt")); err != nil {
		return nil, err
	}
	if bundle.TLSKey, err = p.readObject(ctx, location.bucket, objectKey(location.revisionPrefix, "tls.key")); err != nil {
		return nil, err
	}
	if bundle.MetadataJSON, err = p.readObject(ctx, location.bucket, objectKey(location.revisionPrefix, "metadata.json")); err != nil {
		bundle.MetadataJSON, err = p.readObject(ctx, location.bucket, objectKey(location.latestPrefix, "metadata.json"))
		if err != nil {
			return nil, err
		}
	}
	return bundle, nil
}

func (p *S3CertificateBundleProvider) PutCertificateBundle(ctx context.Context, hostname string, revision uint64, bundle *enginepkg.CertificateBundle) error {
	if p == nil {
		return fmt.Errorf("s3 provider is nil")
	}
	if bundle == nil {
		return fmt.Errorf("certificate bundle is nil")
	}
	location, err := p.certificateLocation(hostname, revision)
	if err != nil {
		return err
	}
	for _, key := range []string{
		objectKey(location.latestPrefix, "tls.crt"),
		objectKey(location.revisionPrefix, "tls.crt"),
	} {
		if err := p.writeObject(ctx, location.bucket, key, bundle.TLSCrt, "application/x-pem-file"); err != nil {
			return err
		}
	}
	for _, key := range []string{
		objectKey(location.latestPrefix, "tls.key"),
		objectKey(location.revisionPrefix, "tls.key"),
	} {
		if err := p.writeObject(ctx, location.bucket, key, bundle.TLSKey, "application/x-pem-file"); err != nil {
			return err
		}
	}
	metadataJSON := bundle.MetadataJSON
	if len(metadataJSON) == 0 {
		metadataJSON, err = json.Marshal(map[string]any{
			"hostname": hostname,
			"revision": revision,
		})
		if err != nil {
			return err
		}
	}
	for _, key := range []string{
		objectKey(location.latestPrefix, "metadata.json"),
		objectKey(location.revisionPrefix, "metadata.json"),
	} {
		if err := p.writeObject(ctx, location.bucket, key, metadataJSON, "application/json"); err != nil {
			return err
		}
	}
	return nil
}

type certificateLocation struct {
	bucket         string
	latestPrefix   string
	revisionPrefix string
}

func (p *S3CertificateBundleProvider) certificateLocation(hostname string, revision uint64) (*certificateLocation, error) {
	if p == nil {
		return nil, fmt.Errorf("s3 provider is nil")
	}
	normalized := strings.Trim(strings.ToLower(strings.TrimSpace(hostname)), "/")
	normalized = strings.TrimSuffix(normalized, ".")
	if normalized == "" {
		return nil, fmt.Errorf("hostname is required")
	}
	latestPrefix := normalized
	revisionPrefix := path.Join(normalized, strconv.FormatUint(revision, 10))
	return &certificateLocation{
		bucket:         defaultCertificateBucket,
		latestPrefix:   latestPrefix,
		revisionPrefix: revisionPrefix,
	}, nil
}

func objectKey(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return path.Join(prefix, name)
}

func (p *S3CertificateBundleProvider) readObject(ctx context.Context, bucket, key string) ([]byte, error) {
	ctx, cancel := p.withTimeout(ctx)
	defer cancel()
	reader, err := p.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get s3 object s3://%s/%s: %w", bucket, key, err)
	}
	defer func() { _ = reader.Close() }()
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read s3 object s3://%s/%s: %w", bucket, key, err)
	}
	return body, nil
}

func (p *S3CertificateBundleProvider) writeObject(ctx context.Context, bucket, key string, body []byte, contentType string) error {
	ctx, cancel := p.withTimeout(ctx)
	defer cancel()
	_, err := p.client.PutObject(ctx, bucket, key, bytes.NewReader(body), int64(len(body)), minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("put s3 object s3://%s/%s: %w", bucket, key, err)
	}
	return nil
}

func (p *S3CertificateBundleProvider) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if p == nil || p.cfg.HTTPTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, p.cfg.HTTPTimeout)
}
