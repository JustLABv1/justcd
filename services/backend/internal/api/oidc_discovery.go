package api

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Discovery responses may contain arbitrary content. Report classifications,
// never the raw error or response body, to the unauthenticated login endpoint.
type discoveryTransport struct {
	next   http.RoundTripper
	status int
}

func (t *discoveryTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := t.next.RoundTrip(r)
	if response != nil {
		t.status = response.StatusCode
	}
	return response, err
}

func discoverOIDC(ctx context.Context, issuer string) (*oidc.Provider, string, int, error) {
	transport := &discoveryTransport{next: http.DefaultTransport}
	client := *http.DefaultClient
	client.Transport = transport
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, &client), issuer)
	if err == nil {
		return provider, "", transport.status, nil
	}
	reason := "invalid_document"
	var mismatch *oidc.IssuerMismatchError
	var dns *net.DNSError
	var certificate *tls.CertificateVerificationError
	var unknownCA x509.UnknownAuthorityError
	var network net.Error
	switch {
	case errors.As(err, &mismatch):
		reason = "issuer_mismatch"
	case errors.As(err, &certificate), errors.As(err, &unknownCA):
		reason = "tls_certificate"
	case errors.Is(err, context.DeadlineExceeded):
		reason = "timeout"
	case errors.As(err, &dns):
		reason = "dns"
	case errors.As(err, &network):
		reason = "connection"
		if network.Timeout() {
			reason = "timeout"
		}
	case transport.status != 0 && transport.status != http.StatusOK:
		reason = "http_status"
	}
	return nil, reason, transport.status, err
}

func (s *Server) writeOIDCDiscoveryError(w http.ResponseWriter, r *http.Request, providerID, reason string, status int) {
	remediation := "Check that the issuer serves a valid OpenID discovery document."
	retryable := false
	switch reason {
	case "issuer_mismatch":
		remediation = "Set the configured issuer to exactly match the issuer in the discovery document, including its path and trailing slash."
	case "tls_certificate":
		remediation = "Check the provider certificate hostname, expiry, and CA trust in the backend container."
	case "dns":
		remediation = "Check that the backend can resolve the provider hostname."
		retryable = true
	case "timeout", "connection":
		remediation = "Check backend connectivity to the provider and provider availability."
		retryable = true
	case "http_status":
		remediation = "Check the issuer path and discovery endpoint HTTP status."
		retryable = status == 429 || status >= 500
	}
	s.Logger.ErrorContext(r.Context(), "OIDC discovery failed", "providerId", providerID, "discovery_reason", reason, "http_status", status)
	writeAPIError(w, http.StatusBadGateway, "OIDC provider discovery failed", apiErrorMetadata{
		code: "oidc.discovery_failed", category: "dependency", retryable: retryable, remediation: remediation,
	}, map[string]any{"reason": reason, "httpStatus": status})
}
