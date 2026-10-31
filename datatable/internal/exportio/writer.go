// Package exportio owns the standard io.Writer adapters used by datatable export.
package exportio

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"hash"
	"io"
)

type Writer struct {
	Context          context.Context
	File             io.Writer
	Digest           hash.Hash
	Maximum, Written int64
}

func (w *Writer) Write(p []byte) (int, error) {
	if err := w.Context.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > w.Maximum-w.Written {
		return 0, invalid("export byte limit exceeded")
	}
	n, err := w.File.Write(p)
	w.Written += int64(n)
	if n > 0 {
		_, _ = w.Digest.Write(p[:n])
	}
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	return n, err
}

type XMLBudget struct {
	Writer    io.Writer
	Remaining int64
}

func (w *XMLBudget) Write(p []byte) (int, error) {
	if int64(len(p)) > w.Remaining {
		return 0, invalid("export XML byte limit exceeded")
	}
	n, err := w.Writer.Write(p)
	w.Remaining -= int64(n)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	return n, err
}

func invalid(message string) error { return safeerror.NewFault(fault.Invalid, message) }
