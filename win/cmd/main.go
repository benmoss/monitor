package main

import (
	"encoding/json"
	"fmt"
	"os"

	"monitor/win"
)

func main() {
	mgr, err := win.NewManager()
	if err != nil {
		Fatal(err)
	}
	typ := win.SERVICE_WIN32_OWN_PROCESS |
		win.SERVICE_WIN32_SHARE_PROCESS |
		win.SERVICE_WIN32

	procs, err := mgr.ListServices(typ)
	if err != nil {
		Fatal(err)
	}
	for _, p := range procs {
		fmt.Println(p.ServiceName)
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
