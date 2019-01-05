package gzipbuilder

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"io"
	"io/ioutil"
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const debug = false

func debugLogf(t *testing.T, fmt string, args ...interface{}) {
	t.Helper()

	if debug {
		t.Logf(fmt, args...)
	}
}

func decompressBytes(t *testing.T, b []byte) string {
	t.Helper()

	var err error
	defer func() {
		if err == nil || !strings.HasPrefix(err.Error(), "flate: corrupt input before offset ") {
			return
		}

		i, _ := strconv.Atoi(strings.TrimPrefix(err.Error(), "flate: corrupt input before offset "))
		last := i + 1
		if last > len(b) {
			last = len(b)
		}

		switch {
		case len(b) > 1024 && last < 1024:
			t.Logf("%x %d:%02x %x...", b[:i], i, b[i], b[last:1024])
		case len(b) > 1024:
			t.Logf("%x...", b[:1024])
		default:
			t.Logf("%x %d:%02x %x", b[:i], i, b[i], b[last:])
		}
	}()

	r, err := gzip.NewReader(bytes.NewReader(b))
	require.NoError(t, err, "gzip decompression failed")

	res, err := ioutil.ReadAll(r)
	require.NoError(t, err, "gzip decompression failed")

	err = r.Close()
	require.NoError(t, err, "gzip decompression failed")

	return string(res)
}

func TestBuilder(t *testing.T) {
	for level := HuffmanOnly; level <= BestCompression; level++ {
		t.Run("level:"+strconv.Itoa(level), func(t *testing.T) {
			seg, err := PrecompressData([]byte("hello world "), level)
			require.NoError(t, err, "failed to compress segment")

			b := NewBuilder(level)

			b.AddPrecompressedData(seg)
			b.AddUncompressedData([]byte("super secret"))
			b.AddCompressedData([]byte(" messages need to be sent. "))
			b.AddPrecompressedData(seg)
			io.WriteString(b.CompressedWriter(), "this is another ")
			io.WriteString(b.UncompressedWriter(), "test.")

			bb, err := b.Bytes()
			require.NoError(t, err, "Bytes returned error")
			assert.NoError(t, b.Err(), "Err returned error")

			debugLogf(t, "%d:%x", len(bb), bb)

			assert.Equal(t, "hello world super secret messages need to be sent. hello world this is another test.",
				decompressBytes(t, bb))
		})
	}
}

func TestBuilderLast(t *testing.T) {
	seg, err := PrecompressData([]byte("hello world"), DefaultCompression)
	require.NoError(t, err, "failed to compress segment")

	for _, tc := range []struct {
		name string
		fn   func(*Builder)
	}{
		{"AddPrecompressedData", func(b *Builder) { b.AddPrecompressedData(seg) }},
		{"AddUncompressedData", func(b *Builder) { b.AddUncompressedData([]byte("hello world")) }},
		{"AddCompressedData", func(b *Builder) { b.AddCompressedData([]byte("hello world")) }},
		{"UncompressedWriter", func(b *Builder) { io.WriteString(b.UncompressedWriter(), "hello world") }},
		{"CompressedWriter", func(b *Builder) { io.WriteString(b.CompressedWriter(), "hello world") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBuilder(DefaultCompression)
			tc.fn(b)

			bb, err := b.Bytes()
			require.NoError(t, err, "Bytes returned error")
			assert.NoError(t, b.Err(), "Err returned error")

			debugLogf(t, "%d:%x", len(bb), bb)

			assert.Equal(t, "hello world", decompressBytes(t, bb))
		})
	}
}

func TestBuilderDouble(t *testing.T) {
	seg1, err := PrecompressData([]byte("hello "), DefaultCompression)
	require.NoError(t, err, "failed to compress segment")

	seg2, err := PrecompressData([]byte("world"), DefaultCompression)
	require.NoError(t, err, "failed to compress segment")

	for _, tc := range []struct {
		name string
		fn   func(*Builder)
	}{
		{"AddPrecompressedData", func(b *Builder) {
			b.AddPrecompressedData(seg1)
			b.AddPrecompressedData(seg2)
		}},
		{"AddUncompressedData", func(b *Builder) {
			b.AddUncompressedData([]byte("hello "))
			b.AddUncompressedData([]byte("world"))
		}},
		{"AddCompressedData", func(b *Builder) {
			b.AddCompressedData([]byte("hello "))
			b.AddCompressedData([]byte("world"))
		}},
		{"UncompressedWriter", func(b *Builder) {
			w := b.UncompressedWriter()
			io.WriteString(w, "hello ")
			io.WriteString(w, "world")
		}},
		{"CompressedWriter", func(b *Builder) {
			w := b.CompressedWriter()
			io.WriteString(w, "hello ")
			io.WriteString(w, "world")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBuilder(DefaultCompression)
			tc.fn(b)

			bb, err := b.Bytes()
			require.NoError(t, err, "Bytes returned error")
			assert.NoError(t, b.Err(), "Err returned error")

			debugLogf(t, "%d:%x", len(bb), bb)

			assert.Equal(t, "hello world", decompressBytes(t, bb))
		})
	}
}

func TestBuilderCombinations(t *testing.T) {
	seg, err := PrecompressData([]byte("hello world "), DefaultCompression)
	require.NoError(t, err, "failed to compress segment")

	tcs := []struct {
		name string
		fn   func(*Builder)
	}{
		{"AddPrecompressedData", func(b *Builder) { b.AddPrecompressedData(seg) }},
		{"AddUncompressedData", func(b *Builder) { b.AddUncompressedData([]byte("hello world ")) }},
		{"AddCompressedData", func(b *Builder) { b.AddCompressedData([]byte("hello world ")) }},
		{"UncompressedWriter", func(b *Builder) { io.WriteString(b.UncompressedWriter(), "hello world ") }},
		{"CompressedWriter", func(b *Builder) { io.WriteString(b.CompressedWriter(), "hello world ") }},
	}
	for _, tc1 := range tcs {
		for _, tc2 := range tcs {
			t.Run(tc1.name+"+"+tc2.name, func(t *testing.T) {
				b := NewBuilder(DefaultCompression)
				tc1.fn(b)
				tc2.fn(b)

				bb, err := b.Bytes()
				require.NoError(t, err, "Bytes returned error")
				assert.NoError(t, b.Err(), "Err returned error")

				debugLogf(t, "%d:%x", len(bb), bb)

				assert.Equal(t, "hello world hello world ", decompressBytes(t, bb))
			})
		}
	}
}

func TestBuilderWriterEqual(t *testing.T) {
	for _, tc := range []struct {
		name string
		fn   func(b1, b2 *Builder)
	}{
		{"UncompressedWriter", func(b1, b2 *Builder) {
			io.WriteString(b1.UncompressedWriter(), "hello world")
			b2.AddUncompressedData([]byte("hello world"))
		}},
		{"CompressedWriter", func(b1, b2 *Builder) {
			io.WriteString(b1.CompressedWriter(), "hello world")
			b2.AddCompressedData([]byte("hello world"))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b1 := NewBuilder(DefaultCompression)
			b2 := NewBuilder(DefaultCompression)

			tc.fn(b1, b2)

			bb1, err := b1.Bytes()
			require.NoError(t, err, "Bytes returned error")
			require.NoError(t, b1.Err(), "Err returned error")

			bb2, err := b2.Bytes()
			require.NoError(t, err, "Bytes returned error")
			require.NoError(t, b2.Err(), "Err returned error")

			debugLogf(t, "%d:%x", len(bb1), bb1)

			assert.Equal(t, bb2, bb1)
		})
	}
}

func testBuilderError(t *testing.T, msg string, fn func(*Builder)) {
	t.Helper()

	b := NewBuilder(DefaultCompression)
	fn(b)

	assert.EqualError(t, b.Err(), msg)

	bb, err := b.Bytes()
	if assert.EqualError(t, err, msg) {
		assert.Nil(t, bb, "expected nil []byte from Bytes")
	}

	assert.Panics(t, func() { b.BytesOrPanic() })
}

func TestBuilderLevelMismatch(t *testing.T) {
	seg, err := PrecompressData(nil, BestCompression)
	require.NoError(t, err, "failed to compress segment")

	testBuilderError(t, "gzipbuilder: compression level mismatch", func(b *Builder) { b.AddPrecompressedData(seg) })
}

func TestBuilderFinished(t *testing.T) {
	seg, err := PrecompressData([]byte("hello world"), DefaultCompression)
	require.NoError(t, err, "failed to compress segment")

	for _, tc := range []struct {
		name string
		fn   func(*Builder)
	}{
		{"AddPrecompressedData", func(b *Builder) { b.AddPrecompressedData(seg) }},
		{"AddUncompressedData", func(b *Builder) { b.AddUncompressedData([]byte("hello world")) }},
		{"AddCompressedData", func(b *Builder) { b.AddCompressedData([]byte("hello world")) }},
		{"UncompressedWriter", func(b *Builder) { io.WriteString(b.UncompressedWriter(), "hello world") }},
		{"CompressedWriter", func(b *Builder) { io.WriteString(b.CompressedWriter(), "hello world") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testBuilderError(t, "gzipbuilder: cannot modify Builder after Bytes called", func(b *Builder) {
				b.BytesOrPanic()
				tc.fn(b)
			})
		})
	}
}

func TestBuilderMultipleBytes(t *testing.T) {
	b := NewBuilder(DefaultCompression)
	b.AddUncompressedData([]byte("hello world"))

	b1, err := b.Bytes()
	require.NoError(t, err, "Bytes returned error")
	assert.NoError(t, b.Err(), "Err returned error")

	b2, err := b.Bytes()
	require.NoError(t, err, "Bytes returned error")
	assert.NoError(t, b.Err(), "Err returned error")

	debugLogf(t, "%d:%x", len(b1), b1)

	assert.Equal(t, b1, b2)
}

func TestBuilderMultipleBytesOrPanic(t *testing.T) {
	b := NewBuilder(DefaultCompression)
	b.AddUncompressedData([]byte("hello world"))

	assert.Equal(t, b.BytesOrPanic(), b.BytesOrPanic())
}

func TestBuilderLongData(t *testing.T) {
	data := bytes.Repeat([]byte{'a'}, 1<<17) // 128 KiB

	seg, err := PrecompressData(data, DefaultCompression)
	require.NoError(t, err, "failed to compress segment")

	for _, tc := range []struct {
		name string
		fn   func(*Builder)
	}{
		{"AddPrecompressedData", func(b *Builder) { b.AddPrecompressedData(seg) }},
		{"AddUncompressedData", func(b *Builder) { b.AddUncompressedData(data) }},
		{"AddCompressedData", func(b *Builder) { b.AddCompressedData(data) }},
		{"UncompressedWriter", func(b *Builder) { b.UncompressedWriter().Write(data) }},
		{"CompressedWriter", func(b *Builder) { b.CompressedWriter().Write(data) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBuilder(DefaultCompression)
			tc.fn(b)

			bb, err := b.Bytes()
			require.NoError(t, err, "Bytes returned error")
			assert.NoError(t, b.Err(), "Err returned error")

			assert.True(t, string(data) == decompressBytes(t, bb),
				"decompressed data is wrong")
		})
	}
}

func TestBuilderEmptyData(t *testing.T) {
	seg, err := PrecompressData(nil, DefaultCompression)
	require.NoError(t, err, "failed to compress segment")

	noData := NewBuilder(DefaultCompression).BytesOrPanic()

	for _, tc := range []struct {
		name string
		fn   func(*Builder)
	}{
		{"AddPrecompressedData", func(b *Builder) { b.AddPrecompressedData(seg) }},
		{"AddUncompressedData", func(b *Builder) { b.AddUncompressedData(nil) }},
		{"AddCompressedData", func(b *Builder) { b.AddCompressedData(nil) }},
		{"UncompressedWriter", func(b *Builder) { io.WriteString(b.UncompressedWriter(), "") }},
		{"CompressedWriter", func(b *Builder) { io.WriteString(b.CompressedWriter(), "") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBuilder(DefaultCompression)
			tc.fn(b)

			assert.Equal(t, header, b.last, "last type should be header")

			bb, err := b.Bytes()
			require.NoError(t, err, "Bytes returned error")
			assert.NoError(t, b.Err(), "Err returned error")

			debugLogf(t, "%d:%x", len(bb), bb)

			assert.Equal(t, noData, bb)
			assert.Equal(t, "", decompressBytes(t, bb))
		})
	}
}

func TestBuilderInterleavedCompressedData(t *testing.T) {
	// This test ensures that AddCompressedData properly calls
	// b.fw.Reset() and doesn't emit a match to the wrong place.

	b := NewBuilder(BestCompression)

	b.AddCompressedData([]byte("hello world"))
	b.AddUncompressedData([]byte{0xa5})
	b.AddCompressedData([]byte("hello world"))

	bb, err := b.Bytes()
	require.NoError(t, err, "Bytes returned error")
	assert.NoError(t, b.Err(), "Err returned error")

	debugLogf(t, "%d:%x", len(bb), bb)

	assert.Equal(t, "hello world\xa5hello world", decompressBytes(t, bb))
}

func TestPrecompressDataInvalidLevel(t *testing.T) {
	seg, err := PrecompressData(nil, -100)
	require.EqualError(t, err, "flate: invalid compression level -100: want value in range [-2, 9]")
	assert.Nil(t, seg, "expected nil *PrecompressedData")
}

func TestBuilderInvalidLevel(t *testing.T) {
	b := NewBuilder(-100)

	b.AddUncompressedData(nil)
	assert.Equal(t, start, b.last, "last type should still be start")

	b.RawDeflate()
	assert.False(t, b.rawDeflate, "RawDeflate should be noop")

	bb, err := b.Bytes()
	require.EqualError(t, err, "flate: invalid compression level -100: want value in range [-2, 9]")
	assert.Nil(t, bb, "expected nil []byte from Bytes")
}

func TestPrecompressedWriter(t *testing.T) {
	w := NewPrecompressedWriter(DefaultCompression)

	io.WriteString(w, "abc")
	io.WriteString(w, "def")

	d, err := w.Data()
	assert.NoError(t, err)

	b := NewBuilder(DefaultCompression)
	b.AddPrecompressedData(d)

	bb, err := b.Bytes()
	assert.NoError(t, err)

	debugLogf(t, "%d:%x", len(bb), bb)

	assert.Equal(t, "abcdef", decompressBytes(t, bb))
}

func TestBuilderWriterTemplate(t *testing.T) {
	w := NewPrecompressedWriter(DefaultCompression)

	tmpl, err := template.New("").Parse("Hello {{.}}!")
	require.NoError(t, err, "parsing template")

	err = tmpl.Execute(w, "world")
	assert.NoError(t, err, "executing template")

	_, err = w.Write(nil)
	assert.NoError(t, err, "nil Write")

	d, err := w.Data()
	require.NoError(t, err, "PrecompressedWriter.Data")

	b := NewBuilder(DefaultCompression)
	b.AddPrecompressedData(d)

	cw := b.CompressedWriter()
	io.WriteString(cw, " ")

	err = tmpl.Execute(cw, "Alice")
	assert.NoError(t, err, "executing template")

	uw := b.UncompressedWriter()
	io.WriteString(uw, " ")

	err = tmpl.Execute(uw, "Bob")
	assert.NoError(t, err, "executing template")

	bb, err := b.Bytes()
	assert.NoError(t, err, "Builder.Bytes")

	debugLogf(t, "%d:%x", len(bb), bb)

	assert.Equal(t, "Hello world! Hello Alice! Hello Bob!", decompressBytes(t, bb))
}

func TestBuilderCompressedUncompressed(t *testing.T) {
	data := make([]byte, 128)
	for i := range data {
		data[i] = byte(i)
	}

	for i := 1; i < len(data); i++ {
		b := NewBuilder(DefaultCompression)

		b.AddCompressedData(data[:i])
		b.AddUncompressedData([]byte{0xfe})

		bb, err := b.Bytes()
		if !assert.NoError(t, err) || !assert.Equal(t, string(data[:i])+"\xfe",
			decompressBytes(t, bb), "for len=%d", i) {
			debugLogf(t, "%d:%x (for len=%d)", len(bb), bb, i)
			return
		}
	}
}

func TestBuilderUncompressedPacking(t *testing.T) {
	if !packUncompressedData {
		t.Skip("packing of uncompressed data is disabled")
	}

	data := bytes.Repeat([]byte{'a'}, 1<<17) // 128 KiB

	b1 := NewBuilder(DefaultCompression)
	b1.AddUncompressedData(data)

	bb1, err := b1.Bytes()
	require.NoError(t, err, "Bytes returned error")
	require.NoError(t, b1.Err(), "Err returned error")

	for _, tc := range []struct {
		name string
		fn   func(*Builder)
	}{
		{"rand", func(b *Builder) {
			r := rand.New(rand.NewSource(0))

			data := data
			for len(data) > 0 {
				l := 1 + r.Intn(len(data))
				b.AddUncompressedData(data[:l])
				data = data[l:]
			}
		}},
		{"single", func(b *Builder) {
			for i := range data {
				b.AddUncompressedData(data[i : i+1])
			}
		}},
		{"small+rest", func(b *Builder) {
			b.AddUncompressedData(data[:2])
			b.AddUncompressedData(data[2:])
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b2 := NewBuilder(DefaultCompression)
			tc.fn(b2)

			bb2, err := b2.Bytes()
			require.NoError(t, err, "Bytes returned error")
			require.NoError(t, b2.Err(), "Err returned error")

			assert.True(t, bytes.Equal(bb1, bb2), "compressed data differs; len=%d vs len=%d, diff=%d",
				len(bb1), len(bb2), len(bb2)-len(bb1))
		})
	}
}

func TestRawDeflate(t *testing.T) {
	seg, err := PrecompressData([]byte("hello world"), DefaultCompression)
	require.NoError(t, err, "failed to compress segment")

	for _, tc := range []struct {
		name string
		fn   func(*Builder)
	}{
		{"empty", func(*Builder) {}},
		{"AddPrecompressedData", func(b *Builder) { b.AddPrecompressedData(seg) }},
		{"AddUncompressedData", func(b *Builder) { b.AddUncompressedData([]byte("hello world")) }},
		{"AddCompressedData", func(b *Builder) { b.AddCompressedData([]byte("hello world")) }},
		{"UncompressedWriter", func(b *Builder) { io.WriteString(b.UncompressedWriter(), "hello world") }},
		{"CompressedWriter", func(b *Builder) { io.WriteString(b.CompressedWriter(), "hello world") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBuilder(DefaultCompression)
			b.RawDeflate()
			tc.fn(b)

			assert.Zero(t, b.size, "size field should not have been updated")
			assert.Zero(t, b.crc, "crc field should not have been updated")

			bb, err := b.Bytes()
			require.NoError(t, err, "Bytes returned error")
			assert.NoError(t, b.Err(), "Err returned error")

			debugLogf(t, "%d:%x", len(bb), bb)

			r := flate.NewReader(bytes.NewReader(bb))

			res, err := ioutil.ReadAll(r)
			require.NoError(t, err, "flate decompression failed")

			err = r.Close()
			require.NoError(t, err, "flate decompression failed")

			if tc.name == "empty" {
				assert.Equal(t, "", string(res))
			} else {
				assert.Equal(t, "hello world", string(res))
			}
		})
	}
}

func TestRawDeflateErrorAfterWrite(t *testing.T) {
	seg, err := PrecompressData([]byte("hello world"), DefaultCompression)
	require.NoError(t, err, "failed to compress segment")

	for _, tc := range []struct {
		name string
		fn   func(*Builder)
	}{
		{"AddPrecompressedData", func(b *Builder) { b.AddPrecompressedData(seg) }},
		{"AddUncompressedData", func(b *Builder) { b.AddUncompressedData([]byte("hello world")) }},
		{"AddCompressedData", func(b *Builder) { b.AddCompressedData([]byte("hello world")) }},
		{"UncompressedWriter", func(b *Builder) { io.WriteString(b.UncompressedWriter(), "hello world") }},
		{"CompressedWriter", func(b *Builder) { io.WriteString(b.CompressedWriter(), "hello world") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testBuilderError(t, "gzipbuilder: setting options must be done before writing", func(b *Builder) {
				tc.fn(b)
				b.RawDeflate()
			})
		})
	}
}

func TestPrecompressedWriterReset(t *testing.T) {
	w := NewPrecompressedWriter(DefaultCompression)

	io.WriteString(w, "hello world 1")

	data1, err := w.Data()
	require.NoError(t, err, "PrecompressedWriter.Data failed")

	buf1 := w.buf
	w.Reset()
	assert.False(t, buf1 == w.buf, "buffer alias after Reset")

	io.WriteString(w, "hello world 2")

	data2, err := w.Data()
	require.NoError(t, err, "PrecompressedWriter.Data failed")

	b := NewBuilder(DefaultCompression)
	b.AddPrecompressedData(data1)
	b.AddPrecompressedData(data2)

	bb, err := b.Bytes()
	require.NoError(t, err, "Bytes returned error")
	assert.NoError(t, b.Err(), "Err returned error")

	debugLogf(t, "%d:%x", len(bb), bb)

	assert.Equal(t, "hello world 1hello world 2", decompressBytes(t, bb))
}

func TestPrecompressedWriterResetInvalidLevel(t *testing.T) {
	w := NewPrecompressedWriter(-100)
	w.Reset()

	data, err := w.Data()
	require.EqualError(t, err, "flate: invalid compression level -100: want value in range [-2, 9]")
	assert.Nil(t, data, "expected nil *PrecompressedData")
}
