package win

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
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

type Manager struct {
	mgr      *mgr.Mgr
	mu       sync.RWMutex
	services map[string]*Service
}

func NewManager() (*Manager, error) {
	mgr, err := mgr.Connect()
	if err != nil {
		return nil, err
	}
	return &Manager{mgr: mgr}, nil
}

func (m *Manager) Close() error {
	return m.mgr.Disconnect()
}

func (m *Manager) Monitor(svcName string) error {
	if m.services == nil {
		m.services = make(map[string]*Service)
	}
	s, err := m.mgr.OpenService(svcName)
	if err != nil {
		return err
	}
	svc := &Service{
		svc:  s,
		Name: svcName,
	}
	conf, err := m.queryServiceConfig(svc)
	if err != nil {
		svc.Close()
		return err
	}
	svc.Config = &Config{
		ServiceType: conf.ServiceType,
		StartType:   conf.StartType,
		DisplayName: conf.DisplayName,
	}
	desc, err := m.queryServiceDescription(svc)
	if err != nil {
		svc.Close()
		return err
	}
	svc.Config.Description = desc
	m.services[svcName] = svc
	return nil
}

func (m *Manager) queryServiceDescription(svc *Service) (string, error) {
	const SERVICE_CONFIG_DESCRIPTION uint32 = 1
	var bytesNeeded uint32
	_, _, e1 := syscall.Syscall6(
		procQueryServiceConfig2W.Addr(),
		uintptr(5),
		uintptr(svc.svc.Handle),               // hService
		uintptr(SERVICE_CONFIG_DESCRIPTION),   // dwInfoLevel
		uintptr(0),                            // lpServiceConfig
		uintptr(0),                            // cbBufSize
		uintptr(unsafe.Pointer(&bytesNeeded)), // pcbBytesNeeded
		uintptr(0),
	)
	if e1 != 0 {
		if errno.Errno(e1) != errno.ERROR_INSUFFICIENT_BUFFER {
			return "", error(e1)
		}
	}

	buffer := make([]byte, bytesNeeded)
	_, _, e1 = syscall.Syscall6(
		procQueryServiceConfig2W.Addr(),
		uintptr(5),
		uintptr(svc.svc.Handle),               // hService
		uintptr(SERVICE_CONFIG_DESCRIPTION),   // dwInfoLevel
		uintptr(unsafe.Pointer(&buffer[0])),   // lpServiceConfig
		uintptr(len(buffer)),                  // cbBufSize
		uintptr(unsafe.Pointer(&bytesNeeded)), // pcbBytesNeeded
		uintptr(0),
	)
	if e1 != 0 {
		return "", error(e1)
	}

	d := (*SERVICE_DESCRIPTION)(unsafe.Pointer(&buffer[0]))
	if d == nil {
		return "", errors.New("nil: SERVICE_DESCRIPTION")
	}
	return toString(d.Description), nil
}

// TODO: Rename
func (m *Manager) queryServiceConfig(svc *Service) (*QueryServiceConfig, error) {
	var bytesNeeded uint32
	_, _, e1 := syscall.Syscall6(
		procQueryServiceConfigW.Addr(),
		uintptr(4),
		uintptr(svc.svc.Handle),               // hService
		uintptr(0),                            // lpServiceConfig
		uintptr(0),                            // cbBufSize
		uintptr(unsafe.Pointer(&bytesNeeded)), // pcbBytesNeeded
		uintptr(0),
		uintptr(0),
	)
	if e1 != 0 {
		if errno.Errno(e1) != errno.ERROR_INSUFFICIENT_BUFFER {
			return nil, error(e1)
		}
	}

	buffer := make([]byte, bytesNeeded)
	_, _, e1 = syscall.Syscall6(
		procQueryServiceConfigW.Addr(),
		uintptr(4),
		uintptr(svc.svc.Handle),               // hService
		uintptr(unsafe.Pointer(&buffer[0])),   // lpServiceConfig
		uintptr(len(buffer)),                  // cbBufSize
		uintptr(unsafe.Pointer(&bytesNeeded)), // pcbBytesNeeded
		uintptr(0),
		uintptr(0),
	)
	if e1 != 0 {
		return nil, error(e1)
	}

	conf := (*QUERY_SERVICE_CONFIG)(unsafe.Pointer(&buffer[0]))
	if conf == nil {
		return nil, errors.New("nil: QUERY_SERVICE_CONFIG")
	}
	return NewQueryServiceConfig(conf), nil
}

func (m *Manager) ListServices(typ ServiceType) ([]EnumServiceStatusProcess, error) {
	return m.listServices(typ)
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

func (m *Manager) Services() []Service {
	svcs := make([]Service, 0, len(m.services))
	for _, s := range m.services {
		if s != nil {
			svcs = append(svcs, *s)
		}
	}
	return svcs
}

type SvcStatus uint32

const (
	AccessDenied SvcStatus = iota
	Ignored
	Watched
)

type Svc struct {
	Name   string
	Status int
}

type Monitor struct {
	mgr  *Manager
	ch   chan *ServiceNotify
	errs chan error
}

func newMonitor(mgr *Manager) (*Monitor, error) {
	m := &Monitor{
		mgr:  mgr,
		ch:   make(chan *ServiceNotify, 10),
		errs: make(chan error, 10),
	}
	return m, nil
}

func (m *Monitor) Callback(p uintptr) uintptr {
	if p != 0 {
		m.ch <- newServiceNotify((*SERVICE_NOTIFY)(unsafe.Pointer(p)))
	}
	return 1
}

func (m *Monitor) sendErr(err error) {
	select {
	case m.errs <- err:
	default:
		fmt.Println("missed error:", err) // WARN
	}
}

// TODO (CEV): RENAME: this only monitors SCM level changes (create/delete).
func (m *Monitor) NotifyStatusChange() {
	const (
		Duration  = MaxUint32
		Alertable = 1
	)
	const mask = SERVICE_NOTIFY_CREATED | SERVICE_NOTIFY_DELETED

	var n SERVICE_NOTIFY
	callback := func(p uintptr) uintptr {
		if p != 0 {
			n = *(*SERVICE_NOTIFY)(unsafe.Pointer(p))
		}
		return 1
	}

	statusNotify := SERVICE_NOTIFY{
		Version:        SERVICE_NOTIFY_STATUS_CHANGE,
		NotifyCallback: syscall.NewCallback(callback),
	}

	for {
		r1, _, e1 := procNotifyServiceStatusChange.Call(
			uintptr(m.mgr.mgr.Handle),
			uintptr(mask),
			uintptr(unsafe.Pointer(&statusNotify)),
		)
		if r1 != 0 {
			m.sendErr(newErrno(r1, e1)) // TODO (CEV): When to exit?
		} else {
			procSleepEx.Call(uintptr(Duration), uintptr(Alertable))
		}
	}
}

func newErrno(r1 uintptr, e1 error) error {
	if r1 <= uintptr(MaxUint32) {
		return fmt.Errorf("%s: %s", errno.Errno(r1), e1)
	}
	return fmt.Errorf("Invalid Errno (%x): %s", r1, e1)
}

type Config struct {
	ServiceType ServiceType
	StartType   StartType
	DisplayName string
	Description string
}

type Service struct {
	svc              *mgr.Service
	Name             string
	State            ServiceState
	ControlsAccepted ServiceControl
	Config           *Config
}

func (s *Service) Close() error {
	if s == nil {
		return errors.New("invalid argument")
	}
	return s.svc.Close()
}
