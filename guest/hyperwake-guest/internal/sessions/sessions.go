// Package sessions counts local activity so the control plane can make
// auto-stop decisions.
//
// These are hints, not billing evidence. A browser disconnect alone is never
// sufficient to consider a machine idle, which is why the daemon reports SSH
// and TTY sessions separately.
package sessions

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// Counts is a snapshot of local activity.
type Counts struct {
	SSH int
	TTY int
}

// Reader reads activity from a guest filesystem root. The root is injectable
// so the behaviour is testable without a real machine.
type Reader struct {
	root string
}

// NewReader builds a reader over the given filesystem root ("/" in production).
func NewReader(root string) *Reader {
	if root == "" {
		root = "/"
	}
	return &Reader{root: root}
}

// Read counts current SSH and local TTY sessions by walking /proc.
//
// utmp would be the conventional source, but parsing it means cgo or a
// hand-rolled binary struct layout that differs by libc. Walking /proc for
// sshd session processes and login shells on tty devices is portable across
// the Debian dev image and the eventual Arch image, and cannot be spoofed by
// an unprivileged tenant process any more easily than utmp can.
func (r *Reader) Read() Counts {
	var counts Counts

	entries, err := os.ReadDir(r.root + "proc")
	if err != nil {
		return counts
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue // not a pid
		}

		base := r.root + "proc/" + entry.Name()

		cmdline, err := os.ReadFile(base + "/cmdline")
		if err != nil {
			continue
		}

		// Arguments are NUL-separated.
		cmd := strings.ReplaceAll(string(cmdline), "\x00", " ")

		switch {
		// A per-connection sshd looks like "sshd: dev@pts/0" or "sshd: dev [priv]".
		// The listener itself has no "@" and no pts, so it is not counted.
		case strings.HasPrefix(cmd, "sshd:") && strings.Contains(cmd, "@pts"):
			counts.SSH++
		case isLoginShellOnTTY(base):
			counts.TTY++
		}
	}

	return counts
}

// isLoginShellOnTTY reports whether the process has a controlling terminal
// that is a real tty rather than a pseudo-terminal owned by sshd.
func isLoginShellOnTTY(procDir string) bool {
	f, err := os.Open(procDir + "/stat")
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return false
	}

	// The comm field can contain spaces inside parentheses, so split after it.
	line := scanner.Text()
	closing := strings.LastIndex(line, ")")
	if closing < 0 || closing+2 >= len(line) {
		return false
	}

	fields := strings.Fields(line[closing+2:])
	// Fields after comm: state, ppid, pgrp, session, tty_nr, ...
	if len(fields) < 5 {
		return false
	}

	ttyNr, err := strconv.Atoi(fields[4])

	return err == nil && ttyNr != 0
}

// LoadAverage returns the 1-minute load average, or 0 where unavailable.
func (r *Reader) LoadAverage() float64 {
	data, err := os.ReadFile(r.root + "proc/loadavg")
	if err != nil {
		return 0
	}

	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}

	load, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}

	return load
}

// Uptime returns the guest's uptime in seconds.
func (r *Reader) Uptime() int64 {
	data, err := os.ReadFile(r.root + "proc/uptime")
	if err != nil {
		return 0
	}

	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}

	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}

	return int64(seconds)
}

// Platform describes the guest architecture, reported at registration.
func Platform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}
