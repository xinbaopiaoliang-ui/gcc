//go:build !linux

package systemstats

import "time"

type Sampler struct{}

func NewSampler() *Sampler {
	return &Sampler{}
}

func (s *Sampler) Snapshot(time.Time) *Snapshot {
	return nil
}
