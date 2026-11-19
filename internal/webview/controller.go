package webview

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

type Controller struct {
	Exe        string
	Args       []string
	Env        []string
	OnEvent    func(Event)
	OnFallback func(err error)

	mu       sync.Mutex
	cmd      *exec.Cmd
	in       io.WriteCloser
	ready    bool
	done     chan struct{}
	fallback bool
	notified bool
}

func (c *Controller) Fallback() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fallback
}

func (c *Controller) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.in != nil && c.ready
}

func (c *Controller) Send(cmd Command) error {
	if !cmd.Valid() {
		return errors.New("invalid browser command")
	}
	c.mu.Lock()
	if c.fallback {
		c.mu.Unlock()
		return ErrUnavailable
	}
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		if c.in == nil {
			if err = c.startLocked(); err != nil {
				break
			}
		}
		allowForeground(c.cmd)
		if err = WriteCommand(c.in, cmd); err == nil {
			c.mu.Unlock()
			return nil
		}
		c.stopLocked()
	}
	c.fallback = true
	notify := !c.notified
	c.notified = true
	c.mu.Unlock()
	if notify && c.OnFallback != nil {
		c.OnFallback(err)
	}
	return ErrUnavailable
}

func (c *Controller) startLocked() error {
	exe := c.Exe
	if exe == "" {
		var err error
		if exe, err = os.Executable(); err != nil {
			return err
		}
	}
	args := c.Args
	if args == nil {
		args = []string{HelperFlag}
	}
	cmd := exec.Command(exe, args...)
	if c.Env != nil {
		cmd.Env = c.Env
	}
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	c.cmd, c.in, c.ready = cmd, in, false
	done := make(chan struct{})
	c.done = done
	go func() {
		defer close(done)
		c.watch(cmd, out)
	}()
	return nil
}

func (c *Controller) watch(cmd *exec.Cmd, out io.Reader) {
	var unavailable error
	_ = ReadEvents(out, func(e Event) {
		switch e.Event {
		case EvReady:
			c.mu.Lock()
			if c.cmd == cmd {
				c.ready = true
			}
			c.mu.Unlock()
		case EvUnavailable:
			unavailable = errors.New(e.Error)
		default:
			if c.OnEvent != nil {
				c.OnEvent(e)
			}
		}
	})
	werr := cmd.Wait()
	c.mu.Lock()
	if c.cmd != cmd {
		c.mu.Unlock()
		return
	}
	wasReady := c.ready
	c.cmd, c.in, c.ready = nil, nil, false
	if wasReady && unavailable == nil {
		c.mu.Unlock()
		return
	}
	if unavailable == nil {
		unavailable = werr
		if unavailable == nil {
			unavailable = ErrUnavailable
		}
	}
	c.fallback = true
	notify := !c.notified
	c.notified = true
	c.mu.Unlock()
	if notify && c.OnFallback != nil {
		c.OnFallback(unavailable)
	}
}

func (c *Controller) stopLocked() {
	if c.in != nil {
		_ = c.in.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	c.cmd, c.in, c.ready = nil, nil, false
}

func (c *Controller) CloseWait(d time.Duration) {
	c.mu.Lock()
	done := c.done
	c.mu.Unlock()
	c.Close()
	if done != nil {
		select {
		case <-done:
		case <-time.After(d):
		}
	}
}

func (c *Controller) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.in != nil {
		_ = WriteCommand(c.in, Command{Op: OpQuit})
		_ = c.in.Close()
	}
	c.cmd, c.in, c.ready = nil, nil, false
}
