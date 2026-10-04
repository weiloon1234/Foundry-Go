//go:build cgo && (darwin || linux || freebsd || windows)

package imaging

/*
#include <stdint.h>
*/
import "C"

import (
	"context"
	"io"
	"runtime/cgo"
	"sync"
	"unsafe"
)

type nativeCall struct {
	ctx      context.Context
	output   *boundedOutput
	mu       sync.Mutex
	position int64
}

//export foundryVipsCanceled
func foundryVipsCanceled(id C.uintptr_t) C.int {
	call := cgo.Handle(id).Value().(*nativeCall)
	if call.ctx.Err() != nil {
		return 1
	}
	return 0
}

//export foundryVipsWrite
func foundryVipsWrite(id C.uintptr_t, data unsafe.Pointer, length C.int64_t) C.int64_t {
	call := cgo.Handle(id).Value().(*nativeCall)
	call.mu.Lock()
	defer call.mu.Unlock()
	out := call.output
	if out == nil || out.err != nil {
		return -1
	}
	if err := call.ctx.Err(); err != nil {
		out.err = err
		return -1
	}
	n := int64(length)
	if n < 0 || n > out.maximum-call.position {
		out.err = limited()
		return -1
	}
	end := call.position + n
	if end > int64(len(out.data)) {
		out.data = append(out.data, make([]byte, end-int64(len(out.data)))...)
	}
	copy(out.data[call.position:end], unsafe.Slice((*byte)(data), int(n)))
	call.position = end
	return length
}

//export foundryVipsRead
func foundryVipsRead(id C.uintptr_t, data unsafe.Pointer, length C.int64_t) C.int64_t {
	call := cgo.Handle(id).Value().(*nativeCall)
	call.mu.Lock()
	defer call.mu.Unlock()
	if call.output == nil || call.output.err != nil || call.ctx.Err() != nil || length < 0 {
		return -1
	}
	if call.position >= int64(len(call.output.data)) {
		return 0
	}
	n := min(int64(length), int64(len(call.output.data))-call.position)
	copy(unsafe.Slice((*byte)(data), int(n)), call.output.data[call.position:call.position+n])
	call.position += n
	return C.int64_t(n)
}

//export foundryVipsSeek
func foundryVipsSeek(id C.uintptr_t, offset C.int64_t, whence C.int) C.int64_t {
	call := cgo.Handle(id).Value().(*nativeCall)
	call.mu.Lock()
	defer call.mu.Unlock()
	if call.output == nil || call.output.err != nil || call.ctx.Err() != nil {
		return -1
	}
	base := int64(0)
	switch int(whence) {
	case io.SeekStart:
	case io.SeekCurrent:
		base = call.position
	case io.SeekEnd:
		base = int64(len(call.output.data))
	default:
		return -1
	}
	if int64(offset) < -base || int64(offset) > call.output.maximum-base {
		call.output.err = limited()
		return -1
	}
	call.position = base + int64(offset)
	return C.int64_t(call.position)
}
