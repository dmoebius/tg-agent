// Package prompt asks the user for input on the terminal.
package prompt

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"golang.org/x/term"
)

var in = bufio.NewReader(os.Stdin)

func Interactive() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// ErrInterrupted is returned by the prompts when the user presses Ctrl-C.
var ErrInterrupted = errors.New("interrupted")

// interruptible runs the blocking read f and returns ErrInterrupted as soon as
// Ctrl-C is pressed, instead of waiting for Enter. The terminal state (echo,
// disabled by Secret) is restored. The abandoned read keeps blocking, so the
// caller must not prompt again but exit.
func interruptible(f func() (string, error)) (string, error) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)
	fd := int(os.Stdin.Fd())
	st, _ := term.GetState(fd)
	type result struct {
		s   string
		err error
	}
	done := make(chan result, 1)
	go func() {
		s, err := f()
		done <- result{s, err}
	}()
	select {
	case r := <-done:
		return r.s, r.err
	case <-sig:
		if st != nil {
			_ = term.Restore(fd, st)
		}
		fmt.Println()
		return "", ErrInterrupted
	}
}

func readLine() (string, error) {
	return interruptible(func() (string, error) {
		s, err := in.ReadString('\n')
		if err != nil && s == "" {
			return "", err
		}
		return strings.TrimSpace(s), nil
	})
}

// String asks for a value; empty input keeps def.
func String(label, def string) (string, error) {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	s, err := readLine()
	if err != nil {
		return "", err
	}
	if s == "" {
		return def, nil
	}
	return s, nil
}

// Secret asks for a value without echo.
func Secret(label string) (string, error) {
	fmt.Printf("%s: ", label)
	s, err := interruptible(func() (string, error) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		return string(b), err
	})
	return strings.TrimSpace(s), err
}

// Int64 asks until a valid integer is entered (empty keeps def if def != 0).
func Int64(label string, def int64) (int64, error) {
	for {
		d := ""
		if def != 0 {
			d = strconv.FormatInt(def, 10)
		}
		s, err := String(label, d)
		if err != nil {
			return 0, err
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n != 0 {
			return n, nil
		}
		fmt.Println("  please enter a non-zero integer")
	}
}

// Int64List parses a comma/space separated list of integers.
func Int64List(s string) ([]int64, error) {
	var out []int64
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		n, err := strconv.ParseInt(f, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid id %q", f)
		}
		out = append(out, n)
	}
	return out, nil
}
