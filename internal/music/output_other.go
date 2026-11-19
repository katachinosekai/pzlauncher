//go:build !windows

package music

import (
	"github.com/jfreymuth/pulse"
)

func openOutput() error {
	c, err := pulse.NewClient(pulse.ClientApplicationName("PZ Launcher"))
	if err != nil {
		return err
	}
	var frames [][2]float64
	reader := pulse.Float32Reader(func(out []float32) (int, error) {
		n := len(out) / 2
		if cap(frames) < n {
			frames = make([][2]float64, n)
		}
		frames = frames[:n]
		fill(frames)
		for i, f := range frames {
			out[2*i] = float32(f[0])
			out[2*i+1] = float32(f[1])
		}
		return n * 2, nil
	})
	stream, err := c.NewPlayback(reader, pulse.PlaybackStereo, pulse.PlaybackSampleRate(int(sampleRate)), pulse.PlaybackLatency(0.1))
	if err != nil {
		c.Close()
		return err
	}
	stream.Start()
	return nil
}
