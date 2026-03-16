package acme

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/go-acme/lego/v4/certificate"
	enginepkg "github.com/jabberwocky238/luna-edge/engine"
)

func buildBundle(resource *certificate.Resource, revision uint64) (*enginepkg.CertificateBundle, time.Time, time.Time, string, string, error) {
	if resource == nil {
		return nil, time.Time{}, time.Time{}, "", "", fmt.Errorf("certificate resource is nil")
	}
	block, _ := pem.Decode(resource.Certificate)
	if block == nil {
		return nil, time.Time{}, time.Time{}, "", "", fmt.Errorf("decode certificate pem")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, time.Time{}, time.Time{}, "", "", err
	}
	crtHash := sha256.Sum256(resource.Certificate)
	keyHash := sha256.Sum256(resource.PrivateKey)
	metadataJSON, _ := json.Marshal(map[string]any{
		"hostname":   resource.Domain,
		"revision":   revision,
		"not_before": cert.NotBefore,
		"not_after":  cert.NotAfter,
	})
	return &enginepkg.CertificateBundle{
		Hostname:     resource.Domain,
		Revision:     revision,
		TLSCrt:       resource.Certificate,
		TLSKey:       resource.PrivateKey,
		MetadataJSON: metadataJSON,
	}, cert.NotBefore, cert.NotAfter, hex.EncodeToString(crtHash[:]), hex.EncodeToString(keyHash[:]), nil
}
