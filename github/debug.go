package github

import (
	"fmt"
	"os"
)

func vprintln(a ...any) (int, error) {
	if VERBOSITY > 0 {
		return fmt.Fprintln(os.Stderr, a...)
	}

	return 0, nil
}

func vprintf(format string, a ...any) (int, error) {
	if VERBOSITY > 0 {
		return fmt.Fprintf(os.Stderr, format, a...)
	}

	return 0, nil
}
