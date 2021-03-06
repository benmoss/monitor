package win

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows/svc/mgr"

	"monitor/errno"
)

type ServiceListener struct {
	Name    string
	State   ServiceNotification
	Service *mgr.Service

	updates chan Notification
	halt    chan struct{}
}

func newServiceListener(name string, svc *mgr.Service) *ServiceListener {
	s := &ServiceListener{
		Name:    name,
		Service: svc,
		updates: make(chan Notification, 10),
		halt:    make(chan struct{}),
	}
	return s
}

func (s *ServiceListener) closed() bool {
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

func (s *ServiceListener) Close() (err error) {
	if !s.closed() && s.halt != nil {
		fmt.Println("Before s.halt:", s.halt)
		close(s.halt)
		fmt.Println("After s.halt:", s.halt)
	}
	if s.Service != nil {
		fmt.Println("closing service", goid())
		err = s.Service.Close()
		fmt.Println("closed service", goid())
	}
	return err
}

// func (s ServiceListener) String() string {
// const format = "{Name: %s Status: %s Service: %v}"
// return fmt.Sprintf(format, s.Name, s.Status, s.Service != nil)
// }

func (s *ServiceListener) notify(n *ServiceNotify, act MonitorAction) {
	if s != nil {
		notify := Notification{
			Name:   s.Name,
			Notify: n,
			Action: act,
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

func goid() int {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	idField := strings.Fields(strings.TrimPrefix(string(buf[:n]), "goroutine "))[0]
	id, err := strconv.Atoi(idField)
	if err != nil {
		panic(fmt.Sprintf("cannot get goroutine id: %v", err))
	}
	return id
}

func (s *ServiceListener) notifyStatusChange() {
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
		fmt.Println("ServiceListener called", goid())
		if s.closed() {
			fmt.Println("ServiceListener closed", goid())
			break
		}
		fmt.Println("ServiceListener not closed", s.Service.Handle, goid())
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
			fmt.Println("ServiceListener Not ActionSuccess")
			break
		}

		r1, _, _ = syscall.Syscall(
			procSleepEx.Addr(),
			uintptr(2),
			uintptr(Duration),
			uintptr(Alertable),
			uintptr(0),
		)
		fmt.Println("ServiceListener woke up")
		if r1 == WAIT_IO_COMPLETION {
			s.notify(newServiceNotify(notify), act)
			if notify.NotificationTriggered == SERVICE_NOTIFY_DELETE_PENDING {
				fmt.Println("ServiceListener is deleting")
				break
			}
		}
	}
}
