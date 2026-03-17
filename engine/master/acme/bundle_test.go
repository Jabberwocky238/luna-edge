package acme

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/go-acme/lego/v4/certificate"
)

func TestBuildBundleIncludesIssuerCertificateInTLSCrt(t *testing.T) {
	t.Parallel()

	leafPEM, issuerPEM := newLeafAndIssuerPEM(t, "app.example.com")
	resource := &certificate.Resource{
		Domain:            "app.example.com",
		Certificate:       leafPEM,
		IssuerCertificate: issuerPEM,
		PrivateKey:        []byte("key"),
	}

	bundle, _, _, _, _, err := buildBundle(resource, 1)
	if err != nil {
		t.Fatalf("build bundle: %v", err)
	}
	if bytes.Count(bundle.TLSCrt, []byte("BEGIN CERTIFICATE")) != 2 {
		t.Fatalf("expected fullchain with 2 certs, got:\n%s", string(bundle.TLSCrt))
	}
	if !bytes.Contains(bundle.TLSCrt, issuerPEM) {
		t.Fatal("expected issuer certificate to be appended to tls.crt")
	}
}

func TestCertificateFullChainAvoidsDuplicateIssuerAppend(t *testing.T) {
	t.Parallel()

	leafPEM, issuerPEM := newLeafAndIssuerPEM(t, "app.example.com")
	alreadyBundled := append(append([]byte{}, bytes.TrimSpace(leafPEM)...), '\n')
	alreadyBundled = append(alreadyBundled, bytes.TrimSpace(issuerPEM)...)
	alreadyBundled = append(alreadyBundled, '\n')

	fullChain := certificateFullChain(alreadyBundled, issuerPEM)
	if bytes.Count(fullChain, []byte("BEGIN CERTIFICATE")) != 2 {
		t.Fatalf("expected issuer not to be duplicated, got:\n%s", string(fullChain))
	}
}

func newLeafAndIssuerPEM(t *testing.T, hostname string) ([]byte, []byte) {
	t.Helper()

	issuerKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate issuer key: %v", err)
	}
	issuerTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test Issuer"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	issuerDER, err := x509.CreateCertificate(rand.Reader, issuerTemplate, issuerTemplate, &issuerKey.PublicKey, issuerKey)
	if err != nil {
		t.Fatalf("create issuer certificate: %v", err)
	}
	issuerCert, err := x509.ParseCertificate(issuerDER)
	if err != nil {
		t.Fatalf("parse issuer certificate: %v", err)
	}

	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: hostname},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{hostname},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, issuerCert, &leafKey.PublicKey, issuerKey)
	if err != nil {
		t.Fatalf("create leaf certificate: %v", err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: issuerDER})
}
