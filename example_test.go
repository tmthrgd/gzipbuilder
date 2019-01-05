package gzipbuilder

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/url"
)

func mustDecompress(b []byte) []byte {
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		panic(fmt.Sprintf("gzip decompression failed: %v", err))
	}

	res, err := ioutil.ReadAll(r)
	if err != nil {
		panic(fmt.Sprintf("gzip decompression failed: %v", err))
	}

	if err := r.Close(); err != nil {
		panic(fmt.Sprintf("gzip decompression failed: %v", err))
	}

	return res
}

func ExampleTemplate() {
	template := "https://{{host}}/?foo={{bar}}{{bar}}&q={{query}}&baz={{baz}}"
	t := New(template, "{{", "}}", BestCompression)

	// Substitution map.
	// Since "baz" tag is missing in the map, it will be substituted
	// by an empty string.
	m := map[string]interface{}{
		"host": "google.com",     // string - convenient
		"bar":  []byte("foobar"), // byte slice - the fastest

		// TagFunc - flexible value. TagFunc is called only if the given
		// tag exists in the template.
		"query": TagFunc(func(w io.Writer, tag string) error {
			_, err := io.WriteString(w, url.QueryEscape(tag+"=world"))
			return err
		}),
	}

	s := t.ExecuteBytes(m)
	s = mustDecompress(s)
	fmt.Printf("%s", s)

	// Output:
	// https://google.com/?foo=foobarfoobar&q=query%3Dworld&baz=
}

func ExampleTagFunc() {
	template := "foo[baz]bar"
	t, err := NewTemplate(template, "[", "]", BestCompression)
	if err != nil {
		log.Fatalf("unexpected error when parsing template: %s", err)
	}

	bazSlice := [][]byte{[]byte("123"), []byte("456"), []byte("789")}
	m := map[string]interface{}{
		// Always wrap the function into TagFunc.
		//
		// "baz" tag function writes bazSlice contents into w.
		"baz": TagFunc(func(w io.Writer, tag string) error {
			for _, x := range bazSlice {
				if _, err := w.Write(x); err != nil {
					return err
				}
			}
			return nil
		}),
	}

	s := t.ExecuteBytes(m)
	s = mustDecompress(s)
	fmt.Printf("%s", s)

	// Output:
	// foo123456789bar
}

func ExampleTemplate_ExecuteFuncBytes() {
	template := "Hello, [user]! You won [prize]!!! [foobar]"
	t, err := NewTemplate(template, "[", "]", BestCompression)
	if err != nil {
		log.Fatalf("unexpected error when parsing template: %s", err)
	}
	s := t.ExecuteFuncBytes(func(w io.Writer, tag string) error {
		switch tag {
		case "user":
			_, err := io.WriteString(w, "John")
			return err
		case "prize":
			_, err := io.WriteString(w, "$100500")
			return err
		default:
			_, err := fmt.Fprintf(w, "[unknown tag %q]", tag)
			return err
		}
	})
	s = mustDecompress(s)
	fmt.Printf("%s", s)

	// Output:
	// Hello, John! You won $100500!!! [unknown tag "foobar"]
}
