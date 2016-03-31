package win

import (
	"monitor/errno"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/svc/mgr"
)

type Filter func(svcName string, conf *mgr.Config) bool

type Supervisor struct {
	mgr         *mgr.Mgr
	mu          sync.RWMutex // svcs mutex
	svcs        map[string]*Svc
	filter      Filter
	scmListener *SCMListener

	updates chan Notification
	halt    chan struct{}
}

func NewSupervisor(filter Filter) (*Supervisor, error) {
	mgr, err := mgr.Connect()
	if err != nil {
		return nil, err
	}
	scmListener, err := newSCMListener()
	if err != nil {
		return nil, err
	}
	s := &Supervisor{
		mgr:         mgr,
		svcs:        make(map[string]*Svc),
		filter:      filter,
		scmListener: scmListener,

		updates: make(chan Notification, 200),
		halt:    make(chan struct{}, 1),
	}
	if err := s.updateSvcs(); err != nil {
		return nil, err
	}
	go s.listenSCM()
	return s, nil
}

func (s *Supervisor) Close() error {
	err := s.scmListener.Close()
	if err != nil {
		return err
	}
	s.mu.Lock()
	for name, svc := range s.svcs {
		err = svc.Close()
		if err != nil {
			return err
		}
		delete(s.svcs, name)
	}
	s.mu.Unlock()
	err = s.mgr.Disconnect()
	if err != nil {
		return err
	}
	close(s.halt)
	return nil
}

func (s *Supervisor) closed() bool {
	select {
	case <-s.halt:
		return true
	default:
		return false
	}
}

func (s *Supervisor) listenSCM() {
	for {
		select {
		case <-s.halt:
			break
		case n := <-s.scmListener.updates:
			switch n.Action {
			case ActionSuccess:
				// SCM Notifications.
				switch n.Notify.NotificationTriggered {
				case SERVICE_NOTIFY_CREATED:
					for _, name := range n.Notify.ServiceNames {
						s.monitorService(name)
					}
				}
			}
		}
	}
}

// WARN: DEV ONLY
// func (s *Supervisor) Update() ([]*Svc, error) {
// if err := s.updateSvcs(); err != nil {
// return nil, err
// }
// svcs := make([]*Svc, 0, len(s.svcs))
// for _, s := range s.svcs {
// svcs = append(svcs, s)
// }
// return svcs, nil
// }

func (s *Supervisor) Services() []Svc {
	s.mu.RLock()
	svcs := make([]Svc, 0, len(s.svcs))
	for _, s := range s.svcs {
		if s != nil {
			svcs = append(svcs, *s)
		}
	}
	s.mu.RUnlock()
	return svcs
}

func (s *Supervisor) updateSvcs() error {
	// // TODO: Cleanup
	procs, err := s.listServices(SERVICE_WIN32)
	if err != nil {
		return err
	}

	// seen := make(map[string]bool, len(procs))
	for _, p := range procs {
		// seen[p.ServiceName] = true
		if err := s.monitorService(p.ServiceName); err != nil {
			return err
		}
	}

	// // Remove not seen
	// s.mu.Lock()
	// for _, svc := range s.svcs {
	// if !seen[svc.Name] {
	// svc.Close()
	// delete(s.svcs, svc.Name)
	// }
	// }
	// s.mu.Unlock()

	return nil
}

func (s *Supervisor) monitorService(svcName string) error {
	// TODO: Cleanup
	const ERROR_ACCESS_DENIED = syscall.Errno(errno.ERROR_ACCESS_DENIED)

	svc, err := s.mgr.OpenService(svcName)
	if err != nil {
		if err == ERROR_ACCESS_DENIED {
			return nil
		}
		return err
	}

	conf, err := svc.Config()
	if err != nil {
		svc.Close()
		return err
	}
	if !s.filter(svc.Name, &conf) {
		svc.Close()
		return nil
	}
	s.mu.Lock()
	s.svcs[svc.Name] = newSvc(svc.Name, svc)
	s.mu.Unlock()
	return nil
}

// func (s *Supervisor) unmonitorService(svcName string) (err error) {
// s.mu.Lock()
// if svc := s.svcs[svcName]; svc != nil {
// err = svc.Close()
// }
// s.mu.Unlock()
// return err
// }

func (s *Supervisor) listServices(typ ServiceType) ([]EnumServiceStatusProcess, error) {
	var (
		bytesNeeded      uint32
		servicesReturned uint32
		resumeHandle     uint32
		groupName        uint32
	)

	_, _, e1 := syscall.Syscall12(
		procEnumServicesStatusExW.Addr(),
		uintptr(10),
		uintptr(s.mgr.Handle),                      // hSCSupervisor,
		uintptr(SC_ENUM_PROCESS_INFO),              // InfoLevel,
		uintptr(typ),                               // dwServiceType,
		uintptr(SERVICE_STATE_ALL),                 // dwServiceState,
		uintptr(0),                                 // lpServices,
		uintptr(0),                                 // cbBufSize,
		uintptr(unsafe.Pointer(&bytesNeeded)),      // pcbBytesNeeded,
		uintptr(unsafe.Pointer(&servicesReturned)), // lpServicesReturned,
		uintptr(unsafe.Pointer(&resumeHandle)),     // lpResumeHandle,
		uintptr(unsafe.Pointer(&groupName)),        // pszGroupName
		uintptr(0),
		uintptr(0),
	)
	if e1 != 0 {
		switch e := errno.Errno(e1); e {
		case errno.ERROR_MORE_DATA:
			// Ok
		case errno.ERROR_ACCESS_DENIED, errno.ERROR_INVALID_PARAMETER,
			errno.ERROR_INVALID_HANDLE, errno.ERROR_INVALID_LEVEL:
			// Programming error
			return nil, e
		case errno.ERROR_SHUTDOWN_IN_PROGRESS:
			// Trigger stop
			return nil, e
		default:
			return nil, syscall.EINVAL
		}
	}

	var procs []EnumServiceStatusProcess

	// TODO: Allocate buffer once, and advance pointer.
	// TODO: Allocate to nearest ENUM_SERVICE_STATUS_PROCESS
	// size boundry and set cap to prevent GC'ing of string
	// memory.
	if bytesNeeded > 256*1024 {
		bytesNeeded = 256 * 1024
	}
	for bytesNeeded > 0 {
		buffer := make([]byte, bytesNeeded)
		_, _, e1 := syscall.Syscall12(
			procEnumServicesStatusExW.Addr(),
			uintptr(10),
			uintptr(s.mgr.Handle),                      // hSCSupervisor,
			uintptr(SC_ENUM_PROCESS_INFO),              // InfoLevel,
			uintptr(typ),                               // dwServiceType,
			uintptr(SERVICE_STATE_ALL),                 // dwServiceState,
			uintptr(unsafe.Pointer(&buffer[0])),        // lpServices,
			uintptr(len(buffer)),                       // cbBufSize,
			uintptr(unsafe.Pointer(&bytesNeeded)),      // pcbBytesNeeded,
			uintptr(unsafe.Pointer(&servicesReturned)), // lpServicesReturned,
			uintptr(unsafe.Pointer(&resumeHandle)),     // lpResumeHandle,
			uintptr(unsafe.Pointer(&groupName)),        // pszGroupName
			uintptr(0),
			uintptr(0),
		)
		if e1 != 0 {
			switch e := errno.Errno(e1); e {
			case errno.ERROR_MORE_DATA:
				// Ok
			case errno.ERROR_ACCESS_DENIED, errno.ERROR_INVALID_PARAMETER,
				errno.ERROR_INVALID_HANDLE, errno.ERROR_INVALID_LEVEL:
				// Programming error
				return nil, e
			case errno.ERROR_SHUTDOWN_IN_PROGRESS:
				// Trigger stop
				return nil, e
			default:
				return nil, syscall.EINVAL
			}
		}

		var list []ENUM_SERVICE_STATUS_PROCESS
		sp := (*sliceHeader)(unsafe.Pointer(&list))
		sp.Data = unsafe.Pointer(&buffer[0])
		sp.Len = int(servicesReturned)
		sp.Cap = int(servicesReturned)

		for _, p := range list {
			procs = append(procs, EnumServiceStatusProcess{
				ServiceName:          toString(p.ServiceName),
				DisplayName:          toString(p.DisplayName),
				ServiceStatusProcess: p.ServiceStatusProcess,
			})
		}
	}

	return procs, nil
}

// func (s *Supervisor) updateSvcState(svcName string, state ServiceNotification) {
// s.mu.Lock()
// if svc := s.svcs[svcName]; svc != nil {
// svc.State = state
// }
// s.mu.Unlock()
// }

// WARN: RENAME
// func (s *Supervisor) monitor() {
// go func() {
// for n := range s.updates {
// switch n.Action {
// case ActionSuccess:
// // SCM Notifications.
// switch n.Notify.NotificationTriggered {
// case SERVICE_NOTIFY_CREATED:
// for _, name := range n.Notify.ServiceNames {
// s.monitorService(name)
// }

// case SERVICE_NOTIFY_DELETED:
// for _, name := range n.Notify.ServiceNames {
// s.updateSvcState(name, SERVICE_NOTIFY_DELETED)
// s.unmonitorService(name)
// }

// // Service Notifications.
// default:
// s.updateSvcState(n.SvcName, n.Notify.NotificationTriggered)
// }

// case ActionDelete:
// s.unmonitorService(n.SvcName)

// case ActionReload:
// s.unmonitorService(n.SvcName)
// s.monitorService(n.SvcName)
// }
// }
// }()
// }
