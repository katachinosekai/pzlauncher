package music

import (
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/speaker"
)

func openOutput() error {
	if err := speaker.Init(sampleRate, sampleRate.N(time.Second/10)); err != nil {
		return err
	}
	speaker.Play(beep.StreamerFunc(func(s [][2]float64) (int, bool) {
		fill(s)
		return len(s), true
	}))
	return nil
}
