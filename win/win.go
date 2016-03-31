package win

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows/svc/mgr"

	"monitor/errno"
)

var (
	kernel32DLL = syscall.MustLoadDLL("kernel32")
	advapi32DLL = syscall.MustLoadDLL("Advapi32")
	psapiDLL    = syscall.MustLoadDLL("psapi")

	procGetProcessId              = kernel32DLL.MustFindProc("GetProcessId")
	procSleepEx                   = kernel32DLL.MustFindProc("SleepEx")
	procOpenProcess               = kernel32DLL.MustFindProc("OpenProcess")
	procLocalFree                 = kernel32DLL.MustFindProc("LocalFree")
	procNotifyServiceStatusChange = advapi32DLL.MustFindProc("NotifyServiceStatusChange")
	procEnumServicesStatusExW     = advapi32DLL.MustFindProc("EnumServicesStatusExW")
	procQueryServiceConfigW       = advapi32DLL.MustFindProc("QueryServiceConfigW")
	procQueryServiceConfig2W      = advapi32DLL.MustFindProc("QueryServiceConfig2W")
	procGetProcessMemoryInfo      = psapiDLL.MustFindProc("GetProcessMemoryInfo")
)

type SvcStatus uint32

/*
const (
	AccessDenied SvcStatus = iota
	Ignored
	Watched
)

var svcStatusStr = [...]string{
	"AccessDenied",
	"Ignored",
	"Watched",
}

func (s SvcStatus) String() string {
	if int(s) < len(svcStatusStr) {
		return svcStatusStr[s]
	}
	return "Invalid"
}
*/

type MonitorAction int

const (
	ActionSuccess MonitorAction = iota // Ignore
	ActionDelete                       // Remove from monitors
	ActionReload                       // Close and reload handle
)

type Notification struct {
	SvcName string // Only used for service notifications.
	Notify  *ServiceNotify
	Action  MonitorAction
}

type Svc struct {
	Name    string
	Status  SvcStatus // TODO: Remove
	State   ServiceNotification
	Service *mgr.Service

	updates chan Notification
	halt    chan struct{}
}

func newSvc(name string, svc *mgr.Service) *Svc {
	s := &Svc{
		Name:    name,
		Service: svc,
		updates: make(chan Notification, 10),
		halt:    make(chan struct{}),
	}
	return s
}

func (s *Svc) closed() bool {
	if s.halt == nil {
		return true
	}
	select {
	case <-s.halt:
		return true
	default:
		return false
	}
}

func (s *Svc) Close() (err error) {
	if !s.closed() {
		close(s.halt)
	}
	if s.Service != nil && s.Service.Handle != 0 {
		err = s.Service.Close()
		s.Service.Handle = 0
	}
	return err
}

func (s Svc) String() string {
	const format = "{Name: %s Status: %s Service: %v}"
	return fmt.Sprintf(format, s.Name, s.Status, s.Service != nil)
}

func (s *Svc) notify(n *ServiceNotify, act MonitorAction) {
	if s != nil {
		notify := Notification{
			SvcName: s.Name,
			Notify:  n,
			Action:  act,
		}
		select {
		case s.updates <- notify:
			// Ok
		case <-time.After(time.Millisecond * 50):
			// Error
			// WARN: Dev only
			fmt.Printf("Notify Timout: %+v\n", notify)
		}
	}
}

func (s *Svc) notifyStatusChange() {
	const (
		Duration           = 1000 // milliseconds
		Alertable          = 1
		WAIT_IO_COMPLETION = 192
	)
	const mask = SERVICE_NOTIFY_CONTINUE_PENDING | SERVICE_NOTIFY_DELETE_PENDING |
		SERVICE_NOTIFY_PAUSE_PENDING | SERVICE_NOTIFY_PAUSED |
		SERVICE_NOTIFY_RUNNING | SERVICE_NOTIFY_START_PENDING |
		SERVICE_NOTIFY_STOP_PENDING | SERVICE_NOTIFY_STOPPED

	var notify *SERVICE_NOTIFY
	callback := func(p uintptr) uintptr {
		if p != 0 {
			notify = (*SERVICE_NOTIFY)(unsafe.Pointer(p))
		}
		return 1
	}

	statusNotify := SERVICE_NOTIFY{
		Version:        SERVICE_NOTIFY_STATUS_CHANGE,
		NotifyCallback: syscall.NewCallback(callback),
	}

	defer s.Close()
	for {
		if s.closed() {
			break
		}
		r1, _, _ := syscall.Syscall(
			procNotifyServiceStatusChange.Addr(),
			3,
			uintptr(s.Service.Handle),              // hService
			uintptr(mask),                          // dwNotifyMask
			uintptr(unsafe.Pointer(&statusNotify)), // pNotifyBuffer
		)
		var act MonitorAction
		switch errno.Errno(r1) {
		case errno.ERROR_SUCCESS:
			act = ActionSuccess
		default:
			act = ActionDelete
		}
		if act != ActionSuccess {
			s.notify(newServiceNotify(nil), act)
			break
		}

		r1, _, _ = syscall.Syscall(
			procSleepEx.Addr(),
			uintptr(2),
			uintptr(Duration),
			uintptr(Alertable),
			uintptr(0),
		)
		if r1 == WAIT_IO_COMPLETION {
			s.notify(newServiceNotify(notify), act)
			if notify.NotificationTriggered == SERVICE_NOTIFY_DELETE_PENDING {
				break
			}
		}
	}
}
