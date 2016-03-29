package win

import (
	"syscall"
	"unicode/utf16"
	"unsafe"
)

func UTF16ToStringPtr(p *uint16) *string {
	if p == nil {
		return nil
	}
	s := UTF16ToString(p)
	return &s
}

func UTF16ToString(p *uint16) string {
	if p == nil || *p == 0 {
		return ""
	}
	return syscall.UTF16ToString((*[4096]uint16)(unsafe.Pointer(p))[:])
}

type sliceHeader struct {
	Data unsafe.Pointer
	Len  int
	Cap  int
}

func toString(p *uint16) string {
	if p == nil {
		return ""
	}
	return syscall.UTF16ToString((*[4096]uint16)(unsafe.Pointer(p))[:])
}

func toStringSlice(ps *uint16) []string {
	if ps == nil {
		return nil
	}
	r := make([]string, 0)
	for from, i, p := 0, 0, (*[1 << 24]uint16)(unsafe.Pointer(ps)); true; i++ {
		if p[i] == 0 {
			// empty string marks the end
			if i <= from {
				break
			}
			r = append(r, string(utf16.Decode(p[from:i])))
			from = i + 1
		}
	}
	return r
}
