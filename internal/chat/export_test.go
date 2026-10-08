package chat

import (
	"time"

	"bitbucket.org/senprints/agent-office/internal/workflow"
)

// SetWatchEvery makes runs' supervisors check this often; the returned
// func puts it back.
func SetWatchEvery(d time.Duration) func() {
	old := watchEvery
	watchEvery = func(def workflow.Def) time.Duration {
		if def.SuperviseEvery() > 0 {
			return d
		}
		return 0
	}
	return func() { watchEvery = old }
}
