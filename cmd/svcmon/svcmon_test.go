package main

import (
	"runtime"
	"strings"
	"syscall"
	"testing"
	"unicode/utf16"

	"golang.org/x/sys/windows/svc/mgr"
)

const longString = `Go is expressive, concise, clean, and efficient. Its
concurrency mechanisms make it easy to write programs that get the most
out of multicore and networked machines, while its novel type system enables
flexible and modular program construction. Go compiles quickly to machine
code yet has the convenience of garbage collection and the power of run-time
reflection. It's a fast, statically typed, compiled language that feels like
a dynamically typed, interpreted language.`

var longStringPtr = syscall.StringToUTF16Ptr(longString)

var utf16ToStringTests = []struct {
	Ptr *uint16
	Exp string
}{
	{
		utf16Ptr(""),
		"",
	},
	{
		nil,
		"",
	},
	{
		utf16Ptr("\x00Hello"),
		"",
	},
	{
		utf16Ptr("hello, world"),
		"hello, world",
	},
	{
		utf16Ptr("hello\x00, world"),
		"hello",
	},
	{
		utf16Ptr(longString),
		longString,
	},
}

func utf16Ptr(s string) *uint16 {
	return &utf16.Encode([]rune(s + "\x00"))[0]
}

func TestUTF16ToString(t *testing.T) {
	for _, x := range utf16ToStringTests {
		s := UTF16ToString(x.Ptr)
		if s != x.Exp {
			t.Errorf("UTF16ToString (%q): %q", x.Exp, s)
		}
	}

	// Max length
	n := (4096 * 2) / len(longString)
	u := strings.Repeat(longString, n)
	s := UTF16ToString(utf16Ptr(u))
	if len(s) != 4096 {
		t.Fatalf("Max length changed expected (%d) got (%d)", 4096, len(s))
	}
	if s != u[0:4096] {
		for i := 0; i < len(s); i++ {
			if s[i] != u[i] {
				t.Fatalf("Encoding string of length (%d) mismatch at: (%d)", len(u), i)
			}
		}
		t.Fatal("Max length failed")
	}
}

func BenchmarkUTF16ToString_Short(b *testing.B) {
	p := utf16Ptr("hello, world")
	for i := 0; i < b.N; i++ {
		UTF16ToString(p)
	}
}

func BenchmarkUTF16ToString_Long(b *testing.B) {
	for i := 0; i < b.N; i++ {
		UTF16ToString(longStringPtr)
	}
}

func BenchmarkListServices(b *testing.B) {
	m, err := mgr.Connect()
	if err != nil {
		b.Fatal(err)
	}
	defer m.Disconnect()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ListServices(m, SERVICE_ALL)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestServiceStatuses(t *testing.T) {
	m, err := mgr.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer m.Disconnect()
	for i := 0; i < 100; i++ {
		_, err := ServiceStatuses(m, SERVICE_ALL)
		if err != nil {
			t.Fatal(err)
		}
		// Make sure *uint16 str ptrs are not GC'd.
		runtime.GC()
	}
}

func BenchmarkServiceStatuses(b *testing.B) {
	m, err := mgr.Connect()
	if err != nil {
		b.Fatal(err)
	}
	defer m.Disconnect()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ServiceStatuses(m, SERVICE_ALL)
		if err != nil {
			b.Fatal(err)
		}
	}
}
