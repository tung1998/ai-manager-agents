package mcpgateway

import (
	"bytes"
	"errors"
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

func TestReadReplySSEMidStreamError(t *testing.T) {
	r := &errReader{
		data: []byte("data: {\"not\":\"complete\"}\n"),
		err:  io.ErrUnexpectedEOF,
	}
	_, err := readReply(sseResponse(r), 1)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected wrapped io.ErrUnexpectedEOF, got: %v", err)
	}
}

func TestReadReplySSEValidMessageThenStreamError(t *testing.T) {
	valid := `data: {"jsonrpc":"2.0","id":1,"result":{"ok":true}}` + "\n\n"
	r := &errReader{
		data: []byte(valid),
		err:  io.ErrUnexpectedEOF,
	}
	m, err := readReply(sseResponse(r), 1)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if m.Result == nil {
		t.Fatal("expected result to be set")
	}
}

func TestReadReplyJSONReadError(t *testing.T) {
	r := &errReader{err: errors.New("boom")}
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   io.NopCloser(r),
	}
	_, err := readReply(resp, 1)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected wrapped error containing 'boom', got: %v", err)
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
