package selfupdate

import (
	"testing"
	"time"
)

// A line longer than the scanner's 1MB buffer must not leave exec() hanging:
// the reader goroutine has to drain the pipe to EOF even after bufio.ErrTooLong,
// otherwise the child blocks writing to a full pipe and cmd.Wait() never returns.
func TestExecSurvivesOverlongLine(t *testing.T) {
	u := New(Source{}, t.TempDir(), nil, nil)
	script := `
head -c 2000000 /dev/zero | tr '\0' 'x'
echo
for i in $(seq 1 5); do echo "line $i"; done
exit 0
`
	done := make(chan error, 1)
	go func() { done <- u.exec("", nil, []string{"sh", "-c", script}) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("exec returned error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("exec() hung on an overlong line instead of draining the pipe")
	}
}
