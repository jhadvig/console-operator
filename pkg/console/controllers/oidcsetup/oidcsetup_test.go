package oidcsetup

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateOIDCIssuer(t *testing.T) {
	tests := []struct {
		name        string
		issuerURL   string
		caBundle    []byte
		handler     http.HandlerFunc
		wantErr     bool
		errContains string
	}{
		{
			name:        "empty issuer URL",
			issuerURL:   "",
			wantErr:     true,
			errContains: "issuer URL is empty",
		},
		{
			name:        "non-HTTPS scheme",
			issuerURL:   "http://example.com",
			wantErr:     true,
			errContains: "must use the HTTPS scheme",
		},
		{
			name:        "no host in URL",
			issuerURL:   "https://",
			wantErr:     true,
			errContains: "must include a host",
		},
		{
			name:        "malformed URL",
			issuerURL:   "://not-a-url",
			wantErr:     true,
			errContains: "invalid issuer URL",
		},
		{
			name:      "valid issuer with successful discovery",
			issuerURL: "", // set dynamically from TLS test server
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"issuer":"https://example.com"}`))
			},
			wantErr: false,
		},
		{
			name:      "discovery returns 404",
			issuerURL: "", // set dynamically
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr:     true,
			errContains: "returned HTTP 404",
		},
		{
			name:      "discovery returns 500",
			issuerURL: "", // set dynamically
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr:     true,
			errContains: "returned HTTP 500",
		},
		{
			name:        "unreachable host",
			issuerURL:   "https://192.0.2.1:1", // RFC 5737 TEST-NET, guaranteed unreachable
			wantErr:     true,
			errContains: "OIDC discovery request to",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issuerURL := tt.issuerURL
			var caBundle []byte

			if tt.handler != nil {
				server := httptest.NewTLSServer(tt.handler)
				defer server.Close()

				issuerURL = server.URL
				caBundle = extractTLSServerCA(t, server)
			}

			err := validateOIDCIssuer(context.Background(), issuerURL, caBundle)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errContains)
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got: %v", tt.errContains, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidateOIDCIssuerTrailingSlash(t *testing.T) {
	var requestedPath string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"issuer":"https://example.com"}`))
	}))
	defer server.Close()

	caBundle := extractTLSServerCA(t, server)

	err := validateOIDCIssuer(context.Background(), server.URL+"/", caBundle)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if requestedPath != "/.well-known/openid-configuration" {
		t.Errorf("expected discovery path %q, got %q", "/.well-known/openid-configuration", requestedPath)
	}
}

func TestValidateOIDCIssuerWithCustomCA(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"issuer":"https://example.com"}`))
	}))
	defer server.Close()

	t.Run("succeeds with correct CA", func(t *testing.T) {
		caBundle := extractTLSServerCA(t, server)
		err := validateOIDCIssuer(context.Background(), server.URL, caBundle)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("fails without CA for self-signed cert", func(t *testing.T) {
		err := validateOIDCIssuer(context.Background(), server.URL, nil)
		if err == nil {
			t.Fatal("expected TLS error, got nil")
		}
		if !strings.Contains(err.Error(), "OIDC discovery request to") {
			t.Errorf("expected TLS-related discovery failure, got: %v", err)
		}
	})
}

func extractTLSServerCA(t *testing.T, server *httptest.Server) []byte {
	t.Helper()
	serverCert := server.TLS.Certificates[0]
	leaf, err := x509.ParseCertificate(serverCert.Certificate[0])
	if err != nil {
		t.Fatalf("failed to parse server certificate: %v", err)
	}

	// For httptest TLS servers, the leaf cert is self-signed and acts as its own CA.
	// If there's a proper CA in the chain, prefer it.
	caCert := leaf
	if len(serverCert.Certificate) > 1 {
		caCert, err = x509.ParseCertificate(serverCert.Certificate[len(serverCert.Certificate)-1])
		if err != nil {
			t.Fatalf("failed to parse CA certificate: %v", err)
		}
	}

	return pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caCert.Raw,
	})
}

// Verify that the test helper's CA actually works with Go's TLS stack.
func TestExtractTLSServerCA(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	caBundle := extractTLSServerCA(t, server)
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caBundle) {
		t.Fatal("failed to add CA cert to pool")
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: pool,
			},
		},
	}

	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("request with extracted CA failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
