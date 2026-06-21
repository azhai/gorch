package common

import (
	"os"
	"os/signal"
	"syscall"
)

func SetupSignalHandler(handler func(sig os.Signal)) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)

	go func() {
		for sig := range sigCh {
			handler(sig)
		}
	}()
}
