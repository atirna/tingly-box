package quota

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/sirupsen/logrus"
)

// sensitiveHeaders are logged as "<redacted>" rather than dropped, so the log
// still shows whether a credential header was sent at all.
var sensitiveHeaders = map[string]bool{
	"authorization": true, "cookie": true, "set-cookie": true,
	"proxy-authorization": true, "x-api-key": true, "x-goog-api-key": true,
	"api-key": true,
}

// loggingTransport traces every quota HTTP call, for every fetcher. Quota reads
// run unattended on a ticker, so without this a failing provider leaves nothing
// behind but a status code on its usage record.
//
// A failed call logs at warn with everything needed to explain it: what was
// sent (headers, credential values redacted — several vendors gate their usage
// endpoints on client identity, not just the token) and what came back
// (headers, and the body in full, since a clipped body is exactly the case
// where the log fails to explain the failure).
type loggingTransport struct {
	base http.RoundTripper
}

func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}

	log := logrus.WithFields(logrus.Fields{"method": req.Method, "url": req.URL.String()})

	resp, err := base.RoundTrip(req)
	if err != nil {
		log.WithError(err).Warn("quota request failed")
		return nil, err
	}

	// Anything below 400 is not a failure to report: the client follows
	// redirects itself, so a 3xx here is a hop, not an outcome.
	log = log.WithField("status", resp.StatusCode)
	if resp.StatusCode < http.StatusBadRequest {
		log.Debug("quota response")
		return resp, nil
	}

	// A read error still leaves whatever arrived before it in raw, which is
	// more useful in the log than dropping the body entirely.
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(raw))

	log.WithFields(logrus.Fields{
		"request_headers":  redactHeaders(req.Header),
		"response_headers": redactHeaders(resp.Header),
		"body":             string(raw),
	}).Warn("quota upstream returned an error status")
	return resp, nil
}

// redactHeaders renders headers as a "name=value" list with credential values
// masked.
func redactHeaders(header http.Header) string {
	parts := make([]string, 0, len(header))
	for name, values := range header {
		if sensitiveHeaders[strings.ToLower(name)] {
			parts = append(parts, name+"=<redacted>")
			continue
		}
		parts = append(parts, name+"="+strings.Join(values, ","))
	}
	return strings.Join(parts, "; ")
}
