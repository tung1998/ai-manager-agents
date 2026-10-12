package mcpgateway

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
)

// errReader returns some bytes then a read error.
type errReader struct {
	data []byte
	err  error
}

func (r *errReader) Read(p []byte) (int, error) {
	if len(r.data) > 0 {
		n := copy(p, r.data)
		r.data = r.data[n:]
		return n, nil
	}
	return 0, r.err
}

func sseResponse(body io.Reader) *http.Response {
	return &http.Response{
		Header: http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:   io.NopCloser(body),
	}
}

func TestReadReplySSETooLong(t *testing.T) {
	huge := strings.Repeat("x", 9<<20)
	body := "data: " + huge + "\n\n"
	_, err := readReply(sseResponse(strings.NewReader(body)), 1)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "vượt 8MB") {
		t.Fatalf("expected 'vượt 8MB' error, got: %v", err)
	}
}

func TestReadReplyJSONOK(t *testing.T) {
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   io.NopCloser(bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"result":{}}`)),
	}
	m, err := readReply(resp, 1)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if m.Result == nil {
		t.Fatal("expected result to be set")
	}
}
