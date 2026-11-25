package pdfium

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"unsafe"
)

// PDFium copies this descriptor. It retains the numeric token until document close.
type fileAccess struct {
	length cLong
	read   uintptr
	token  uintptr
}

type fileSource struct {
	reader  io.ReaderAt
	size    int64
	token   uintptr
	release func()
	ctx     context.Context
	err     error
}

// PDFium calls the trampoline synchronously under the native gate.
var (
	fileSources = make(map[uintptr]*fileSource)
	fileToken   uintptr
	fileReader  = sync.OnceValue(func() uintptr { return newFileCallback(readBlock) })
)

func readBlock(token uintptr, offset cLong, dst unsafe.Pointer, count cLong) uintptr {
	source := fileSources[token]
	if source == nil || source.err != nil {
		return 0
	}
	if source.ctx != nil && source.ctx.Err() != nil {
		source.err = source.ctx.Err()
		return 0
	}
	size := cLong(source.size)
	if count == 0 || offset > size || count > size-offset {
		source.err = io.ErrUnexpectedEOF
		return 0
	}
	buffer := unsafe.Slice((*byte)(dst), int(count))
	n, err := source.reader.ReadAt(buffer, int64(offset))
	if n == len(buffer) && (err == nil || errors.Is(err, io.EOF)) {
		return 1
	}
	if err == nil || errors.Is(err, io.EOF) {
		err = io.ErrUnexpectedEOF
	}
	source.err = fmt.Errorf("read PDF at byte %d: %w", offset, err)
	return 0
}

func (doc *Document) startRead(ctx context.Context) {
	if doc.source != nil {
		doc.source.ctx, doc.source.err = ctx, nil
	}
}

func (doc *Document) finishRead() error {
	if doc.source == nil {
		return nil
	}
	err := doc.source.err
	doc.source.ctx, doc.source.err = nil, nil
	return err
}
