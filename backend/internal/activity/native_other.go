//go:build !darwin || !cgo

package activity

import "context"

type NativeSampler struct{}

func PlatformSupported() bool                                        { return false }
func (NativeSampler) Sample(context.Context, Config) (Sample, error) { return Sample{}, ErrUnsupported }
