package gzipbuilder

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/ioutil"
	"strings"
	"testing"
)

func decompressBytes(t *testing.T, b []byte) []byte {
	t.Helper()

	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("gzip decompression failed: %v", err)
	}

	res, err := ioutil.ReadAll(r)
	if err != nil {
		t.Fatalf("gzip decompression failed: %v", err)
	}

	if err := r.Close(); err != nil {
		t.Fatalf("gzip decompression failed: %v", err)
	}

	return res
}

func TestEmptyTemplate(t *testing.T) {
	tpl := New("", "[", "]", BestCompression)

	s := tpl.ExecuteBytes(map[string]interface{}{"foo": "bar", "aaa": "bbb"})
	s = decompressBytes(t, s)
	if len(s) != 0 {
		t.Fatalf("unexpected string returned %q. Expected empty string", s)
	}
}

func TestEmptyTagStart(t *testing.T) {
	expectPanic(t, func() { NewTemplate("foobar", "", "]", BestCompression) })
}

func TestEmptyTagEnd(t *testing.T) {
	expectPanic(t, func() { NewTemplate("foobar", "[", "", BestCompression) })
}

func TestNoTags(t *testing.T) {
	template := "foobar"
	tpl := New(template, "[", "]", BestCompression)

	s := tpl.ExecuteBytes(map[string]interface{}{"foo": "bar", "aaa": "bbb"})
	s = decompressBytes(t, s)
	if string(s) != template {
		t.Fatalf("unexpected template value %q. Expected %q", s, template)
	}
}

func TestEmptyTagName(t *testing.T) {
	template := "foo[]bar"
	tpl := New(template, "[", "]", BestCompression)

	s := tpl.ExecuteBytes(map[string]interface{}{"": "111", "aaa": "bbb"})
	s = decompressBytes(t, s)
	result := "foo111bar"
	if string(s) != result {
		t.Fatalf("unexpected template value %q. Expected %q", s, result)
	}
}

func TestOnlyTag(t *testing.T) {
	template := "[foo]"
	tpl := New(template, "[", "]", BestCompression)

	s := tpl.ExecuteBytes(map[string]interface{}{"foo": "111", "aaa": "bbb"})
	s = decompressBytes(t, s)
	result := "111"
	if string(s) != result {
		t.Fatalf("unexpected template value %q. Expected %q", s, result)
	}
}

func TestStartWithTag(t *testing.T) {
	template := "[foo]barbaz"
	tpl := New(template, "[", "]", BestCompression)

	s := tpl.ExecuteBytes(map[string]interface{}{"foo": "111", "aaa": "bbb"})
	s = decompressBytes(t, s)
	result := "111barbaz"
	if string(s) != result {
		t.Fatalf("unexpected template value %q. Expected %q", s, result)
	}
}

func TestEndWithTag(t *testing.T) {
	template := "foobar[foo]"
	tpl := New(template, "[", "]", BestCompression)

	s := tpl.ExecuteBytes(map[string]interface{}{"foo": "111", "aaa": "bbb"})
	s = decompressBytes(t, s)
	result := "foobar111"
	if string(s) != result {
		t.Fatalf("unexpected template value %q. Expected %q", s, result)
	}
}

func TestDuplicateTags(t *testing.T) {
	template := "[foo]bar[foo][foo]baz"
	tpl := New(template, "[", "]", BestCompression)

	s := tpl.ExecuteBytes(map[string]interface{}{"foo": "111", "aaa": "bbb"})
	s = decompressBytes(t, s)
	result := "111bar111111baz"
	if string(s) != result {
		t.Fatalf("unexpected template value %q. Expected %q", s, result)
	}
}

func TestMultipleTags(t *testing.T) {
	template := "foo[foo]aa[aaa]ccc"
	tpl := New(template, "[", "]", BestCompression)

	s := tpl.ExecuteBytes(map[string]interface{}{"foo": "111", "aaa": "bbb"})
	s = decompressBytes(t, s)
	result := "foo111aabbbccc"
	if string(s) != result {
		t.Fatalf("unexpected template value %q. Expected %q", s, result)
	}
}

func TestLongDelimiter(t *testing.T) {
	template := "foo{{{foo}}}bar"
	tpl := New(template, "{{{", "}}}", BestCompression)

	s := tpl.ExecuteBytes(map[string]interface{}{"foo": "111", "aaa": "bbb"})
	s = decompressBytes(t, s)
	result := "foo111bar"
	if string(s) != result {
		t.Fatalf("unexpected template value %q. Expected %q", s, result)
	}
}

func TestIdenticalDelimiter(t *testing.T) {
	template := "foo@foo@foo@aaa@"
	tpl := New(template, "@", "@", BestCompression)

	s := tpl.ExecuteBytes(map[string]interface{}{"foo": "111", "aaa": "bbb"})
	s = decompressBytes(t, s)
	result := "foo111foobbb"
	if string(s) != result {
		t.Fatalf("unexpected template value %q. Expected %q", s, result)
	}
}

func TestDlimitersWithDistinctSize(t *testing.T) {
	template := "foo<?phpaaa?>bar<?phpzzz?>"
	tpl := New(template, "<?php", "?>", BestCompression)

	s := tpl.ExecuteBytes(map[string]interface{}{"zzz": "111", "aaa": "bbb"})
	s = decompressBytes(t, s)
	result := "foobbbbar111"
	if string(s) != result {
		t.Fatalf("unexpected template value %q. Expected %q", s, result)
	}
}

func TestEmptyValue(t *testing.T) {
	template := "foobar[foo]"
	tpl := New(template, "[", "]", BestCompression)

	s := tpl.ExecuteBytes(map[string]interface{}{"foo": "", "aaa": "bbb"})
	s = decompressBytes(t, s)
	result := "foobar"
	if string(s) != result {
		t.Fatalf("unexpected template value %q. Expected %q", s, result)
	}
}

func TestNoValue(t *testing.T) {
	template := "foobar[foo]x[aaa]"
	tpl := New(template, "[", "]", BestCompression)

	s := tpl.ExecuteBytes(map[string]interface{}{"aaa": "bbb"})
	s = decompressBytes(t, s)
	result := "foobarxbbb"
	if string(s) != result {
		t.Fatalf("unexpected template value %q. Expected %q", s, result)
	}
}

func TestNoEndDelimiter(t *testing.T) {
	template := "foobar[foo"
	_, err := NewTemplate(template, "[", "]", BestCompression)
	if err == nil {
		t.Fatalf("expected non-nil error. got nil")
	}

	expectPanic(t, func() { New(template, "[", "]", BestCompression) })
}

func TestUnsupportedValue(t *testing.T) {
	template := "foobar[foo]"
	tpl := New(template, "[", "]", BestCompression)

	expectPanic(t, func() {
		tpl.ExecuteBytes(map[string]interface{}{"foo": 123, "aaa": "bbb"})
	})
}

func TestMixedValues(t *testing.T) {
	template := "foo[foo]bar[bar]baz[baz]"
	tpl := New(template, "[", "]", BestCompression)

	s := tpl.ExecuteBytes(map[string]interface{}{
		"foo": "111",
		"bar": []byte("bbb"),
		"baz": TagFunc(func(w io.Writer, tag string) error {
			_, err := io.WriteString(w, tag)
			return err
		}),
	})
	s = decompressBytes(t, s)
	result := "foo111barbbbbazbaz"
	if string(s) != result {
		t.Fatalf("unexpected template value %q. Expected %q", s, result)
	}
}

func TestLongValue(t *testing.T) {
	template := "foobar[foo]"
	tpl := New(template, "[", "]", BestCompression)

	foo := strings.Repeat("a", int(^uint16(0))+16)
	s := tpl.ExecuteBytes(map[string]interface{}{
		"foo": foo,
	})
	s = decompressBytes(t, s)
	result := "foobar" + foo
	if string(s) != result {
		t.Fatal("unexpected template value")
	}
}

func expectPanic(t *testing.T, f func()) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("missing panic")
		}
	}()
	f()
}
