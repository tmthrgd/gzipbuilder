// Package fasttemplate implements simple and fast template library.
//
// Fasttemplate is faster than text/template, strings.Replace
// and strings.Replacer.
//
// Fasttemplate ideally fits for fast and simple placeholders' substitutions.
package fasttemplate

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
)

// These constants are copied from the flate package, so that code that imports
// this package does not also have to import "compress/flate".
const (
	NoCompression      = flate.NoCompression
	BestSpeed          = flate.BestSpeed
	BestCompression    = flate.BestCompression
	DefaultCompression = flate.DefaultCompression
	HuffmanOnly        = flate.HuffmanOnly
)

// Template implements simple template engine, which can be used for fast
// tags' (aka placeholders) substitution.
type Template struct {
	template []byte
	texts    [][]byte
	tags     []string
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
	var t Template
	err := t.Reset(template, startTag, endTag, level)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// TagFunc can be used as a substitution value in the map passed to Execute*.
// Execute* functions pass tag (placeholder) name in 'tag' argument.
//
// TagFunc must be safe to call from concurrently running goroutines.
//
// TagFunc must write contents to w and return the number of bytes written.
type TagFunc func(w io.Writer, tag string) (int, error)

// Reset resets the template t to new one defined by
// template, startTag and endTag.
//
// Reset allows Template object re-use.
//
// Reset may be called only if no other goroutines call t methods at the moment.
func (t *Template) Reset(template, startTag, endTag string, level int) error {
	t.texts = t.texts[:0]
	t.tags = t.tags[:0]

	if len(startTag) == 0 {
		panic("startTag cannot be empty")
	}
	if len(endTag) == 0 {
		panic("endTag cannot be empty")
	}

	tagsCount := strings.Count(template, startTag)
	if tagsCount == 0 {
		var buf bytes.Buffer
		fw, err := flate.NewWriter(&buf, level)
		if err != nil {
			return err
		}

		if _, err := fw.Write([]byte(template)); err != nil {
			return err
		}

		if err := fw.Close(); err != nil {
			return err
		}

		t.template = buf.Bytes()
		return nil
	}

	if tagsCount+1 > cap(t.texts) {
		t.texts = make([][]byte, 0, tagsCount+1)
	}
	if tagsCount > cap(t.tags) {
		t.tags = make([]string, 0, tagsCount)
	}

	fw, err := flate.NewWriter(nil, level)
	if err != nil {
		return err
	}

	st := template

	for {
		var buf bytes.Buffer
		fw.Reset(&buf)

		n := strings.Index(st, startTag)
		ni := n
		if n < 0 {
			ni = len(st)
		}

		if _, err := fw.Write([]byte(st[:ni])); err != nil {
			return err
		}

		var err error
		if n < 0 {
			err = fw.Close()
		} else {
			err = fw.Flush()
		}
		if err != nil {
			return err
		}

		t.texts = append(t.texts, buf.Bytes())
		if n < 0 {
			break
		}

		st = st[n+len(startTag):]

		n = strings.Index(st, endTag)
		if n < 0 {
			return fmt.Errorf("Cannot find end tag=%q in the template=%q starting from %q", endTag, template, st)
		}

		t.tags = append(t.tags, st[:n])

		st = st[n+len(endTag):]
	}

	return nil
}

// ExecuteFunc calls f on each template tag (placeholder) occurrence.
//
// Returns the number of bytes written to w.
func (t *Template) ExecuteFunc(w io.Writer, f TagFunc) (int64, error) {
	var nn int64

	n := len(t.texts) - 1
	if n == -1 {
		ni, err := w.Write(t.template)
		return int64(ni), err
	}

	zw := &typeZeroWriter{w: w}

	for i := 0; i < n; i++ {
		ni, err := w.Write(t.texts[i])
		nn += int64(ni)
		if err != nil {
			return nn, err
		}

		ni, err = f(zw, t.tags[i])
		nn += int64(ni)
		if err != nil {
			return nn, err
		}
	}
	ni, err := w.Write(t.texts[n])
	nn += int64(ni)
	return nn, err
}

// Execute substitutes template tags (placeholders) with the corresponding
// values from the map m and writes the result to the given writer w.
//
// Substitution map m may contain values with the following types:
//   * []byte - the fastest value type
//   * string - convenient value type
//   * TagFunc - flexible value type
//
// Returns the number of bytes written to w.
func (t *Template) Execute(w io.Writer, m map[string]interface{}) (int64, error) {
	return t.ExecuteFunc(w, func(w io.Writer, tag string) (int, error) { return stdTagFunc(w, tag, m) })
}

// ExecuteFuncString calls f on each template tag (placeholder) occurrence
// and substitutes it with the data written to TagFunc's w.
//
// Returns the resulting string.
func (t *Template) ExecuteFuncString(f TagFunc) string {
	var sb strings.Builder
	sb.Grow(len(t.template))
	if _, err := t.ExecuteFunc(&sb, f); err != nil {
		panic(fmt.Sprintf("unexpected error: %s", err))
	}
	return sb.String()
}

// ExecuteString substitutes template tags (placeholders) with the corresponding
// values from the map m and returns the result.
//
// Substitution map m may contain values with the following types:
//   * []byte - the fastest value type
//   * string - convenient value type
//   * TagFunc - flexible value type
func (t *Template) ExecuteString(m map[string]interface{}) string {
	return t.ExecuteFuncString(func(w io.Writer, tag string) (int, error) { return stdTagFunc(w, tag, m) })
}

func stdTagFunc(w io.Writer, tag string, m map[string]interface{}) (int, error) {
	v := m[tag]
	if v == nil {
		return 0, nil
	}
	switch value := v.(type) {
	case []byte:
		return w.Write(value)
	case string:
		return w.Write([]byte(value))
	case TagFunc:
		return value(w, tag)
	default:
		panic(fmt.Sprintf("tag=%q contains unexpected value type=%#v. Expected []byte, string or TagFunc", tag, v))
	}
}

type typeZeroWriter struct {
	w io.Writer

	hdrBuf [5]byte
}

func (w *typeZeroWriter) Write(p []byte) (n int, err error) {
	if len(p) > int(^uint16(0)) {
		return 0, errors.New("input data too long")
	}

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

	w.hdrBuf[0] = 0
	binary.LittleEndian.PutUint16(w.hdrBuf[1:], uint16(len(p)))
	binary.LittleEndian.PutUint16(w.hdrBuf[3:], ^uint16(len(p)))

	if _, err = w.w.Write(w.hdrBuf[:]); err != nil {
		return
	}

	return w.w.Write(p)
}
