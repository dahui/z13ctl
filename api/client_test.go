package api

// client_test.go — socket client behaviour against a stub daemon. These cover
// the failure modes that are invisible when the real daemon behaves: a daemon
// that accepts but never replies, and a caller that abandons a subscription.

import (
	"bufio"
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// stubDaemon listens on the path SocketPath() resolves to and runs handle for
// each connection. It redirects XDG_RUNTIME_DIR, so it must not run in parallel
// with other tests in this package.
func stubDaemon(t *testing.T, handle func(net.Conn)) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	if err := os.MkdirAll(dir+"/z13ctl", 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	ln, err := net.Listen("unix", SocketPath())
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go handle(c)
		}
	}()
}

// shortTimeouts shrinks the package timeouts so deadline tests stay fast.
func shortTimeouts(t *testing.T) {
	t.Helper()
	origDial, origCmd := dialTimeout, commandTimeout
	dialTimeout, commandTimeout = 200*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { dialTimeout, commandTimeout = origDial, origCmd })
}

func TestSendCommandReturnsNotHandledWhenDaemonAbsent(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir()) // no socket here
	handled, profile, err := SendProfileGet()
	if handled {
		t.Error("handled = true with no daemon listening, want false so the caller falls back")
	}
	if err != nil {
		t.Errorf("err = %v, want nil (absence is not an error)", err)
	}
	if profile != "" {
		t.Errorf("profile = %q, want empty", profile)
	}
}

// TestSendCommandTimesOutOnSilentDaemon is the regression guard for a CLI hang:
// DialTimeout only bounds connecting, so without a deadline on the connection a
// daemon that accepts and never replies blocks the caller forever.
func TestSendCommandTimesOutOnSilentDaemon(t *testing.T) {
	shortTimeouts(t)
	accepted := make(chan struct{}, 1)
	stubDaemon(t, func(c net.Conn) {
		_ = bufio.NewScanner(c).Scan()
		accepted <- struct{}{}
		// Hold the connection open without replying.
		time.Sleep(30 * time.Second)
		_ = c.Close()
	})

	done := make(chan error, 1)
	go func() {
		_, _, err := SendProfileGet()
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("SendProfileGet() = nil error, want a timeout error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SendProfileGet() never returned — the connection has no read deadline")
	}
	<-accepted
}

func TestSendCommandSurfacesDaemonError(t *testing.T) {
	stubDaemon(t, func(c net.Conn) {
		_ = bufio.NewScanner(c).Scan()
		_, _ = c.Write([]byte(`{"ok":false,"error":"boom"}` + "\n"))
		_ = c.Close()
	})

	handled, _, err := SendProfileGet()
	if !handled {
		t.Fatal("handled = false, want true when the daemon replied")
	}
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v, want the daemon's error text", err)
	}
}

func TestSendCommandParsesValue(t *testing.T) {
	stubDaemon(t, func(c net.Conn) {
		_ = bufio.NewScanner(c).Scan()
		_, _ = c.Write([]byte(`{"ok":true,"value":"performance"}` + "\n"))
		_ = c.Close()
	})

	handled, profile, err := SendProfileGet()
	if !handled || err != nil {
		t.Fatalf("SendProfileGet() = %v, %v, want handled with no error", handled, err)
	}
	if profile != "performance" {
		t.Errorf("profile = %q, want \"performance\"", profile)
	}
}

func TestSubscribeStreamsEvents(t *testing.T) {
	stubDaemon(t, func(c net.Conn) {
		_ = bufio.NewScanner(c).Scan()
		_, _ = c.Write([]byte(`{"ok":true}` + "\n"))
		for range 3 {
			_, _ = c.Write([]byte(`{"event":"gui-toggle"}` + "\n"))
		}
	})

	ch, cancel, err := Subscribe([]string{"gui-toggle"})
	if err != nil || ch == nil {
		t.Fatalf("Subscribe() = %v, %v, want a channel", ch, err)
	}
	defer cancel()

	for i := range 3 {
		select {
		case ev := <-ch:
			if ev != "gui-toggle" {
				t.Errorf("event %d = %q, want \"gui-toggle\"", i, ev)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting for event %d", i)
		}
	}
}

func TestSubscribeRejectsDaemonNack(t *testing.T) {
	stubDaemon(t, func(c net.Conn) {
		_ = bufio.NewScanner(c).Scan()
		_, _ = c.Write([]byte(`{"ok":false,"error":"nope"}` + "\n"))
		_ = c.Close()
	})

	ch, cancel, err := Subscribe([]string{"gui-toggle"})
	if err == nil {
		if cancel != nil {
			cancel()
		}
		t.Fatal("Subscribe() = nil error, want the daemon's rejection")
	}
	if ch != nil {
		t.Error("channel is non-nil on a rejected subscription")
	}
}

// TestSubscribeCancelReleasesAbandonedReader is the regression guard for a
// goroutine leak: a caller that stops reading fills the 8-slot buffer and parks
// the reader in a channel send, where closing the connection cannot reach it.
// cancel() must unblock it so the goroutine exits and the channel closes.
func TestSubscribeCancelReleasesAbandonedReader(t *testing.T) {
	stubDaemon(t, func(c net.Conn) {
		_ = bufio.NewScanner(c).Scan()
		_, _ = c.Write([]byte(`{"ok":true}` + "\n"))
		for {
			if _, err := c.Write([]byte(`{"event":"gui-toggle"}` + "\n")); err != nil {
				return
			}
			time.Sleep(time.Millisecond)
		}
	})

	ch, cancel, err := Subscribe([]string{"gui-toggle"})
	if err != nil || ch == nil {
		t.Fatalf("Subscribe() = %v, %v", ch, err)
	}
	// Deliberately never read: let the buffer fill and park the reader.
	time.Sleep(200 * time.Millisecond)
	cancel()
	cancel() // must be idempotent

	for range 30 {
		time.Sleep(100 * time.Millisecond)
		if !subscribeReaderRunning() {
			return
		}
	}
	t.Error("the Subscribe reader goroutine is still parked after cancel — it leaks")
}

// subscribeReaderRunning reports whether any goroutine is inside Subscribe's
// reader closure.
func subscribeReaderRunning() bool {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	return strings.Contains(string(buf[:n]), "api.Subscribe.func")
}
