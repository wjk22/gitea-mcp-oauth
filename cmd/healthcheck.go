package cmd

import (
	"fmt"
	"io"
	"net/http"
)

// runHealthcheck dials the /healthz endpoint on 127.0.0.1:port and reports
// success or failure. It returns true when the server responds with a 2xx
// status.
func runHealthcheck(client *http.Client, port int, stdout, stderr io.Writer) bool {
	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)

	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(stderr, "healthcheck failed: %v\n", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		fmt.Fprintf(stderr, "healthcheck failed: unexpected status %s\n", resp.Status)
		return false
	}

	fmt.Fprintln(stdout, "healthy")
	return true
}
