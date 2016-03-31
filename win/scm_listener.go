package win

import (
	"monitor/errno"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/svc/mgr"
)

type SCMListener struct {
	manager *mgr.Mgr

	updates chan Notification
	halt    chan struct{}
}

func newSCMListener() (*SCMListener, error) {
	mgr, err := mgr.Connect()
	if err != nil {
		return nil, err
	}
	scml := &SCMListener{
		manager: mgr,

		updates: make(chan Notification, 10),
		halt:    make(chan struct{}),
	}
	go scml.notifyStatusChange()
	return scml, nil
}

func (s *SCMListener) notifyStatusChange() {
	const (
		Duration           = MaxUint32 // milliseconds
		Alertable          = 1
		WAIT_IO_COMPLETION = 192
	)
	const mask = SERVICE_NOTIFY_CREATED

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
