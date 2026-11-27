package gui

// settle.go — a slider's settle-then-send, shared by the charge limit, TDP,
// rumble and undervolt sliders (it was four hand copies).

import (
	"time"

	"github.com/diamondburned/gotk4/pkg/glib/v2"

	"github.com/dahui/voltaire/v2/internal/apiresult"
)

// settleSend waits for a slider to rest before writing: a drag passes
// through every value between its ends, and each would be a real write. The
// delay is about ordering more than chattiness — goroutines per move could
// land at the daemon out of order and store an intermediate value.
//
// pending is true from the first move of a drag until the send for its last
// move has landed, and a section's sync must leave the slider alone while it
// is: a background refresh would otherwise move it out from under the drag.
// gen counts drags, so an earlier send landing cannot clear a later drag's
// pending.
type settleSend struct {
	delay   time.Duration
	timer   *time.Timer
	gen     int
	pending bool
}

// queue schedules a send, on the GTK thread, from a slider's value-changed
// handler. After the rest, read runs on the GTK thread (widgets are read
// nowhere else) and send on a goroutine; a failure is reported under op. With
// refresh, state is refetched afterwards either way, which puts the slider on
// what the daemon holds when the write was clamped or refused.
//
// A change made by sync is not an edit: the caller's handler is skipped while
// w.syncing, checked here because by the time the timer fires the sync has
// long finished. Without it every drawer open wrote the value straight back.
func (q *settleSend) queue(w *Window, read func() int, send func(int) (bool, error), op string, refresh bool) {
	if w.syncing {
		return
	}
	q.pending = true
	q.gen++
	if q.timer != nil {
		q.timer.Stop()
	}
	q.timer = time.AfterFunc(q.delay, func() {
		glib.IdleAdd(func() bool {
			v, gen := read(), q.gen
			go func() {
				err := apiresult.Err(send(v))
				glib.IdleAdd(func() bool {
					if q.gen == gen {
						q.pending = false
					}
					return false
				})
				if err != nil {
					w.reportError(op, err)
				} else {
					w.clearErrorAsync()
				}
				if refresh {
					w.refreshState()
				}
			}()
			return false
		})
	})
}
