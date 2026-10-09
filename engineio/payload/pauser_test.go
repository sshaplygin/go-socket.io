package payload

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPauserTrigger(t *testing.T) {
	should := assert.New(t)
	p := newPauser()

	ok := p.Working()
	should.True(ok)

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()

		ok := p.Pause()
		should.True(ok)

		defer p.Resume()
	}()

	select {
	case <-p.PausingTrigger():
	case <-time.After(time.Second / 10):
		should.True(false, "should not run here")
	}
	select {
	case <-p.PausedTrigger():
		should.True(false, "should not run here")
	case <-time.After(time.Second / 10):
	}

	go func() {
		time.Sleep(time.Second / 10)
		p.Done()
	}()

	select {
	case <-p.PausedTrigger():
	case <-time.After(time.Second):
		should.True(false, "should not run here")
	}

	wg.Wait()

	select {
	case <-p.PausingTrigger():
		should.True(false, "should not run here")
	case <-p.PausedTrigger():
		should.True(false, "should not run here")
	case <-time.After(time.Second / 10):
	}

}

func TestPauserPauseOnlyOnce(t *testing.T) {
	should := assert.New(t)
	p := newPauser()
	s := make(chan int)

	go func() {
		ok := p.Pause()
		should.True(ok)
		defer p.Resume()
		s <- 1
		<-s
	}()

	<-s
	ok := p.Pause()
	should.False(ok)
	s <- 1
}

func TestPauserPauseAfterResume(t *testing.T) {
	should := assert.New(t)
	p := newPauser()

	ok := p.Pause()
	should.True(ok)
	p.Resume()

	ok = p.Pause()
	should.True(ok)
	p.Resume()
}

func TestPauserPauseMultiplyResumeOnce(t *testing.T) {
	should := assert.New(t)
	p := newPauser()

	ok := p.Pause()
	should.True(ok)
	for i := 0; i < 10; i++ {
		ok = p.Pause()
		should.False(ok)
	}
	p.Resume()

	// check if it reset to normal
	ok = p.Pause()
	should.True(ok)
	p.Resume()
}

func TestPauserConcurrencyWorkingDone(t *testing.T) {
	p := newPauser()
	wg := sync.WaitGroup{}

	f := func() {
		defer wg.Done()
		should := assert.New(t)
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		time.Sleep(time.Microsecond * time.Duration(r.Intn(100)))
		ok := p.Working()
		should.True(ok)
		defer p.Done()
		time.Sleep(time.Microsecond * time.Duration(r.Intn(100)))
	}

	max := 1000
	wg.Add(max)
	for i := 0; i < max; i++ {
		go f()
	}

	wg.Wait()
}

func TestPauserCanWorkingDuringPauseWaiting(t *testing.T) {
	should := assert.New(t)
	p := newPauser()
	wg := sync.WaitGroup{}

	ok := p.Working()
	should.True(ok)

	wg.Add(1)
	go func() {
		defer wg.Done()
		o := p.Pause()
		should.True(o)
		defer p.Resume()
	}()
	<-p.PausingTrigger()

	ok = p.Working()
	should.True(ok)

	p.Done()
	p.Done()
	wg.Wait()
}

func TestPauserPauseWhenAllDone(t *testing.T) {
	should := assert.New(t)
	p := newPauser()

	n := 10
	for i := 0; i < n; i++ {
		ok := p.Working()
		should.True(ok)
	}
	for i := 0; i < n; i++ {
		p.Done()
	}

	ok := p.Pause()
	should.True(ok)

	ok = p.Pause()
	should.False(ok)

	p.Resume()
}

func TestPauserOnlyOnePauseAfterWaiting(t *testing.T) {
	should := assert.New(t)
	count := int64(0)
	wg := sync.WaitGroup{}
	p := newPauser()

	ok := p.Working()
	should.True(ok)

	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func() {
			defer wg.Done()
			ok := p.Pause()
			if ok {
				atomic.AddInt64(&count, 1)
			}
		}()
	}
	time.Sleep(time.Second / 10) // Wait all goroutines pausing

	p.Done()
	wg.Wait()
	should.Equal(int64(1), count)
	p.Resume()
}

func TestPauserCannotWorkingAfterPause(t *testing.T) {
	should := assert.New(t)
	p := newPauser()

	ok := p.Pause()
	should.True(ok)
	defer p.Resume()

	ok = p.Working()
	should.False(ok)
	p.Done()
}

// TestPauserRandom starts 1000 workers around a pending Pause (half before it, half after its
// trigger) and checks that Pause waits for all of them. Channels, not sleeps, fix the order.
func TestPauserRandom(t *testing.T) {
	p := newPauser()
	var started, done sync.WaitGroup
	var released int32
	release := make(chan struct{})

	assert.True(t, p.Working()) // held until every worker runs, so Pause cannot finish early
	for i := 0; i < 1000; i++ {
		started.Add(1)
		done.Add(1)
		go func(late bool) {
			defer done.Done()
			if late {
				<-p.PausingTrigger()
			}
			ok := p.Working()
			started.Done()
			assert.True(t, ok)
			<-release
			p.Done()
		}(i%2 == 1)
	}

	paused := make(chan bool) // Pause returned true after the workers were released
	go func() { ok := p.Pause(); paused <- ok && atomic.LoadInt32(&released) == 1 }()
	<-p.PausingTrigger()
	started.Wait()
	atomic.StoreInt32(&released, 1)
	close(release)
	p.Done()
	assert.True(t, <-paused)
	done.Wait()
}
