package burn

import (
	"errors"
	"fmt"
	"testing"

	"bitbucket.org/senprints/agent-office/internal/llm"
)

func TestTechnical(t *testing.T) {
	yes := []error{
		errors.New("⚠️ Quy trình Hai góc nhìn không xong: bước Góc nhìn A: codex: ERROR codex_models_manager: failed to refresh available models: unexpected status 401 Unauthorized: Encountered invalidated oauth token"),
		fmt.Errorf("%w: phiên Codex đã hết hạn", llm.ErrNeedsLogin),
		errors.New("API Error: 529 overloaded_error"),
		errors.New("dial tcp: lookup api.anthropic.com: no such host"),
		errors.New("API Error: 403 Forbidden"),
		// the CLI refused office's own input: no piece is to blame (Burn 14/10)
		errors.New("⚠️ Quy trình Hai góc nhìn và tổng hợp không xong: bước Góc nhìn A: codex: Failed to read prompt from stdin: input is not valid UTF-8 (invalid byte at offset 9530)"),
	}
	for _, err := range yes {
		if !technical(err) {
			t.Errorf("not technical: %v", err)
		}
	}
	no := []error{
		nil,
		errors.New("go test ./... failed: 3 tests"),
		errors.New("agent không báo kết quả (burn_done/burn_fail)"),
		errors.New("không có workflow review"),
	}
	for _, err := range no {
		if technical(err) {
			t.Errorf("technical: %v", err)
		}
	}
}
