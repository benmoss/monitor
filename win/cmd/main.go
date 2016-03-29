package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/windows/svc/mgr"

	"monitor/win"
)

var (
	_ = mgr.Mgr{}
	_ = time.ANSIC
)

/*
t := time.Now()
	N := 100
	for i := 0; i < N; i++ {
		_, err = mgr.Update()
		if err != nil {
			Fatal(err)
		}
	}
	d := time.Since(t)
	fmt.Println(d, d/time.Duration(N))
	return
*/

func main() {
	m, err := win.NewManager()
	if err != nil {
		Fatal(err)
	}
	const typ = win.SERVICE_WIN32_OWN_PROCESS |
		win.SERVICE_WIN32_SHARE_PROCESS |
		win.SERVICE_WIN32

	// procs, err := m.ListServices(typ)
	// if err != nil {
	// 	Fatal(err)
	// }
	// for _, p := range procs {
	// 	fmt.Println(p.ServiceName)
	// }

	fn := func(name string, _ *mgr.Config) bool {
		return strings.Contains(strings.ToLower(name), "v")
	}
	m.AddFilters(fn)

	_, err = m.Update()
	if err != nil {
		Fatal(err)
	}

	// for _, s := range svcs {
	// 	fmt.Println(s)
	// }
	for _, s := range m.Services() {
		fmt.Println(s)
	}
}

func JSON(v interface{}) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("JSON Error: %s", err)
	}
	return string(b)
}

func Fatal(v interface{}) {
	switch e := v.(type) {
	case nil:
		return // Ignore
	case error, string:
		fmt.Fprintln(os.Stderr, "Error:", e)
	default:
		fmt.Fprintf(os.Stderr, "Error: %#v", e)
	}
	os.Exit(1)
}
