package acme

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"

	"github.com/go-acme/lego/v4/certificate"
	"github.com/jabberwocky238/luna-edge/replication"
	"github.com/jabberwocky238/luna-edge/repository/metadata"
)

func buildBundleAndRevision(resource *certificate.Resource, revision uint64) (*replication.CertificateBundle, *metadata.CertificateRevision, error) {
	if resource == nil {
		return nil, nil, fmt.Errorf("certificate resource is nil")
	}
	fullChain := certificateFullChain(resource.Certificate, resource.IssuerCertificate)
	block, _ := pem.Decode(fullChain)
	if block == nil {
		return nil, nil, fmt.Errorf("decode certificate pem")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, err
	}
	crtHash := sha256.Sum256(fullChain)
	keyHash := sha256.Sum256(resource.PrivateKey)
	metadataJSON, _ := json.Marshal(map[string]any{
		"hostname":   resource.Domain,
		"revision":   revision,
		"not_before": cert.NotBefore,
		"not_after":  cert.NotAfter,
	})
	return &replication.CertificateBundle{
			Hostname:     resource.Domain,
			Revision:     revision,
			TLSCrt:       fullChain,
			TLSKey:       resource.PrivateKey,
			MetadataJSON: metadataJSON,
		}, &metadata.CertificateRevision{
			NotBefore: cert.NotBefore,
			NotAfter:  cert.NotAfter,
			SHA256Crt: hex.EncodeToString(crtHash[:]),
			SHA256Key: hex.EncodeToString(keyHash[:]),
		}, nil
}

func certificateFullChain(certificatePEM, issuerPEM []byte) []byte {
	certificatePEM = bytes.TrimSpace(certificatePEM)
	issuerPEM = bytes.TrimSpace(issuerPEM)
	if len(certificatePEM) == 0 {
		return nil
	}
	if len(issuerPEM) == 0 || bytes.Contains(certificatePEM, issuerPEM) {
		return append(append([]byte(nil), certificatePEM...), '\n')
	}
	fullChain := make([]byte, 0, len(certificatePEM)+1+len(issuerPEM)+1)
	fullChain = append(fullChain, certificatePEM...)
	fullChain = append(fullChain, '\n')
	fullChain = append(fullChain, issuerPEM...)
	fullChain = append(fullChain, '\n')
	return fullChain
}
