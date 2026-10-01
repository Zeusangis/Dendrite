//go:build darwin && cgo

package activity

/*
#cgo LDFLAGS: -framework Cocoa -framework ApplicationServices -framework IOKit
#include <stdlib.h>
char *dendrite_sample(int titles, int browser);
*/
import "C"
import (
	"context"
	"encoding/json"
	"unsafe"
)

type NativeSampler struct{}

func PlatformSupported() bool { return true }
func (NativeSampler) Sample(ctx context.Context, cfg Config) (Sample, error) {
	var sample Sample
	if err := ctx.Err(); err != nil {
		return sample, err
	}
	titles := C.int(0)
	if cfg.WindowTitles {
		titles = 1
	}
	browser := C.int(0)
	if cfg.BrowserPages {
		browser = 1
	}
	raw := C.dendrite_sample(titles, browser)
	defer C.free(unsafe.Pointer(raw))
	err := json.Unmarshal([]byte(C.GoString(raw)), &sample)
	return sample, err
}
