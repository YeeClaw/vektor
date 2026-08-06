package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// logCapture collects JSON log records so tests can assert on them. Not safe for
// concurrent use; every test here drives one request at a time.
type logCapture struct {
	buf bytes.Buffer
}

func newLogCapture() (*slog.Logger, *logCapture) {
	c := &logCapture{}
	h := slog.NewJSONHandler(&c.buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(h), c
}

// only decodes the single record written, failing if there was not exactly one.
func (c *logCapture) only(t *testing.T) map[string]any {
	t.Helper()

	lines := strings.Split(strings.TrimSpace(c.buf.String()), "\n")
	if len(lines) != 1 || lines[0] == "" {
		t.Fatalf("got %d log records, want exactly 1:\n%s", len(lines), c.buf.String())
	}

	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("log line is not valid JSON: %v (line: %q)", err, lines[0])
	}
	return rec
}

// newLoggingServer builds the minimum Server that withRequestLogging needs. The
// real NewServer requires a database and an Authenticator; the request logger
// touches neither.
func newLoggingServer() (*Server, *logCapture) {
	logger, capture := newLogCapture()
	return &Server{log: logger}, capture
}

// serve runs one request through the request logger wrapping next.
func serve(s *Server, next http.Handler, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	s.withRequestLogging(next).ServeHTTP(rec, req)
	return rec
}

// TestRequestLoggingRecordsOutcome pins the fields an operator actually needs.
// The status is the interesting one: http.ResponseWriter has no way to read a
// status back, so the only reason this works is statusRecorder intercepting
// WriteHeader on the way through.
func TestRequestLoggingRecordsOutcome(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		status int
	}{
		{"ok", http.MethodGet, "/api/me", http.StatusOK},
		// Request logging wraps *outside* auth, so rejected requests still get a
		// line. Wrapped inside, every 401 would vanish from the request log.
		{"unauthorized", http.MethodGet, "/api/projects", http.StatusUnauthorized},
		{"not found", http.MethodGet, "/api/nope", http.StatusNotFound},
		{"created", http.MethodPost, "/api/projects", http.StatusCreated},
		{"server error", http.MethodPatch, "/api/issues/1", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, capture := newLoggingServer()
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
			})

			rec := serve(s, next, tt.method, tt.path)
			if rec.Code != tt.status {
				t.Errorf("status written to client = %d, want %d", rec.Code, tt.status)
			}

			got := capture.only(t)
			if got["msg"] != "request" {
				t.Errorf("msg = %v, want %q", got["msg"], "request")
			}
			if got["method"] != tt.method {
				t.Errorf("method = %v, want %q", got["method"], tt.method)
			}
			if got["path"] != tt.path {
				t.Errorf("path = %v, want %q", got["path"], tt.path)
			}
			// JSON numbers decode to float64.
			if status, ok := got["status"].(float64); !ok || int(status) != tt.status {
				t.Errorf("status = %#v, want %d", got["status"], tt.status)
			}
			// slog.Duration serializes as integer nanoseconds, so this is a number
			// rather than a string like "1.2ms".
			if d, ok := got["duration"].(float64); !ok || d < 0 {
				t.Errorf("duration = %#v, want a non-negative number of nanoseconds", got["duration"])
			}
		})
	}
}

// TestRequestLoggingStatusDefaultsToOK pins the initializer in withRequestLogging.
// net/http sends an implicit 200 when a handler writes a body without calling
// WriteHeader, and statusRecorder never sees that -- so without seeding the field
// to 200 this logs status=0 for every successful response that just writes JSON.
func TestRequestLoggingStatusDefaultsToOK(t *testing.T) {
	s, capture := newLoggingServer()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`)) // no WriteHeader call
	})

	rec := serve(s, next, http.MethodGet, "/api/me")
	if rec.Code != http.StatusOK {
		t.Fatalf("status written to client = %d, want %d", rec.Code, http.StatusOK)
	}

	got := capture.only(t)
	if status, ok := got["status"].(float64); !ok || int(status) != http.StatusOK {
		t.Errorf("status = %#v, want %d", got["status"], http.StatusOK)
	}
}

// TestRequestLoggingLogsAfterHandlerCompletes checks the ordering: the log line
// has to be written after next.ServeHTTP returns, or the status it reports is
// whatever the field held before the handler ran.
func TestRequestLoggingLogsAfterHandlerCompletes(t *testing.T) {
	s, capture := newLoggingServer()

	var loggedDuringHandler bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		loggedDuringHandler = capture.buf.Len() > 0
		w.WriteHeader(http.StatusTeapot)
	})

	serve(s, next, http.MethodGet, "/api/me")

	if loggedDuringHandler {
		t.Error("request line was written before the handler ran, want it written after")
	}
	if got := capture.only(t); int(got["status"].(float64)) != http.StatusTeapot {
		t.Errorf("status = %v, want %d", got["status"], http.StatusTeapot)
	}
}

// TestStatusRecorderSupportsResponseController is the reason statusRecorder has
// an Unwrap method. Embedding http.ResponseWriter replaces the dynamic type seen
// downstream, so without Unwrap a handler reaching for Flush (SSE, streaming)
// gets http.ErrNotSupported instead of the real writer.
func TestStatusRecorderSupportsResponseController(t *testing.T) {
	s, _ := newLoggingServer()
	s.log = discardLogger()

	var flushErr error
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		flushErr = http.NewResponseController(w).Flush()
	})

	serve(s, next, http.MethodGet, "/api/me")

	if flushErr != nil {
		t.Errorf("Flush through statusRecorder: %v, want nil (is Unwrap still defined?)", flushErr)
	}
}
