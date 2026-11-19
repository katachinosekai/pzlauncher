package music

import (
	"sync"

	"github.com/gopxl/beep/v2"
)

var (
	outMu     sync.Mutex
	outStream beep.Streamer
	initOnce  sync.Once
	initErr   error
)

func lockOut()   { outMu.Lock() }
func unlockOut() { outMu.Unlock() }

func setStream(s beep.Streamer) {
	outMu.Lock()
	outStream = s
	outMu.Unlock()
}

func fill(buf [][2]float64) {
	outMu.Lock()
	defer outMu.Unlock()
	n := 0
	if outStream != nil {
		n, _ = outStream.Stream(buf)
	}
	for i := n; i < len(buf); i++ {
		buf[i] = [2]float64{}
	}
}

func initOutput() error {
	initOnce.Do(func() { initErr = openOutput() })
	return initErr
}
