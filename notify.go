package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// @region app:notify -- BELL + OS NOTIFICATION

// indirection so tests stay silent
var notifyFn = notifyDone

// rings the terminal bell and fires an OS notification. both are best-effort
// and fired in the background so a slow or missing binary can never stall the
// tick; all errors are ignored on purpose.
func notifyDone(label string) {
	fmt.Fprint(os.Stderr, "\a")

	body := fmt.Sprintf("%s complete", label)
	go func() {
		switch runtime.GOOS {
		case "darwin":
			script := fmt.Sprintf(
				"display notification %s with title %s",
				osaQuote(body), osaQuote("cycler"),
			)
			exec.Command("osascript", "-e", script).Run()
		case "linux":
			exec.Command("notify-send", "cycler", body).Run()
		}
	}()
}

// wraps a string as an AppleScript literal, escaping backslashes and quotes so
// a label with punctuation can't break out of the script
func osaQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
