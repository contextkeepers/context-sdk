//go:build unix

package ctxsdk

import (
	"os"
	"os/signal"
	"syscall"
)

// CloseOnSignal drains the client on SIGTERM or SIGINT, then restores the
// default action and re-raises, so the process still exits exactly as it
// would have.
//
// Opt-in on purpose. Registering a signal suppresses Go's default
// termination, so a library doing this unasked would stop the caller's
// process from exiting at all unless they also handled it.
//
// Only right for a process whose only shutdown work is this client - a
// worker, a CLI, a cron job. A service that also drains connections must
// call Close itself, in order, or whichever handler finishes first wins
// and the shutdown events are the ones lost.
func CloseOnSignal(client *Client) {
	received := make(chan os.Signal, 1)
	signal.Notify(received, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		sig := <-received
		_ = client.Close()

		// Hand the signal back to the default handler so the exit status
		// is the one the caller's supervisor expects.
		signal.Reset(sig)
		if unix, ok := sig.(syscall.Signal); ok {
			_ = syscall.Kill(syscall.Getpid(), unix)
		}
	}()
}
