// Package gzipbuilder provides methods to construct gzip compressed messages.
package gzipbuilder

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
)

const packUncompressedData = true

var closeFooter = []byte{0x01, 0x00, 0x00, 0xff, 0xff} // zero-length type 0 block, w/ final block flag

// These constants are copied from the flate package, so that code that imports
// this package does not also have to import "compress/flate".
const (
	NoCompression      = flate.NoCompression
	BestSpeed          = flate.BestSpeed
	BestCompression    = flate.BestCompression
	DefaultCompression = flate.DefaultCompression
	HuffmanOnly        = flate.HuffmanOnly
)

type sectionType int

const (
	start sectionType = iota
	header
	precompressed
	compressed
	uncompressed
	finished
)

type Builder struct {
	level      int
	rawDeflate bool

	last sectionType

	uncompHeaderIdx int
	uncompLen       uint16

	buf  *bytes.Buffer
	size uint32
	crc  uint32

	fw *flate.Writer

	err error
}

func NewBuilder(level int) *Builder {
	b := &Builder{
		level: level,

		buf: new(bytes.Buffer),
	}
	if level < HuffmanOnly || level > BestCompression {
		b.err = fmt.Errorf("flate: invalid compression level %d: want value in range [%d, %d]",
			level, HuffmanOnly, BestCompression)
	}

	return b
}

func (b *Builder) canSetOption() bool {
	if b.last != start && b.err == nil {
		b.err = errors.New("gzipbuilder: setting options must be done before writing")
	}

	return b.err == nil
}

func (b *Builder) RawDeflate() {
	if !b.canSetOption() {
		return
	}

	b.rawDeflate = true
}

func (b *Builder) Err() error {
	return b.err
}

func (b *Builder) canWrite() bool {
	if b.last == finished && b.err == nil {
		b.err = errors.New("gzipbuilder: cannot modify Builder after Bytes called")
	}

	return b.err == nil
}

func (b *Builder) writeHeader() {
	if b.err != nil || b.last != start {
		return
	}
	b.last = header

	if b.rawDeflate {
		return
	}

	const (
		gzipID1     = 0x1f
		gzipID2     = 0x8b
		gzipDeflate = 8
	)
	gzipHdr := [10]byte{
		0: gzipID1, 1: gzipID2, 2: gzipDeflate,
		9: 255, // unknown OS
	}

	switch b.level {
	case BestCompression:
		gzipHdr[8] = 2
	case BestSpeed:
		gzipHdr[8] = 4
	}

	b.buf.Write(gzipHdr[:])
}

var crc32Mat = precomputeCRC32(crc32.IEEE)

func (b *Builder) AddPrecompressedData(data *PrecompressedData) {
	if b.last == start {
		b.writeHeader()
	}
	if !b.canWrite() {
		return
	}
	if b.level != data.level {
		b.err = errors.New("gzipbuilder: compression level mismatch")
		return
	}
	// Check for an empty write after the compression level, this way we
	// always surface a mismatch error regardless of the size.
	if data.size == 0 || !b.flushCompressed() {
		return
	}
	b.last = precompressed

	if !b.rawDeflate {
		b.size += uint32(data.size)
		b.crc = combineCRC32(crc32Mat, b.crc, data.crc, uint64(data.size))
	}

	b.buf.Write(data.bytes)
}

func (b *Builder) AddCompressedData(data []byte) {
	if b.last == start {
		b.writeHeader()
	}
	if !b.canWrite() || len(data) == 0 {
		return
	}

	if b.last != compressed && b.fw != nil {
		b.fw.Reset(b.buf)
	}
	b.last = compressed

	if b.fw == nil {
		b.fw, _ = flate.NewWriter(b.buf, b.level)
	}

	if !b.rawDeflate {
		b.size += uint32(len(data))
		b.crc = crc32.Update(b.crc, crc32.IEEETable, data)
	}

	_, b.err = b.fw.Write(data)
}

func (b *Builder) flushCompressed() bool {
	if b.last == compressed {
		b.err = b.fw.Flush()
	}

	return b.err == nil
}

func (b *Builder) AddUncompressedData(data []byte) {
	if b.last == start {
		b.writeHeader()
	}
	if !b.canWrite() || len(data) == 0 || !b.flushCompressed() {
		return
	}

	if !b.rawDeflate {
		b.size += uint32(len(data))
		b.crc = crc32.Update(b.crc, crc32.IEEETable, data)
	}

	if packUncompressedData && b.last == uncompressed {
		data = b.packUncompressed(data)
		if len(data) == 0 {
			return
		}
	}
	b.last = uncompressed

	const maxLength = ^uint16(0)
	for len(data) > int(maxLength) {
		b.zeroWrite(data[:maxLength])
		data = data[maxLength:]
	}

	b.uncompHeaderIdx = b.buf.Len()
	b.uncompLen = uint16(len(data))

	b.zeroWrite(data)
}

func (b *Builder) zeroWrite(p []byte) {
	/* The following code is equivalent to:
	 *  hbw := newHuffmanBitWriter(b.buf)
	 *  hbw.writeStoredHeader(len(p), false)
	 *
	 *  if hbw.err == nil {
	 *          hbw.writeBytes(p)
	 *  }
	 *
	 *  b.err = hbw.err
	 */

	var hdr [5]byte
	binary.LittleEndian.PutUint16(hdr[1:], uint16(len(p)))
	binary.LittleEndian.PutUint16(hdr[3:], ^uint16(len(p)))
	b.buf.Write(hdr[:])

	b.buf.Write(p)
}

func (b *Builder) packUncompressed(data []byte) []byte {
	const maxLength = ^uint16(0)
	if b.uncompLen == maxLength {
		return data
	}

	remaining := maxLength - b.uncompLen
	if int(remaining) > len(data) {
		remaining = uint16(len(data))
	}
	b.uncompLen += remaining

	hdr := b.buf.Bytes()[b.uncompHeaderIdx : b.uncompHeaderIdx+5]
	binary.LittleEndian.PutUint16(hdr[1:], uint16(b.uncompLen))
	binary.LittleEndian.PutUint16(hdr[3:], ^uint16(b.uncompLen))

	b.buf.Write(data[:remaining])
	return data[remaining:]
}

func (b *Builder) finish() bool {
	if b.err != nil {
		b.fw = nil // See comment below.
		return false
	}

	switch b.last {
	case finished:
		return true
	case compressed:
		if b.err = b.fw.Close(); b.err != nil {
			return false
		}
	case start:
		b.writeHeader()
		fallthrough
	default:
		b.buf.Write(closeFooter)
	}
	b.last = finished

	// Allow garbage collector to free the *flate.Writer now that
	// it can no longer be used.
	b.fw = nil

	if b.rawDeflate {
		return true
	}

	var footer [8]byte
	binary.LittleEndian.PutUint32(footer[:4], b.crc)
	binary.LittleEndian.PutUint32(footer[4:], b.size)
	b.buf.Write(footer[:])
	return true
}

func (b *Builder) Bytes() ([]byte, error) {
	if !b.finish() {
		return nil, b.err
	}

	return b.buf.Bytes(), nil
}

func (b *Builder) BytesOrPanic() []byte {
	if !b.finish() {
		panic(b.err)
	}

	return b.buf.Bytes()
}

type uncompressedWriter struct{ *Builder }

func (w uncompressedWriter) Write(p []byte) (int, error) {
	w.AddUncompressedData(p)
	return len(p), w.err
}

func (b *Builder) UncompressedWriter() io.Writer {
	return uncompressedWriter{b}
}

type compressedWriter struct{ *Builder }

func (w compressedWriter) Write(p []byte) (int, error) {
	w.AddCompressedData(p)
	return len(p), w.err
}

func (b *Builder) CompressedWriter() io.Writer {
	return compressedWriter{b}
}

type PrecompressedData struct {
	level int

	bytes []byte
	size  int64
	crc   uint32
}

func PrecompressData(data []byte, level int) (*PrecompressedData, error) {
	w := NewPrecompressedWriter(level)
	w.Write(data)
	return w.Data()
}

type PrecompressedWriter struct {
	level int

	buf *bytes.Buffer
	fw  *flate.Writer

	size int64
	crc  uint32

	err error
}

func NewPrecompressedWriter(level int) *PrecompressedWriter {
	w := &PrecompressedWriter{
		level: level,

		buf: new(bytes.Buffer),
	}
	w.fw, w.err = flate.NewWriter(w.buf, level)
	return w
}

func (w *PrecompressedWriter) Reset() {
	if w.fw == nil {
		// The compression level was invalid, w.err contains the error.
		return
	}

	*w = PrecompressedWriter{
		level: w.level,

		buf: new(bytes.Buffer),
		fw:  w.fw,
	}
	w.fw.Reset(w.buf)
}

func (w *PrecompressedWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}

	w.size += int64(len(p))
	w.crc = crc32.Update(w.crc, crc32.IEEETable, p)

	n, err := w.fw.Write(p)
	w.err = err
	return n, err
}

func (w *PrecompressedWriter) Data() (*PrecompressedData, error) {
	if w.err == nil {
		w.err = w.fw.Flush()
	}
	if w.err != nil {
		return nil, w.err
	}

	return &PrecompressedData{
		level: w.level,

		bytes: w.buf.Bytes(),
		size:  w.size,
		crc:   w.crc,
	}, nil
}
