// Package gzipbuilder provides methods to construct GZIP compressed messages.
//
// The compression level taken throughout this package can be
// DefaultCompression, NoCompression, HuffmanOnly or any integer value between
// BestSpeed and BestCompression inclusive.
package gzipbuilder

import (
	"bufio"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"sync"
)

const packUncompressedData = true

var (
	// closeFooter is a zero-length type 0 block, w/ final block flag.
	closeFooter = []byte{0x01, 0x00, 0x00, 0xff, 0xff}

	crc32Mat = precomputeCRC32(crc32.IEEE)

	flateWriterPools [flate.BestCompression - flate.HuffmanOnly + 1]sync.Pool

	bufioWriterPool = &sync.Pool{
		New: func() interface{} {
			return bufio.NewWriterSize(nil, 1024)
		},
	}
)

func flateWriterPool(level int) *sync.Pool {
	return &flateWriterPools[level-flate.HuffmanOnly]
}

func flateWriterGet(w io.Writer, level int) *flate.Writer {
	if fw, ok := flateWriterPool(level).Get().(*flate.Writer); ok {
		fw.Reset(w)
		return fw
	}

	fw, _ := flate.NewWriter(w, level)
	return fw
}

func flateWriterPut(fw *flate.Writer, level int) {
	flateWriterPool(level).Put(fw)
}

// These constants are copied from the flate package, so that code that imports
// this package does not also have to import "compress/flate".
const (
	NoCompression      = flate.NoCompression
	BestSpeed          = flate.BestSpeed
	BestCompression    = flate.BestCompression
	DefaultCompression = flate.DefaultCompression
	HuffmanOnly        = flate.HuffmanOnly
)

func validCompressionLevel(level int) error {
	if level >= HuffmanOnly && level <= BestCompression {
		return nil
	}

	return fmt.Errorf("flate: invalid compression level %d: want value in range [%d, %d]",
		level, HuffmanOnly, BestCompression)
}

type sectionType int8

const (
	start sectionType = iota
	header
	precompressed
	compressed
	uncompressed
	finished
)

type builder struct {
	level int

	last sectionType

	rawDeflate bool

	uncompLen       uint16
	uncompHeaderIdx int

	w  io.Writer
	fw *flate.Writer

	size uint32
	crc  uint32

	err error

	scratch *[10]byte
}

// newBuilder constructs a new builder.
//
// If w is a *bytes.Buffer, successive uncompressed writes will be packed to
// use as little space as possible.
func newBuilder(w io.Writer, level int) builder {
	return builder{
		level: level,

		w: w,

		err: validCompressionLevel(level),

		scratch: new([10]byte),
	}
}

func (b *builder) canSetOption() bool {
	if b.last != start && b.err == nil {
		b.err = errors.New("gzipbuilder: setting options must be done before writing")
	}

	return b.err == nil
}

// RawDeflate sets the builder to only emit a raw DEFLATE stream without GZIP
// framing.
func (b *builder) RawDeflate() {
	if !b.canSetOption() {
		return
	}

	b.rawDeflate = true
}

// Err returns an error if one has occurred during building.
func (b *builder) Err() error {
	return b.err
}

func (b *builder) canWrite() bool {
	if b.last == finished && b.err == nil {
		b.err = errors.New("gzipbuilder: cannot modify Builder after Bytes called")
	}

	return b.err == nil
}

func (b *builder) writeHeader() {
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
	*b.scratch = [10]byte{
		0: gzipID1, 1: gzipID2, 2: gzipDeflate,
		9: 255, // unknown OS
	}

	switch b.level {
	case BestCompression:
		b.scratch[8] = 2
	case BestSpeed:
		b.scratch[8] = 4
	}

	_, b.err = b.w.Write(b.scratch[:])
}

// AddPrecompressedData adds data that was precompressed to the builder.
//
// The PrecompressedData must have been created with the same compression level
// as the builder.
func (b *builder) AddPrecompressedData(data *PrecompressedData) {
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
		b.crc = combineCRC32(crc32Mat, b.crc, data.crc, data.size)
	}

	_, b.err = b.w.Write(data.bytes)
}

// AddCompressedData compresses data and adds it to the builder.
//
// Note: AddCompressedData is vulnerable to exploits such as BREACH when used
// with secret data.
func (b *builder) AddCompressedData(data []byte) {
	if b.last == start {
		b.writeHeader()
	}
	if !b.canWrite() || len(data) == 0 {
		return
	}

	if b.last != compressed && b.fw != nil {
		b.fw.Reset(b.w)
	}
	b.last = compressed

	if b.fw == nil {
		b.fw = flateWriterGet(b.w, b.level)
	}

	if !b.rawDeflate {
		b.size += uint32(len(data))
		b.crc = crc32.Update(b.crc, crc32.IEEETable, data)
	}

	_, b.err = b.fw.Write(data)
}

func (b *builder) flushCompressed() bool {
	if b.last == compressed {
		b.err = b.fw.Flush()
	}

	return b.err == nil
}

// AddUncompressedData adds data to the builder without compressing it.
//
// Note: AddUncompressedData should be used to add secret data to the stream,
// such as authentication cookies, as it is immune to exploits such as BREACH.
func (b *builder) AddUncompressedData(data []byte) {
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
		if b.err != nil {
			return
		}

		data = data[maxLength:]
	}

	if buf, ok := b.w.(*bytes.Buffer); ok {
		b.uncompHeaderIdx = buf.Len()
		b.uncompLen = uint16(len(data))
	}

	b.zeroWrite(data)
}

func (b *builder) zeroWrite(p []byte) {
	b.scratch[0] = 0
	binary.LittleEndian.PutUint16(b.scratch[1:], uint16(len(p)))
	binary.LittleEndian.PutUint16(b.scratch[3:], ^uint16(len(p)))
	_, b.err = b.w.Write(b.scratch[:5])
	if b.err != nil {
		return
	}

	_, b.err = b.w.Write(p)
}

func (b *builder) packUncompressed(data []byte) []byte {
	const maxLength = ^uint16(0)
	buf, ok := b.w.(*bytes.Buffer)
	if !ok || b.uncompLen == maxLength {
		return data
	}

	remaining := maxLength - b.uncompLen
	if int(remaining) > len(data) {
		remaining = uint16(len(data))
	}
	b.uncompLen += remaining

	hdr := buf.Bytes()[b.uncompHeaderIdx : b.uncompHeaderIdx+5]
	binary.LittleEndian.PutUint16(hdr[1:], uint16(b.uncompLen))
	binary.LittleEndian.PutUint16(hdr[3:], ^uint16(b.uncompLen))

	buf.Write(data[:remaining])
	return data[remaining:]
}

type uncompressedWriter struct{ *builder }

func (w uncompressedWriter) Write(p []byte) (int, error) {
	w.AddUncompressedData(p)
	return len(p), w.err
}

// UncompressedWriter returns an io.Writer that will write uncompressed data to
// the builder.
func (b *builder) UncompressedWriter() io.Writer {
	return uncompressedWriter{b}
}

type compressedWriter struct{ *builder }

func (w compressedWriter) Write(p []byte) (int, error) {
	w.AddCompressedData(p)
	return len(p), w.err
}

// CompressedWriter returns an io.Writer that will write compressed data to the
// builder.
func (b *builder) CompressedWriter() io.Writer {
	return compressedWriter{b}
}

func (b *builder) finish() bool {
	switch b.last {
	case finished:
		return b.err == nil
	case compressed:
		if b.err == nil {
			b.err = b.fw.Close()
		}
	case start:
		b.writeHeader()
		fallthrough
	default:
		if b.err == nil {
			_, b.err = b.w.Write(closeFooter)
		}
	}
	b.last = finished

	if !b.rawDeflate && b.err == nil {
		binary.LittleEndian.PutUint32(b.scratch[:4], b.crc)
		binary.LittleEndian.PutUint32(b.scratch[4:], b.size)
		_, b.err = b.w.Write(b.scratch[:8])
	}

	if b.fw != nil {
		flateWriterPut(b.fw, b.level)
		b.fw = nil
	}

	return b.err == nil
}

// A Builder incrementally builds a compressed GZIP stream. It supports
// interleaving compressed, pre-compressed or uncompressed data into the
// output.
type Builder struct{ builder }

// NewBuilder creates a Builder using the given compression level.
func NewBuilder(level int) *Builder {
	return &Builder{newBuilder(new(bytes.Buffer), level)}
}

// Bytes returns the bytes written by the builder or an error if one has
// occurred during building.
func (b *Builder) Bytes() ([]byte, error) {
	if !b.finish() {
		return nil, b.err
	}

	return b.w.(*bytes.Buffer).Bytes(), nil
}

// BytesOrPanic returns the bytes written by the builder or panics if an error
// has occurred during building.
func (b *Builder) BytesOrPanic() []byte {
	if !b.finish() {
		panic(b.err)
	}

	return b.w.(*bytes.Buffer).Bytes()
}

// A Writer incrementally builds a compressed GZIP stream. It supports
// interleaving compressed, pre-compressed or uncompressed data into the
// output.
type Writer struct{ builder }

// NewWriter creates a Writer using the given compression level. Data is
// written to w.
func NewWriter(w io.Writer, level int) *Writer {
	bw := bufioWriterPool.Get().(*bufio.Writer)
	bw.Reset(w)
	return &Writer{newBuilder(bw, level)}
}

// Close closes the Writer by flushing any unwritten data to the underlying
// io.Writer and writing the GZIP footer. It returns an error if one has
// occurred during building. It does not close the underlying io.Writer.
func (b *Writer) Close() error {
	if b.last == finished {
		return b.err
	}

	buf := b.w.(*bufio.Writer)
	if b.finish() {
		b.err = buf.Flush()
	}

	bufioWriterPool.Put(buf)
	b.w = nil
	return b.err
}

// PrecompressedData holds data that was compressed once and can be passed to a
// Builder to avoid re-compressing static data.
type PrecompressedData struct {
	level int

	bytes []byte
	size  uint64
	crc   uint32
}

// PrecompressData compresses data at the given compression level.
func PrecompressData(data []byte, level int) (*PrecompressedData, error) {
	w := NewPrecompressedWriter(level)
	w.Write(data)
	return w.Data()
}

// PrecompressedWriter is an io.Writer that allows incrementally percompressing
// data. Writes to a PrecompressedWriter are compressed and returned by Data.
//
// The PrecompressedData returned from Data can be passed to a Builder to avoid
// re-compressing static data.
type PrecompressedWriter struct {
	level int

	buf *bytes.Buffer
	fw  *flate.Writer

	size uint64
	crc  uint32

	lastFlush bool

	err error
}

// NewPrecompressedWriter creates a PrecompressedWriter using the given
// compression level.
func NewPrecompressedWriter(level int) *PrecompressedWriter {
	w := &PrecompressedWriter{
		level: level,

		buf: new(bytes.Buffer),
	}
	w.fw, w.err = flate.NewWriter(w.buf, level)
	return w
}

// Reset discards the PrecompressedWriter's state and makes it equivalent to
// the result of its original state from NewPrecompressedWriter. This permits
// reusing a PrecompressedWriter rather than allocating a new one.
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

// Write writes a compressed form of p to the PrecompressedWriter.
//
// It will return any error that has occurred during writing.
func (w *PrecompressedWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}

	w.lastFlush = false

	w.size += uint64(len(p))
	w.crc = crc32.Update(w.crc, crc32.IEEETable, p)

	n, err := w.fw.Write(p)
	w.err = err
	return n, err
}

// Data returns a PrecompressedData struct containing the compressed data
// written to the writer. It will return any error that has occurred during
// writing.
//
// It is safe to call Data multiple times.
func (w *PrecompressedWriter) Data() (*PrecompressedData, error) {
	if w.err == nil && !w.lastFlush {
		w.err = w.fw.Flush()
		w.lastFlush = true
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
