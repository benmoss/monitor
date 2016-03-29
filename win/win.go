package win

import (
	"fmt"
	"sync"
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

type Filter func(svcName string, conf *mgr.Config) bool

type Manager struct {
	mgr     *mgr.Mgr
	mu      sync.RWMutex // svcs mutex
	svcs    map[string]*Svc
	updates chan Notification
	filters []Filter
}

func NewManager() (*Manager, error) {
	mgr, err := mgr.Connect()
	if err != nil {
		return nil, err
	}
	m := &Manager{
		mgr:     mgr,
		svcs:    make(map[string]*Svc),
		updates: make(chan Notification, 200),
	}
	return m, nil
}

func (m *Manager) AddFilters(filters ...Filter) {
	m.filters = append(m.filters, filters...)
}

func (m *Manager) Close() (first error) {
	m.mu.Lock()
	for name := range m.svcs {
		m.unmonitorService(name)
		delete(m.svcs, name)
	}
	m.mu.Unlock()
	if err := m.mgr.Disconnect(); err != nil && first == nil {
		first = err
	}
	return first
}

func (m *Manager) Monitor(svcName string) error {
	return m.monitorService(svcName)
}

// WARN: DEV ONLY
func (m *Manager) Update() ([]*Svc, error) {
	if err := m.updateSvcs(); err != nil {
		return nil, err
	}
	svcs := make([]*Svc, 0, len(m.svcs))
	for _, s := range m.svcs {
		svcs = append(svcs, s)
	}
	return svcs, nil
}

// WARN: DEV ONLY
func (m *Manager) Services() []Svc {
	m.mu.RLock()
	svcs := make([]Svc, 0, len(m.svcs))
	for _, s := range m.svcs {
		if s != nil {
			svcs = append(svcs, *s)
		}
	}
	m.mu.RUnlock()
	return svcs
}

func (m *Manager) updateSvcs() error {
	const typ = SERVICE_WIN32_OWN_PROCESS | SERVICE_WIN32_SHARE_PROCESS |
		SERVICE_WIN32

	// TODO: Cleanup
	const ERROR_ACCESS_DENIED = syscall.Errno(errno.ERROR_ACCESS_DENIED)

	procs, err := m.listServices(typ)
	if err != nil {
		return err
	}

	seen := make(map[string]bool, len(procs))
	for _, p := range procs {
		seen[p.ServiceName] = true
		if err := m.monitorService(p.ServiceName); err != nil {
			return err
		}
	}

	// Remove not seen
	m.mu.Lock()
	for _, svc := range m.svcs {
		if !seen[svc.Name] {
			if svc.Monitor != nil {
				svc.Monitor.Close()
				svc.Monitor = nil
			} else {
				svc.Close()
			}
			delete(m.svcs, svc.Name)
		}
	}
	m.mu.Unlock()

	return nil
}

func (m *Manager) include(svc *mgr.Service, conf *mgr.Config) bool {
	if len(m.filters) == 0 {
		return true
	}
	for _, fn := range m.filters {
		if fn(svc.Name, conf) {
			return true
		}
	}
	return false
}

func (m *Manager) monitorService(svcName string) error {
	// TODO: Cleanup
	const ERROR_ACCESS_DENIED = syscall.Errno(errno.ERROR_ACCESS_DENIED)

	m.mu.Lock()
	defer m.mu.Unlock()

	svc, err := m.mgr.OpenService(svcName)
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
	if !m.include(svc, &conf) {
		svc.Close()
		return nil
	}
	m.svcs[svc.Name] = &Svc{
		Name:    svc.Name,
		Service: svc,
	}
	return nil
}

func (m *Manager) unmonitorService(svcName string) (err error) {
	m.mu.Lock()
	if svc := m.svcs[svcName]; svc != nil {
		if svc.Monitor != nil {
			err = svc.Monitor.Close()
			svc.Monitor = nil
		} else {
			err = svc.Close()
		}
	}
	m.mu.Unlock()
	return err
}

func (m *Manager) listServices(typ ServiceType) ([]EnumServiceStatusProcess, error) {
	var (
		bytesNeeded      uint32
		servicesReturned uint32
		resumeHandle     uint32
		groupName        uint32
	)

	_, _, e1 := syscall.Syscall12(
		procEnumServicesStatusExW.Addr(),
		uintptr(10),
		uintptr(m.mgr.Handle),                      // hSCManager,
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
			uintptr(m.mgr.Handle),                      // hSCManager,
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

func (m *Manager) updateSvcState(svcName string, state ServiceNotification) {
	m.mu.Lock()
	if svc := m.svcs[svcName]; svc != nil {
		svc.State = state
	}
	m.mu.Unlock()
}

// WARN: RENAME
func (m *Manager) monitor() {
	go func() {
		for n := range m.updates {
			switch n.Action {
			case ActionSuccess:
				// SCM Notifications.
				switch n.Notify.NotificationTriggered {
				case SERVICE_NOTIFY_CREATED:
					for _, name := range n.Notify.ServiceNames {
						m.monitorService(name)
					}

				case SERVICE_NOTIFY_DELETED:
					for _, name := range n.Notify.ServiceNames {
						m.updateSvcState(name, SERVICE_NOTIFY_DELETED)
						m.unmonitorService(name)
					}

				// Service Notifications.
				default:
					m.updateSvcState(n.SvcName, n.Notify.NotificationTriggered)
				}

			case ActionDelete:
				m.unmonitorService(n.SvcName)

			case ActionReload:
				m.unmonitorService(n.SvcName)
				m.monitorService(n.SvcName)
			}
		}
	}()
}

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
	Monitor *monitor
}

func (s *Svc) Close() (err error) {
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

type monitor struct {
	updates chan Notification
	svc     *Svc
	halt    chan struct{}
}

func (m *monitor) Close() error {
	if !m.closed() {
		close(m.halt)
	}
	if m.svc != nil {
		return m.svc.Close()
	}
	return nil
}

func (m *monitor) notify(s *ServiceNotify, act MonitorAction) {
	if s != nil {
		n := Notification{
			SvcName: m.svc.Name,
			Notify:  s,
			Action:  act,
		}
		select {
		case m.updates <- n:
			// Ok
		case <-time.After(time.Millisecond * 50):
			// Error
			// WARN: Dev only
			fmt.Printf("Notify Timout: %+v\n", n)
		}
	}
}

func (m *monitor) closed() bool {
	select {
	case <-m.halt:
		return true
	default:
		return false
	}
}

func (m *monitor) notifyStatusChange() {
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

	defer m.Close()
	for {
		if m.closed() {
			break
		}
		r1, _, _ := syscall.Syscall(
			procNotifyServiceStatusChange.Addr(),
			3,
			uintptr(m.svc.Service.Handle),          // hService
			uintptr(mask),                          // dwNotifyMask
			uintptr(unsafe.Pointer(&statusNotify)), // pNotifyBuffer
		)
		var act MonitorAction
		switch errno.Errno(r1) {
		case errno.ERROR_SUCCESS:
			act = ActionSuccess
			/*
				case errno.ERROR_SERVICE_NOTIFY_CLIENT_LAGGING:
					act = ActionReload
				case errno.ERROR_SERVICE_MARKED_FOR_DELETE:
					act = ActionDelete
			*/
		default:
			act = ActionDelete
		}
		if act != ActionSuccess {
			m.notify(newServiceNotify(notify), act)
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
			m.notify(newServiceNotify(notify), act)
			if notify.NotificationTriggered == SERVICE_NOTIFY_DELETE_PENDING {
				break
			}
		}
	}
}

/*
func (m *Manager) monitorService(svcName string) error {
	// TODO: Cleanup
	const ERROR_ACCESS_DENIED = syscall.Errno(errno.ERROR_ACCESS_DENIED)

	m.mu.Lock()
	defer m.mu.Unlock()

	svc, err := m.mgr.OpenService(svcName)
	if err != nil {
		if err == ERROR_ACCESS_DENIED {
			m.svcs[svcName] = &Svc{
				Name:   svcName,
				Status: AccessDenied,
			}
			return nil
		}
		return err
	}

	conf, err := svc.Config()
	if err != nil {
		return err
	}
	if m.include(svc, &conf) {
		m.svcs[svc.Name] = &Svc{
			Name:    svc.Name,
			Status:  Watched,
			Service: svc,
		}
		return nil
	}

	m.svcs[svc.Name] = &Svc{
		Name:   svc.Name,
		Status: Ignored,
	}
	svc.Close()

	return nil
}
*/

// type Config struct {
// 	ServiceType ServiceType
// 	StartType   StartType
// 	DisplayName string
// 	Description string
// }

// type Service struct {
// 	svc              *mgr.Service
// 	Name             string
// 	State            ServiceState
// 	ControlsAccepted ServiceControl
// 	Config           *Config
// }

// func (s *Service) Close() error {
// 	if s == nil {
// 		return errors.New("invalid argument")
// 	}
// 	return s.svc.Close()
// }

// OLD
////////////////////////////////////////////////////////////////////////////////

// func (m *Manager) queryServiceDescription(svc *Service) (string, error) {
// 	const SERVICE_CONFIG_DESCRIPTION uint32 = 1
// 	var bytesNeeded uint32
// 	_, _, e1 := syscall.Syscall6(
// 		procQueryServiceConfig2W.Addr(),
// 		uintptr(5),
// 		uintptr(svc.svc.Handle),               // hService
// 		uintptr(SERVICE_CONFIG_DESCRIPTION),   // dwInfoLevel
// 		uintptr(0),                            // lpServiceConfig
// 		uintptr(0),                            // cbBufSize
// 		uintptr(unsafe.Pointer(&bytesNeeded)), // pcbBytesNeeded
// 		uintptr(0),
// 	)
// 	if e1 != 0 {
// 		if errno.Errno(e1) != errno.ERROR_INSUFFICIENT_BUFFER {
// 			return "", error(e1)
// 		}
// 	}

// 	buffer := make([]byte, bytesNeeded)
// 	_, _, e1 = syscall.Syscall6(
// 		procQueryServiceConfig2W.Addr(),
// 		uintptr(5),
// 		uintptr(svc.svc.Handle),               // hService
// 		uintptr(SERVICE_CONFIG_DESCRIPTION),   // dwInfoLevel
// 		uintptr(unsafe.Pointer(&buffer[0])),   // lpServiceConfig
// 		uintptr(len(buffer)),                  // cbBufSize
// 		uintptr(unsafe.Pointer(&bytesNeeded)), // pcbBytesNeeded
// 		uintptr(0),
// 	)
// 	if e1 != 0 {
// 		return "", error(e1)
// 	}

// 	d := (*SERVICE_DESCRIPTION)(unsafe.Pointer(&buffer[0]))
// 	if d == nil {
// 		return "", errors.New("nil: SERVICE_DESCRIPTION")
// 	}
// 	return toString(d.Description), nil
// }

// // TODO: Rename
// func (m *Manager) queryServiceConfig(svc *Service) (*QueryServiceConfig, error) {
// 	var bytesNeeded uint32
// 	_, _, e1 := syscall.Syscall6(
// 		procQueryServiceConfigW.Addr(),
// 		uintptr(4),
// 		uintptr(svc.svc.Handle),               // hService
// 		uintptr(0),                            // lpServiceConfig
// 		uintptr(0),                            // cbBufSize
// 		uintptr(unsafe.Pointer(&bytesNeeded)), // pcbBytesNeeded
// 		uintptr(0),
// 		uintptr(0),
// 	)
// 	if e1 != 0 {
// 		if errno.Errno(e1) != errno.ERROR_INSUFFICIENT_BUFFER {
// 			return nil, error(e1)
// 		}
// 	}

// 	buffer := make([]byte, bytesNeeded)
// 	_, _, e1 = syscall.Syscall6(
// 		procQueryServiceConfigW.Addr(),
// 		uintptr(4),
// 		uintptr(svc.svc.Handle),               // hService
// 		uintptr(unsafe.Pointer(&buffer[0])),   // lpServiceConfig
// 		uintptr(len(buffer)),                  // cbBufSize
// 		uintptr(unsafe.Pointer(&bytesNeeded)), // pcbBytesNeeded
// 		uintptr(0),
// 		uintptr(0),
// 	)
// 	if e1 != 0 {
// 		return nil, error(e1)
// 	}

// 	conf := (*QUERY_SERVICE_CONFIG)(unsafe.Pointer(&buffer[0]))
// 	if conf == nil {
// 		return nil, errors.New("nil: QUERY_SERVICE_CONFIG")
// 	}
// 	return NewQueryServiceConfig(conf), nil
// }
