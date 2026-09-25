// Package ateclient dials the Substrate control plane (ateapi) with mutual
// TLS the same way the in-tree benchmark clients do: server verified against
// the servicedns CA, client identity presented from a projected
// pod-certificate credential bundle. The bundle is re-read whenever the file
// changes so kubelet rotations are picked up without a restart.
//
// This mirrors substrate/internal/ateapiauth, which external modules cannot
// import (internal package).
package ateclient

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"sync"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Defaults matching manifests in this repo and the base ate-install.
const (
	DefaultEndpoint   = "dns:///api.ate-system.svc.cluster.local:443"
	DefaultServerName = "api.ate-system.svc"
	DefaultCAFile     = "/run/servicedns-ca/ca.crt"
	DefaultCredBundle = "/run/podidentity.podcert.ate.dev/credential-bundle.pem"
)

type bundleCache struct {
	path string

	mu   sync.Mutex
	fi   os.FileInfo
	cert *tls.Certificate
}

// get parses the credential bundle (cert chain + PKCS8 key in one PEM file),
// re-reading it only when the file identity/mtime/size changed. Rotations of
// projected volumes swap a symlink, which surfaces as an identity change.
func (c *bundleCache) get() (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fi, err := os.Stat(c.path)
	if err != nil {
		return nil, fmt.Errorf("stat credential bundle: %w", err)
	}
	if c.cert != nil && c.fi != nil && os.SameFile(c.fi, fi) &&
		c.fi.ModTime().Equal(fi.ModTime()) && c.fi.Size() == fi.Size() {
		return c.cert, nil
	}
	pem, err := os.ReadFile(c.path)
	if err != nil {
		return nil, fmt.Errorf("read credential bundle: %w", err)
	}
	// The bundle holds both CERTIFICATE blocks and the PRIVATE KEY block;
	// X509KeyPair picks each kind out of whichever argument carries it.
	cert, err := tls.X509KeyPair(pem, pem)
	if err != nil {
		return nil, fmt.Errorf("parse credential bundle: %w", err)
	}
	c.fi, c.cert = fi, &cert
	return &cert, nil
}

// Dial opens an authenticated gRPC connection to ateapi and returns the
// Control client. ateapi rejects unauthenticated calls, so the credential
// bundle is required.
func Dial(endpoint, caFile, credBundle, serverName string) (*grpc.ClientConn, ateapipb.ControlClient, error) {
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, nil, fmt.Errorf("read CA file: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, nil, fmt.Errorf("no certificates in CA file %q", caFile)
	}
	cache := &bundleCache{path: credBundle}
	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    pool,
		ServerName: serverName,
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return cache.get()
		},
	}
	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	if err != nil {
		return nil, nil, fmt.Errorf("dial %s: %w", endpoint, err)
	}
	return conn, ateapipb.NewControlClient(conn), nil
}
