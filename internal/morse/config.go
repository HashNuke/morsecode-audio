// Package morse encodes and incrementally decodes bounded International Morse PCM.
package morse

import "errors"

type Config struct {
	SampleRate          int
	UnitMillis          int
	Frequency           int
	Amplitude           int
	MaxTextBytes        int
	MaxDurationSeconds  int
	AutoCommit          bool
	CommitSilenceMillis int
}

func DefaultConfig() Config {
	return Config{SampleRate: 16000, UnitMillis: 60, Frequency: 700, Amplitude: 4096, MaxTextBytes: 4096, MaxDurationSeconds: 300, AutoCommit: true}
}

func (c Config) validate() error {
	if c.CommitSilenceMillis != 0 && (c.CommitSilenceMillis < c.UnitMillis*14 || c.CommitSilenceMillis > 30000) {
		return errors.New("invalid_morse_commit_silence")
	}
	validRate := false
	for _, r := range []int{8000, 16000, 22050, 24000, 32000, 44100, 48000} {
		if c.SampleRate == r {
			validRate = true
		}
	}
	if !validRate || c.UnitMillis < 20 || c.UnitMillis > 200 || c.Frequency < 300 || c.Frequency > 2000 || c.Frequency >= c.SampleRate/2 || c.Amplitude < 301 || c.Amplitude > 8191 || c.MaxTextBytes < 1 || c.MaxTextBytes > 4096 || c.MaxDurationSeconds < 1 || c.MaxDurationSeconds > 3600 {
		return errors.New("invalid_morse_configuration")
	}
	return nil
}
