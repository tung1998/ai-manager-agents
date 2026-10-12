package api

import "time"

// SetGitWait shortens how long a fetch or push request waits for git (tests
// of the background path); the returned func puts it back.
func SetGitWait(d time.Duration) func() {
	old := gitWait
	gitWait = d
	return func() { gitWait = old }
}

// SetCancelJobRaceHook runs fn right after cancelJob's initial Get, so a test
// can force the window where Claim flips a pending job to running between
// that Get and cancelJob's own write. The returned func puts it back.
func SetCancelJobRaceHook(fn func()) func() {
	old := cancelJobRaceHook
	cancelJobRaceHook = fn
	return func() { cancelJobRaceHook = old }
}
