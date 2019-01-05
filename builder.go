// Package gzipbuilder provides methods to construct gzip compressed messages.
package gzipbuilder

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"strings"
)

// zero-length type 0 block
var syncFlushFooter = []byte{0x00, 0x00, 0x00, 0xff, 0xff}

// These constants are copied from the flate package, so that code that imports
// this package does not also have to import "compress/flate".
const (
	NoCompression      = flate.NoCompression
	BestSpeed          = flate.BestSpeed
	BestCompression    = flate.BestCompression
	DefaultCompression = flate.DefaultCompression
	HuffmanOnly        = flate.HuffmanOnly
)

type segment struct {
	bytes []byte
	size  int
	crc   uint32
}

// Template implements simple template engine, which can be used for fast
// tags' (aka placeholders) substitution.
type Template struct {
	template []byte
	texts    []segment
	tags     []string
	size     uint32
	gzipHdr  [10]byte
}

// New parses the given template using the given startTag and endTag
// as tag start and tag end.
//
// The returned template can be executed by concurrently running goroutines
// using Execute* methods.
//
// New panics if the given template cannot be parsed. Use NewTemplate instead
// if template may contain errors.
func New(template, startTag, endTag string, level int) *Template {
	t, err := NewTemplate(template, startTag, endTag, level)
	if err != nil {
		panic(err)
	}
	return t
}

// NewTemplate parses the given template using the given startTag and endTag
// as tag start and tag end.
//
// The returned template can be executed by concurrently running goroutines
// using Execute* methods.
func NewTemplate(template, startTag, endTag string, level int) (*Template, error) {
	if len(startTag) == 0 {
		panic("gziptemplate: startTag cannot be empty")
	}
	if len(endTag) == 0 {
		panic("gziptemplate: endTag cannot be empty")
	}

	var t Template

	const (
		gzipID1     = 0x1f
		gzipID2     = 0x8b
		gzipDeflate = 8
	)
	t.gzipHdr = [10]byte{
		0: gzipID1, 1: gzipID2, 2: gzipDeflate,
		9: 255, // unknown OS
	}

	if level == BestCompression {
		t.gzipHdr[8] = 2
	} else if level == BestSpeed {
		t.gzipHdr[8] = 4
	}

	tagsCount := strings.Count(template, startTag)
	if tagsCount == 0 {
		var buf bytes.Buffer
		gw, err := gzip.NewWriterLevel(&buf, level)
		if err != nil {
			return nil, err
		}

		if _, err := gw.Write([]byte(template)); err != nil {
			return nil, err
		}

		if err := gw.Close(); err != nil {
			return nil, err
		}

		t.template = buf.Bytes()
		return &t, nil
	}

	t.texts = make([]segment, 0, tagsCount+1)
	t.tags = make([]string, 0, tagsCount)

	fw, err := flate.NewWriter(nil, level)
	if err != nil {
		return nil, err
	}

	s := []byte(template)
	st := template

	for {
		var buf bytes.Buffer
		fw.Reset(&buf)

		n := strings.Index(st, startTag)
		ni := n
		if n < 0 {
			ni = len(st)
		}

		if _, err := fw.Write(s[:ni]); err != nil {
			return nil, err
		}

		var err error
		if n < 0 {
			err = fw.Close()
		} else {
			err = fw.Flush()
		}
		if err != nil {
			return nil, err
		}

		t.size += uint32(ni)
		t.texts = append(t.texts, segment{
			bytes: bytes.TrimSuffix(buf.Bytes(), syncFlushFooter),
			size:  ni,
			crc:   crc32.ChecksumIEEE(s[:ni]),
		})
		if n < 0 {
			break
		}

		s = s[n+len(startTag):]
		st = st[n+len(startTag):]

		n = strings.Index(st, endTag)
		if n < 0 {
			return nil, fmt.Errorf("gziptemplate: missing end tag=%q in template=%q starting from %q", endTag, template, st)
		}

		t.tags = append(t.tags, st[:n])

		s = s[n+len(endTag):]
		st = st[n+len(endTag):]
	}

	return &t, nil
}

// TagFunc can be used as a substitution value in the map passed to Execute*.
// Execute* functions pass tag (placeholder) name in 'tag' argument.
//
// TagFunc must write contents to w and be safe to call from concurrently
// running goroutines.
type TagFunc func(w io.Writer, tag string) error

var crc32Mat = precomputeCRC32(crc32.IEEE)

// ExecuteFunc calls f on each template tag (placeholder) occurrence.
func (t *Template) ExecuteFunc(w io.Writer, f TagFunc) error {
	n := len(t.texts) - 1
	if n == -1 {
		_, err := w.Write(t.template)
		return err
	}

	if _, err := w.Write(t.gzipHdr[:]); err != nil {
		return err
	}

	zw := &typeZeroWriter{
		w: w,

		size: t.size,
		crc:  t.texts[0].crc,
	}

	for i := 0; i < n; i++ {
		ti := &t.texts[i]

		if _, err := w.Write(ti.bytes); err != nil {
			return err
		}
		if i > 0 {
			zw.crc = combineCRC32(crc32Mat, zw.crc, ti.crc, uint64(ti.size))
		}

		// typeZeroWriter clears buf[0] in Write, we use that as a sentinel.
		zw.buf[0] = ^byte(0)

		if err := f(zw, t.tags[i]); err != nil {
			return err
		}

		if zw.buf[0] != 0 {
			if _, err := w.Write(syncFlushFooter); err != nil {
				return err
			}
		}
	}

	tn := &t.texts[n]

	if _, err := w.Write(tn.bytes); err != nil {
		return err
	}
	digest := combineCRC32(crc32Mat, zw.crc, tn.crc, uint64(tn.size))

	binary.LittleEndian.PutUint32(zw.buf[:4], digest)
	binary.LittleEndian.PutUint32(zw.buf[4:], zw.size)

	_, err := w.Write(zw.buf[:])
	return err
}

// Execute substitutes template tags (placeholders) with the corresponding
// values from the map m and writes the result to the given writer w.
//
// Substitution map m may contain values with the following types:
//   * []byte - the fastest value type
//   * string - convenient value type
//   * TagFunc - flexible value type
func (t *Template) Execute(w io.Writer, m map[string]interface{}) error {
	return t.ExecuteFunc(w, func(w io.Writer, tag string) error {
		return stdTagFunc(w, tag, m)
	})
}

// ExecuteFuncBytes calls f on each template tag (placeholder) occurrence
// and substitutes it with the data written to TagFunc's w.
//
// Returns the resulting byte slice.
func (t *Template) ExecuteFuncBytes(f TagFunc) []byte {
	var buf bytes.Buffer
	buf.Grow(len(t.template))
	if err := t.ExecuteFunc(&buf, f); err != nil {
		panic(fmt.Sprintf("gziptemplate: unexpected error: %s", err))
	}
	return buf.Bytes()
}

// ExecuteBytes substitutes template tags (placeholders) with the corresponding
// values from the map m and returns the result.
//
// Substitution map m may contain values with the following types:
//   * []byte - the fastest value type
//   * string - convenient value type
//   * TagFunc - flexible value type
func (t *Template) ExecuteBytes(m map[string]interface{}) []byte {
	return t.ExecuteFuncBytes(func(w io.Writer, tag string) error {
		return stdTagFunc(w, tag, m)
	})
}

func stdTagFunc(w io.Writer, tag string, m map[string]interface{}) error {
	v := m[tag]
	if v == nil {
		return nil
	}
	switch value := v.(type) {
	case []byte:
		_, err := w.Write(value)
		return err
	case string:
		_, err := w.Write([]byte(value))
		return err
	case TagFunc:
		return value(w, tag)
	default:
		panic(fmt.Sprintf("gziptemplate: tag=%q contains unexpected value type=%#v", tag, v))
	}
}

type typeZeroWriter struct {
	w io.Writer

	size uint32
	crc  uint32

	buf [8]byte
}

func (w *typeZeroWriter) Write(p []byte) (n int, err error) {
	const maxLength = ^uint16(0)
	for len(p) > int(maxLength) {
		ni, err := w.Write(p[:maxLength])
		n += ni
		if err != nil {
			return n, err
		}

		p = p[maxLength:]
	}

	w.size += uint32(len(p))
	w.crc = crc32.Update(w.crc, crc32.IEEETable, p)

	/* The following code is equivalent to:
	 *  hbw := newHuffmanBitWriter(w.w)
	 *
	 *  if hbw.writeStoredHeader(len(p), false); hbw.err != nil {
	 *          return 0, hbw.err
	 *  }
	 *
	 *  hbw.writeBytes(p)
	 *  return len(p), hbw.err
	 */

	w.buf[0] = 0
	binary.LittleEndian.PutUint16(w.buf[1:], uint16(len(p)))
	binary.LittleEndian.PutUint16(w.buf[3:], ^uint16(len(p)))

	if _, err = w.w.Write(w.buf[:5]); err != nil {
		return
	}

	ni, err := w.w.Write(p)
	n += ni
	return n, err
}
