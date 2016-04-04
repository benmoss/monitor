package win

import (
	"fmt"
	"monitor/errno"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/svc/mgr"
)

type Filter func(svcName string, conf *mgr.Config) bool

type Supervisor struct {
	mgr              *mgr.Mgr
	filter           Filter
	serviceListeners map[string]*ServiceListener
	scmListener      *SCMListener

	updates chan Notification
	halt    chan struct{}

	mu sync.RWMutex // serviceListeners mutex
}

func NewSupervisor(filter Filter) (*Supervisor, error) {
	fmt.Println("NewSupervisor", 1)
	mgr, err := mgr.Connect()
	fmt.Println("NewSupervisor", 2)
	if err != nil {
		fmt.Println("err connecting")
		return nil, err
	}
	fmt.Println("NewSupervisor", 3)
	scmListener, err := newSCMListener()
	fmt.Println("NewSupervisor", 4)
	if err != nil {
		fmt.Println("err making a newSCMListener")
		return nil, err
	}
	fmt.Println("NewSupervisor", 5)
	s := &Supervisor{
		mgr:              mgr,
		filter:           filter,
		serviceListeners: make(map[string]*ServiceListener),
		scmListener:      scmListener,

		updates: make(chan Notification, 200),
		halt:    make(chan struct{}, 1),
	}
	if err := s.updateServiceListeners(); err != nil {
		fmt.Println("err updateServiceListeners")
		return nil, err
	}
	fmt.Println("NewSupervisor", 6)
	go s.listenSCM()
	fmt.Println("NewSupervisor", 7)
	return s, nil
}

func (s *Supervisor) Close() error {
	err := s.scmListener.Close()
	if err != nil {
		return err
	}
	s.mu.Lock()
	for name, svc := range s.serviceListeners {
		err = svc.Close()
		if err != nil {
			return err
		}
		delete(s.serviceListeners, name)
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
				case SERVICE_NOTIFY_DELETED:
					for _, name := range n.Notify.ServiceNames {
						delete(s.serviceListeners, name)
					}
				}
			}
		}
	}
}

// WARN: DEV ONLY
// func (s *Supervisor) Update() ([]*ServiceListener, error) {
// if err := s.updateServiceListeners(); err != nil {
// return nil, err
// }
// serviceListeners := make([]*ServiceListener, 0, len(s.serviceListeners))
// for _, s := range s.serviceListeners {
// serviceListeners = append(serviceListeners, s)
// }
// return serviceListeners, nil
// }

func (s *Supervisor) Services() []ServiceListener {
	s.mu.RLock()
	serviceListeners := make([]ServiceListener, 0, len(s.serviceListeners))
	for _, s := range s.serviceListeners {
		if s != nil {
			serviceListeners = append(serviceListeners, *s)
		}
	}
	s.mu.RUnlock()
	return serviceListeners
}

func (s *Supervisor) updateServiceListeners() error {
	fmt.Println("updateServiceListeners", 1)
	// TODO: Cleanup
	procs, err := s.listServices(SERVICE_WIN32)
	fmt.Println("updateServiceListeners", 2)
	if err != nil {
		fmt.Println("err listServices")
		return err
	}

	// seen := make(map[string]bool, len(procs))
	fmt.Println("updateServiceListeners", 3)
	for _, p := range procs {
		// seen[p.ServiceName] = true
		fmt.Println("updateServiceListeners", 4, p.ServiceName)
		if err := s.monitorService(p.ServiceName); err != nil {
			fmt.Println("err monitorService", p.ServiceName)
			return err
		}
	}
	fmt.Println("updateServiceListeners", 5)

	// // Remove not seen
	// s.mu.Lock()
	// for _, svc := range s.serviceListeners {
	// if !seen[svc.Name] {
	// svc.Close()
	// delete(s.serviceListeners, svc.Name)
	// }
	// }
	// s.mu.Unlock()

	return nil
}

func (s *Supervisor) monitorService(svcName string) error {
	// TODO: Cleanup
	const ERROR_ACCESS_DENIED = syscall.Errno(errno.ERROR_ACCESS_DENIED)

	fmt.Println("monitorService", 1)

	svc, err := s.mgr.OpenService(svcName)
	fmt.Println("monitorService", 2)
	if err != nil {
		if err == ERROR_ACCESS_DENIED {
			return nil
		}
		fmt.Println("openService failed")
		return err
	}
	fmt.Println("monitorService", 3)

	conf, err := svc.Config()
	fmt.Println("monitorService", 4)
	if err != nil {
		fmt.Println("monitorService", 5)
		svc.Close()
		fmt.Println("svc.Config failed")
		return err
	}
	fmt.Println("monitorService", 6)
	if !s.filter(svc.Name, &conf) {
		svc.Close()
		return nil
	}
	s.mu.Lock()
	s.serviceListeners[svc.Name] = newServiceListener(svc.Name, svc)
	s.mu.Unlock()
	return nil
}

// func (s *Supervisor) unmonitorService(svcName string) (err error) {
// s.mu.Lock()
// if svc := s.serviceListeners[svcName]; svc != nil {
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

// func (s *Supervisor) updateServiceListenerState(svcName string, state ServiceNotification) {
// s.mu.Lock()
// if svc := s.serviceListeners[svcName]; svc != nil {
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
// s.updateServiceListenerState(name, SERVICE_NOTIFY_DELETED)
// s.unmonitorService(name)
// }

// // Service Notifications.
// default:
// s.updateServiceListenerState(n.Name, n.Notify.NotificationTriggered)
// }

// case ActionDelete:
// s.unmonitorService(n.Name)

// case ActionReload:
// s.unmonitorService(n.Name)
// s.monitorService(n.Name)
// }
// }
// }()
// }
