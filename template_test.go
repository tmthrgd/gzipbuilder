package fasttemplate

import (
	"compress/flate"
	"io"
	"io/ioutil"
	"strings"
	"testing"
)

func decompressString(t *testing.T, s string) string {
	t.Helper()

	r := flate.NewReader(strings.NewReader(s))

	res, err := ioutil.ReadAll(r)
	if err != nil {
		t.Fatalf("flate decompression failed: %v", err)
	}

	if err := r.Close(); err != nil {
		t.Fatalf("flate decompression failed: %v", err)
	}

	return string(res)
}

func TestEmptyTemplate(t *testing.T) {
	tpl := New("", "[", "]", BestCompression)

	s := tpl.ExecuteString(map[string]interface{}{"foo": "bar", "aaa": "bbb"})
	s = decompressString(t, s)
	if s != "" {
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

	s := tpl.ExecuteString(map[string]interface{}{"foo": "bar", "aaa": "bbb"})
	s = decompressString(t, s)
	if s != template {
		t.Fatalf("unexpected template value %q. Expected %q", s, template)
	}
}

func TestEmptyTagName(t *testing.T) {
	template := "foo[]bar"
	tpl := New(template, "[", "]", BestCompression)

	s := tpl.ExecuteString(map[string]interface{}{"": "111", "aaa": "bbb"})
	s = decompressString(t, s)
	result := "foo111bar"
	if s != result {
		t.Fatalf("unexpected template value %q. Expected %q", s, result)
	}
}

func TestOnlyTag(t *testing.T) {
	template := "[foo]"
	tpl := New(template, "[", "]", BestCompression)

	s := tpl.ExecuteString(map[string]interface{}{"foo": "111", "aaa": "bbb"})
	s = decompressString(t, s)
	result := "111"
	if s != result {
		t.Fatalf("unexpected template value %q. Expected %q", s, result)
	}
}

func TestStartWithTag(t *testing.T) {
	template := "[foo]barbaz"
	tpl := New(template, "[", "]", BestCompression)

	s := tpl.ExecuteString(map[string]interface{}{"foo": "111", "aaa": "bbb"})
	s = decompressString(t, s)
	result := "111barbaz"
	if s != result {
		t.Fatalf("unexpected template value %q. Expected %q", s, result)
	}
}

func TestEndWithTag(t *testing.T) {
	template := "foobar[foo]"
	tpl := New(template, "[", "]", BestCompression)

	s := tpl.ExecuteString(map[string]interface{}{"foo": "111", "aaa": "bbb"})
	s = decompressString(t, s)
	result := "foobar111"
	if s != result {
		t.Fatalf("unexpected template value %q. Expected %q", s, result)
	}
}

func TestTemplateReset(t *testing.T) {
	template := "foo{bar}baz"
	tpl := New(template, "{", "}", BestCompression)
	s := tpl.ExecuteString(map[string]interface{}{"bar": "111"})
	s = decompressString(t, s)
	result := "foo111baz"
	if s != result {
		t.Fatalf("unexpected template value %q. Expected %q", s, result)
	}

	template = "[xxxyyyzz"
	if err := tpl.Reset(template, "[", "]", BestCompression); err == nil {
		t.Fatalf("expecting error for unclosed tag on %q", template)
	}

	template = "[xxx]yyy[zz]"
	if err := tpl.Reset(template, "[", "]", BestCompression); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	s = tpl.ExecuteString(map[string]interface{}{"xxx": "11", "zz": "2222"})
	s = decompressString(t, s)
	result = "11yyy2222"
	if s != result {
		t.Fatalf("unexpected template value %q. Expected %q", s, result)
	}
}

func TestDuplicateTags(t *testing.T) {
	template := "[foo]bar[foo][foo]baz"
	tpl := New(template, "[", "]", BestCompression)

	s := tpl.ExecuteString(map[string]interface{}{"foo": "111", "aaa": "bbb"})
	s = decompressString(t, s)
	result := "111bar111111baz"
	if s != result {
		t.Fatalf("unexpected template value %q. Expected %q", s, result)
	}
}

func TestMultipleTags(t *testing.T) {
	template := "foo[foo]aa[aaa]ccc"
	tpl := New(template, "[", "]", BestCompression)

	s := tpl.ExecuteString(map[string]interface{}{"foo": "111", "aaa": "bbb"})
	s = decompressString(t, s)
	result := "foo111aabbbccc"
	if s != result {
		t.Fatalf("unexpected template value %q. Expected %q", s, result)
	}
}

func TestLongDelimiter(t *testing.T) {
	template := "foo{{{foo}}}bar"
	tpl := New(template, "{{{", "}}}", BestCompression)

	s := tpl.ExecuteString(map[string]interface{}{"foo": "111", "aaa": "bbb"})
	s = decompressString(t, s)
	result := "foo111bar"
	if s != result {
		t.Fatalf("unexpected template value %q. Expected %q", s, result)
	}
}

func TestIdenticalDelimiter(t *testing.T) {
	template := "foo@foo@foo@aaa@"
	tpl := New(template, "@", "@", BestCompression)

	s := tpl.ExecuteString(map[string]interface{}{"foo": "111", "aaa": "bbb"})
	s = decompressString(t, s)
	result := "foo111foobbb"
	if s != result {
		t.Fatalf("unexpected template value %q. Expected %q", s, result)
	}
}

func TestDlimitersWithDistinctSize(t *testing.T) {
	template := "foo<?phpaaa?>bar<?phpzzz?>"
	tpl := New(template, "<?php", "?>", BestCompression)

	s := tpl.ExecuteString(map[string]interface{}{"zzz": "111", "aaa": "bbb"})
	s = decompressString(t, s)
	result := "foobbbbar111"
	if s != result {
		t.Fatalf("unexpected template value %q. Expected %q", s, result)
	}
}

func TestEmptyValue(t *testing.T) {
	template := "foobar[foo]"
	tpl := New(template, "[", "]", BestCompression)

	s := tpl.ExecuteString(map[string]interface{}{"foo": "", "aaa": "bbb"})
	s = decompressString(t, s)
	result := "foobar"
	if s != result {
		t.Fatalf("unexpected template value %q. Expected %q", s, result)
	}
}

func TestNoValue(t *testing.T) {
	template := "foobar[foo]x[aaa]"
	tpl := New(template, "[", "]", BestCompression)

	s := tpl.ExecuteString(map[string]interface{}{"aaa": "bbb"})
	s = decompressString(t, s)
	result := "foobarxbbb"
	if s != result {
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
		tpl.ExecuteString(map[string]interface{}{"foo": 123, "aaa": "bbb"})
	})
}

func TestMixedValues(t *testing.T) {
	template := "foo[foo]bar[bar]baz[baz]"
	tpl := New(template, "[", "]", BestCompression)

	s := tpl.ExecuteString(map[string]interface{}{
		"foo": "111",
		"bar": []byte("bbb"),
		"baz": TagFunc(func(w io.Writer, tag string) (int, error) { return w.Write([]byte(tag)) }),
	})
	s = decompressString(t, s)
	result := "foo111barbbbbazbaz"
	if s != result {
		t.Fatalf("unexpected template value %q. Expected %q", s, result)
	}
}

func TestLongValue(t *testing.T) {
	template := "foobar[foo]"
	tpl := New(template, "[", "]", BestCompression)

	foo := strings.Repeat("a", int(^uint16(0))+16)
	s := tpl.ExecuteString(map[string]interface{}{
		"foo": foo,
	})
	s = decompressString(t, s)
	result := "foobar" + foo
	if s != result {
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
