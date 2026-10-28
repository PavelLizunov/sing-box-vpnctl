package device

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/sagernet/sing/service/pause"
)

type blockedTimerPause struct {
	pause.Manager
	entered chan struct{}
	resume  chan struct{}
}

func (p *blockedTimerPause) WaitActive() {
	p.entered <- struct{}{}
	<-p.resume
}

func TestTimerDelSyncWhilePaused(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pm := &blockedTimerPause{entered: make(chan struct{}, 2), resume: make(chan struct{})}
		peer := &Peer{device: &Device{pauseManager: pm}}
		calls := 0
		timer := peer.NewTimer(func(*Peer) { calls++ })
		timer.Mod(0)
		synctest.Wait()
		<-pm.entered

		// Race: cancellation must not wait on the paused callback's running lock.
		if !timer.runningLock.TryLock() {
			close(pm.resume)
			synctest.Wait()
			timer.DelSync()
			t.Fatal("paused callback holds runningLock and would block DelSync")
		}
		timer.runningLock.Unlock()
		timer.DelSync()
		close(pm.resume)
		synctest.Wait()
		if calls != 0 || timer.IsPending() {
			t.Fatalf("canceled timer: calls=%d, pending=%v", calls, timer.IsPending())
		}
	})
}

func TestTimerDeadlineGuardOnWake(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pm := &blockedTimerPause{entered: make(chan struct{}, 2), resume: make(chan struct{})}
		peer := &Peer{device: &Device{pauseManager: pm}}
		calls := 0
		timer := peer.NewTimer(func(*Peer) { calls++ })
		defer timer.DelSync()
		timer.Mod(0)
		synctest.Wait()
		<-pm.entered

		timer.Mod(time.Hour)
		close(pm.resume)
		synctest.Wait()
		if calls != 0 || !timer.IsPending() {
			t.Fatalf("rearmed timer fired early: calls=%d, pending=%v", calls, timer.IsPending())
		}
		time.Sleep(time.Hour)
		synctest.Wait()
		if calls != 1 || timer.IsPending() {
			t.Fatalf("new deadline: calls=%d, pending=%v", calls, timer.IsPending())
		}
	})
}

func TestSinceIncludingSleep(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		time.Sleep(time.Minute)
		for _, stamp := range []time.Time{start, start.Round(0)} {
			if got := sinceIncludingSleep(stamp); got != time.Minute {
				t.Fatalf("age = %v, want one minute", got)
			}
		}
	})
}
