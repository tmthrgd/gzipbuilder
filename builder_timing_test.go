package gzipbuilder

import (
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"strings"
	"testing"
	"text/template"

	"github.com/tmthrgd/fasttemplate"
)

var (
	source = "https://{{uid}}.foo.bar.com/?cb={{cb}}{{width}}&width={{width}}&height={{height}}&timeout={{timeout}}&uid={{uid}}&subid={{subid}}&ref={{ref}}"

	m = map[string]interface{}{
		"cb":      []byte("1234"),
		"width":   []byte("1232"),
		"height":  []byte("123"),
		"timeout": []byte("123123"),
		"uid":     []byte("aaasdf"),
		"subid":   []byte("asdfds"),
		"ref":     []byte("https://google.com/aaa/bbb/ccc"),
	}
)

func map2slice(m map[string]interface{}) []string {
	var a []string
	for k, v := range m {
		a = append(a, "{{"+k+"}}", string(v.([]byte)))
	}
	return a
}

func BenchmarkFmtFprintf(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		gw, _ := gzip.NewWriterLevel(ioutil.Discard, gzip.BestCompression)
		for pb.Next() {
			gw.Reset(ioutil.Discard)
			fmt.Fprintf(gw,
				"https://%[5]s.foo.bar.com/?cb=%[1]s%[2]s&width=%[2]s&height=%[3]s&timeout=%[4]s&uid=%[5]s&subid=%[6]s&ref=%[7]s",
				m["cb"], m["width"], m["height"], m["timeout"], m["uid"], m["subid"], m["ref"])
			gw.Close()
		}
	})
}

func BenchmarkTextTemplate(b *testing.B) {
	s := strings.Replace(source, "{{", "{{.", -1)
	t, err := template.New("test").Parse(s)
	if err != nil {
		b.Fatalf("Error when parsing template: %s", err)
	}

	mm := make(map[string]string)
	for k, v := range m {
		mm[k] = string(v.([]byte))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		gw, _ := gzip.NewWriterLevel(ioutil.Discard, gzip.BestCompression)
		for pb.Next() {
			gw.Reset(ioutil.Discard)
			if err := t.Execute(gw, mm); err != nil {
				b.Fatalf("error when executing template: %s", err)
			}
			gw.Close()
		}
	})
}

func BenchmarkFastTemplateExecuteFunc(b *testing.B) {
	t, err := fasttemplate.NewTemplate(source, "{{", "}}")
	if err != nil {
		b.Fatalf("error in template: %s", err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		gw, _ := gzip.NewWriterLevel(ioutil.Discard, gzip.BestCompression)
		for pb.Next() {
			gw.Reset(ioutil.Discard)
			if err := t.ExecuteFunc(gw, testTagFunc); err != nil {
				b.Fatalf("unexpected error: %s", err)
			}
			gw.Close()
		}
	})
}

func BenchmarkFastTemplateExecute(b *testing.B) {
	t, err := fasttemplate.NewTemplate(source, "{{", "}}")
	if err != nil {
		b.Fatalf("error in template: %s", err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		gw, _ := gzip.NewWriterLevel(ioutil.Discard, gzip.BestCompression)
		for pb.Next() {
			gw.Reset(ioutil.Discard)
			if err := t.Execute(gw, m); err != nil {
				b.Fatalf("unexpected error: %s", err)
			}
			gw.Close()
		}
	})
}

func BenchmarkFastTemplateExecuteTagFunc(b *testing.B) {
	t, err := fasttemplate.NewTemplate(source, "{{", "}}")
	if err != nil {
		b.Fatalf("error in template: %s", err)
	}

	mm := make(map[string]interface{})
	for k, v := range m {
		if k == "ref" {
			vv := v.([]byte)
			v = fasttemplate.TagFunc(func(w io.Writer, tag string) error {
				_, err := io.WriteString(w, url.QueryEscape(string(vv)))
				return err
			})
		}
		mm[k] = v
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		gw, _ := gzip.NewWriterLevel(ioutil.Discard, gzip.BestCompression)
		for pb.Next() {
			gw.Reset(ioutil.Discard)
			if err := t.Execute(gw, mm); err != nil {
				b.Fatalf("unexpected error: %s", err)
			}
			gw.Close()
		}
	})
}

func BenchmarkGzipTemplateExecuteFunc(b *testing.B) {
	t, err := NewTemplate(source, "{{", "}}", BestCompression)
	if err != nil {
		b.Fatalf("error in template: %s", err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := t.ExecuteFunc(ioutil.Discard, testTagFunc); err != nil {
				b.Fatalf("unexpected error: %s", err)
			}
		}
	})
}

func BenchmarkGzipTemplateExecute(b *testing.B) {
	t, err := NewTemplate(source, "{{", "}}", BestCompression)
	if err != nil {
		b.Fatalf("error in template: %s", err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := t.Execute(ioutil.Discard, m); err != nil {
				b.Fatalf("unexpected error: %s", err)
			}
		}
	})
}

func BenchmarkGzipTemplateExecuteFuncBytes(b *testing.B) {
	t, err := NewTemplate(source, "{{", "}}", BestCompression)
	if err != nil {
		b.Fatalf("error in template: %s", err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			t.ExecuteFuncBytes(testTagFunc)
		}
	})
}

func BenchmarkGzipTemplateExecuteBytes(b *testing.B) {
	t, err := NewTemplate(source, "{{", "}}", BestCompression)
	if err != nil {
		b.Fatalf("error in template: %s", err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			t.ExecuteBytes(m)
		}
	})
}

func BenchmarkGzipTemplateExecuteTagFunc(b *testing.B) {
	t, err := NewTemplate(source, "{{", "}}", BestCompression)
	if err != nil {
		b.Fatalf("error in template: %s", err)
	}

	mm := make(map[string]interface{})
	for k, v := range m {
		if k == "ref" {
			vv := v.([]byte)
			v = TagFunc(func(w io.Writer, tag string) error {
				_, err := io.WriteString(w, url.QueryEscape(string(vv)))
				return err
			})
		}
		mm[k] = v
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := t.Execute(ioutil.Discard, mm); err != nil {
				b.Fatalf("unexpected error: %s", err)
			}
		}
	})
}

func BenchmarkNewTemplate(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = New(source, "{{", "}}", BestCompression)
		}
	})
}

func testTagFunc(w io.Writer, tag string) error {
	_, err := w.Write(m[tag].([]byte))
	return err
}
