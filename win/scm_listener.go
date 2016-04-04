package win

import (
	"fmt"
	"monitor/errno"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/svc/mgr"
)

type SCMListener struct {
	manager *mgr.Mgr

	updates chan Notification
	halt    chan struct{}
	ready   chan bool
	once    sync.Once
}

func newSCMListener() (*SCMListener, error) {
	mgr, err := mgr.Connect()
	if err != nil {
		return nil, err
	}
	s := &SCMListener{
		manager: mgr,

		updates: make(chan Notification, 10),
		halt:    make(chan struct{}),
		ready:   make(chan bool, 1),
	}
	go s.notifyStatusChange()
	// <-s.ready
	// close(s.ready)

	return s, nil
}

func (s *SCMListener) notifyStatusChange() {
	const (
		Duration           = MaxUint32 // milliseconds
		Alertable          = 1
		WAIT_IO_COMPLETION = 192
	)
	mask := SERVICE_NOTIFY_CREATED | SERVICE_NOTIFY_DELETED
	//mask := SERVICE_NOTIFY_CREATED
	fmt.Println("THE MASK", uintptr(mask))

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

	for {
		if s.closed() {
			break
		}
		fmt.Println("HANDLE:", s.manager.Handle)
		r1, _, _ := syscall.Syscall(
			procNotifyServiceStatusChange.Addr(),
			3,
			uintptr(s.manager.Handle),              // hService
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
			// this sucks
			s.notify(newServiceNotify(nil), act)
			break
		}
		//s.once.Do(func() { s.ready <- true })
		r1, _, _ = syscall.Syscall(
			procSleepEx.Addr(),
			uintptr(2),
			uintptr(Duration),
			uintptr(Alertable),
			uintptr(0),
		)
		fmt.Println("Woke up")
		if r1 == WAIT_IO_COMPLETION {
			s.notify(newServiceNotify(notify), act)
		}
	}
}

func (s *SCMListener) notify(n *ServiceNotify, act MonitorAction) {
	name := ""
	if n != nil {
		name = strings.Join(n.ServiceNames, ",")
	}
	notify := Notification{
		Name:   name,
		Notify: n,
		Action: act,
	}
	s.updates <- notify
}

func (s *SCMListener) Close() error {
	err := s.manager.Disconnect()
	if err != nil {
		return err
	}
	close(s.halt)
	return nil
}

func (s *SCMListener) closed() bool {
	select {
	case <-s.halt:
		return true
	default:
		return false
	}
}
