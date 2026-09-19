package oauth

import "net/http"

// CheckRedirectUpstreamForTest exposes checkRedirectUpstream for testing.
func CheckRedirectUpstreamForTest(giteaHost string) func(req *http.Request, via []*http.Request) error {
	return checkRedirectUpstream(giteaHost)
}
