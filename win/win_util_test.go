package win

import (
	"errors"
	"fmt"
	"monitor/errno"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	svcpkg "golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

var exePath string

func build(svcName string) (string, error) {
	if exePath == "" {
		exepath, err := filepath.Abs(os.Args[0])
		if err != nil {
			return "", err
		}
		baseDir := filepath.Dir(exepath)

		exePath = filepath.Join(baseDir, "testdata", "bin", svcName+".exe")
		if _, err := os.Stat(exePath); err == nil {
			if err := os.Remove(exePath); err != nil {
				return "", fmt.Errorf("removing previous exe (%s): %s", exePath, err)
			}
		}

		args := []string{
			"build",
			"-o", exePath,
			"-ldflags", fmt.Sprintf("-X main.svcName=%s", svcName),
			filepath.Join("testdata", "service.go"),
		}

		out, err := exec.Command("go", args...).CombinedOutput()
		if err != nil {
			if len(out) == 0 {
				return "", fmt.Errorf("building exe (%s): %s", args, err)
			}
			return "", fmt.Errorf("building exe (%s): %s\noutput:\n%s", args, err, out)
		}
	}

	return exePath, nil
}

func install(m *mgr.Mgr, name, exepath string, c mgr.Config) error {

	s, err := m.CreateService(name, exepath, c)
	if err != nil {
		return fmt.Errorf("CreateService(%s) failed: %v", name, err)
	}
	defer s.Close()

	return nil
}

func buildAndInstall(svcName string, conf mgr.Config) (*mgr.Mgr, *mgr.Service, error) {
	exePath, err := build(svcName)
	if err != nil {
		return nil, nil, err
	}
	m, err := mgr.Connect()
	if err != nil {
		return nil, nil, err
	}
	fmt.Println("installing", svcName)
	if err := install(m, svcName, exePath, conf); err != nil {
		m.Disconnect()
		return nil, nil, err
	}
	s, err := m.OpenService(svcName)
	if err != nil {
		m.Disconnect()
		return nil, nil, err
	}
	return m, s, nil
}

func deleteService(m *mgr.Mgr, name string) error {
	errors := errors.New("")
	s, err := m.OpenService(name)
	if err != nil {
		errors = fmt.Errorf("opening service (%s): %s", name, err)
	}
	defer s.Close()
	if _, err := s.Control(svcpkg.Stop); err != nil {
		errors = fmt.Errorf("%s- stopping service (%s): %s", errors.Error(), name, err)
	}
	if err := s.Delete(); err != nil {
		errors = fmt.Errorf("%s- delete service (%s): %s", errors.Error(), name, err)
	}

	// Sometimes it takes a while for the service to get
	// removed after previous test run.
	for i := 0; ; i++ {
		s, err := m.OpenService(name)
		if err != nil { // service has been deleted!
			break
		}
		s.Close()

		if i > 10 {
			return fmt.Errorf("service %s already exists", name)
		}
		time.Sleep(300 * time.Millisecond)
	}
	return errors
}

func remove(s *mgr.Service) error {
	defer s.Close()
	err := s.Delete()
	if err != nil {
		return fmt.Errorf("Delete failed: %s", err)
	}
	return nil
}

func isValidHandle(handle windows.Handle) bool {
	var bytesNeeded uint32
	_, _, e1 := syscall.Syscall6(
		procQueryServiceConfigW.Addr(),
		uintptr(4),
		uintptr(handle),
		uintptr(0),
		uintptr(0),
		uintptr(unsafe.Pointer(&bytesNeeded)),
		uintptr(0),
		uintptr(0),
	)
	switch e := errno.Errno(e1); e {
	case errno.ERROR_INSUFFICIENT_BUFFER:
		return true
	default:
		return false
	}
}
