package win

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	svcpkg "golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func build(svcName string) (string, error) {
	exepath, err := filepath.Abs(os.Args[0])
	if err != nil {
		return "", err
	}
	baseDir := filepath.Dir(exepath)

	exePath := filepath.Join(baseDir, "testdata", "bin", svcName+".exe")
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

	return exePath, nil
}

func install(m *mgr.Mgr, name, exepath string, c mgr.Config) error {
	// Sometimes it takes a while for the service to get
	// removed after previous test run.
	for i := 0; ; i++ {
		s, err := m.OpenService(name)
		if err != nil {
			break
		}
		s.Close()

		if i > 10 {
			return fmt.Errorf("service %s already exists", name)
		}
		time.Sleep(300 * time.Millisecond)
	}

	s, err := m.CreateService(name, exepath, c)
	if err != nil {
		return fmt.Errorf("CreateService(%s) failed: %v", name, err)
	}
	defer s.Close()

	return nil
}

func deleteService(m *mgr.Mgr, name string) (first error) {
	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("opening service (%s): %s", name, err)
	}
	if _, err := s.Control(svcpkg.Stop); err != nil {
		if first == nil {
			first = fmt.Errorf("stopping service (%s): %s", name, err)
		}
	}
	if err := s.Delete(); err != nil {
		if first == nil {
			first = fmt.Errorf("delete service (%s): %s", name, err)
		}
	}
	if err := s.Close(); err != nil {
		if first == nil {
			first = fmt.Errorf("close service (%s): %s", name, err)
		}
	}
	return first
}

func remove(s *mgr.Service) error {
	defer s.Close()
	err := s.Delete()
	if err != nil {
		return fmt.Errorf("Delete failed: %s", err)
	}
	return nil
}
