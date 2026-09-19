package gitea

import (
	"net/http"
	"net/url"
)

// VerifyHostForTest exposes verifyHost for package external tests.
func VerifyHostForTest(host string, targetURL *url.URL) error {
	return verifyHost(host, targetURL)
}

// MakeCheckRedirectOAuthForTest exposes makeCheckRedirectOAuth for package external tests.
func MakeCheckRedirectOAuthForTest(host string) func(req *http.Request, via []*http.Request) error {
	return makeCheckRedirectOAuth(host)
}
