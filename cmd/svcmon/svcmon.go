package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"monitor/errno"
)

const MaxUint32 = 1<<32 - 1

var (
	kernel32DLL = syscall.NewLazyDLL("kernel32")
	advapi32DLL = syscall.MustLoadDLL("Advapi32")
	psapiDLL    = syscall.MustLoadDLL("psapi")

	procGetProcessId              = kernel32DLL.NewProc("GetProcessId")
	procSleepEx                   = kernel32DLL.NewProc("SleepEx")
	procOpenProcess               = kernel32DLL.NewProc("OpenProcess")
	procLocalFree                 = kernel32DLL.NewProc("LocalFree")
	procNotifyServiceStatusChange = advapi32DLL.MustFindProc("NotifyServiceStatusChange")
	procEnumServicesStatusExW     = advapi32DLL.MustFindProc("EnumServicesStatusExW")
	procGetProcessMemoryInfo      = psapiDLL.MustFindProc("GetProcessMemoryInfo")
)

const SC_ENUM_PROCESS_INFO uint32 = 0

type ServiceType uint32

const (
	SERVICE_KERNEL_DRIVER       ServiceType = 0x00000001
	SERVICE_FILE_SYSTEM_DRIVER              = 0x00000002
	SERVICE_DRIVER                          = 0x0000000B
	SERVICE_WIN32_OWN_PROCESS               = 0x00000010
	SERVICE_WIN32_SHARE_PROCESS             = 0x00000020
	SERVICE_WIN32                           = 0x00000030

	SERVICE_ALL = SERVICE_KERNEL_DRIVER | SERVICE_FILE_SYSTEM_DRIVER |
		SERVICE_DRIVER | SERVICE_WIN32_OWN_PROCESS |
		SERVICE_WIN32_SHARE_PROCESS | SERVICE_WIN32
)

var serviceTypeMap = map[ServiceType]string{
	SERVICE_DRIVER:              "SERVICE_DRIVER",
	SERVICE_FILE_SYSTEM_DRIVER:  "SERVICE_FILE_SYSTEM_DRIVER",
	SERVICE_KERNEL_DRIVER:       "SERVICE_KERNEL_DRIVER",
	SERVICE_WIN32:               "SERVICE_WIN32",
	SERVICE_WIN32_OWN_PROCESS:   "SERVICE_WIN32_OWN_PROCESS",
	SERVICE_WIN32_SHARE_PROCESS: "SERVICE_WIN32_SHARE_PROCESS",
}

func (t ServiceType) String() string {
	if s := serviceTypeMap[t]; s != "" {
		return s
	}
	return strconv.FormatUint(uint64(t), 16)
}

type ServiceState uint32

const (
	SERVICE_STOPPED          ServiceState = 1
	SERVICE_START_PENDING                 = 2
	SERVICE_STOP_PENDING                  = 3
	SERVICE_RUNNING                       = 4
	SERVICE_CONTINUE_PENDING              = 5
	SERVICE_PAUSE_PENDING                 = 6
	SERVICE_PAUSED                        = 7
	SERVICE_NO_CHANGE                     = 0xffffffff
)

var serviceStateMap = map[ServiceState]string{
	SERVICE_STOPPED:          "SERVICE_STOPPED",
	SERVICE_START_PENDING:    "SERVICE_START_PENDING",
	SERVICE_STOP_PENDING:     "SERVICE_STOP_PENDING",
	SERVICE_RUNNING:          "SERVICE_RUNNING",
	SERVICE_CONTINUE_PENDING: "SERVICE_CONTINUE_PENDING",
	SERVICE_PAUSE_PENDING:    "SERVICE_PAUSE_PENDING",
	SERVICE_PAUSED:           "SERVICE_PAUSED",
	SERVICE_NO_CHANGE:        "SERVICE_NO_CHANGE",
}

func (c ServiceState) String() string {
	if s := serviceStateMap[c]; s != "" {
		return s
	}
	return strconv.FormatUint(uint64(c), 16)
}

type ServiceControl uint32

const (
	SERVICE_ACCEPT_STOP                  ServiceControl = 1
	SERVICE_ACCEPT_PAUSE_CONTINUE                       = 2
	SERVICE_ACCEPT_SHUTDOWN                             = 4
	SERVICE_ACCEPT_PARAMCHANGE                          = 8
	SERVICE_ACCEPT_NETBINDCHANGE                        = 16
	SERVICE_ACCEPT_HARDWAREPROFILECHANGE                = 32
	SERVICE_ACCEPT_POWEREVENT                           = 64
	SERVICE_ACCEPT_SESSIONCHANGE                        = 128
)

var serviceControlMap = map[ServiceControl]string{
	SERVICE_ACCEPT_STOP:                  "SERVICE_ACCEPT_STOP",
	SERVICE_ACCEPT_PAUSE_CONTINUE:        "SERVICE_ACCEPT_PAUSE_CONTINUE",
	SERVICE_ACCEPT_SHUTDOWN:              "SERVICE_ACCEPT_SHUTDOWN",
	SERVICE_ACCEPT_PARAMCHANGE:           "SERVICE_ACCEPT_PARAMCHANGE",
	SERVICE_ACCEPT_NETBINDCHANGE:         "SERVICE_ACCEPT_NETBINDCHANGE",
	SERVICE_ACCEPT_HARDWAREPROFILECHANGE: "SERVICE_ACCEPT_HARDWAREPROFILECHANGE",
	SERVICE_ACCEPT_POWEREVENT:            "SERVICE_ACCEPT_POWEREVENT",
	SERVICE_ACCEPT_SESSIONCHANGE:         "SERVICE_ACCEPT_SESSIONCHANGE",
}

func (s ServiceControl) String() string {
	if s == 0 {
		return "0x0"
	}
	b := getBuffer()
	defer putBuffer(b)
	for k, v := range serviceControlMap {
		if s&k != 0 {
			if b.Len() != 0 {
				b.WriteByte('|')
			}
			b.WriteString(v)
		}
	}
	return b.String()
}

// https://msdn.microsoft.com/en-us/library/windows/desktop/ms685992(v=vs.85).aspx
type SERVICE_STATUS_PROCESS struct {
	ServiceType             ServiceType
	CurrentState            ServiceState
	ControlsAccepted        ServiceControl
	Win32ExitCode           uint32
	ServiceSpecificExitCode uint32
	CheckPoint              uint32
	WaitHint                uint32
	ProcessId               uint32
	ServiceFlags            uint32
}

func (s SERVICE_STATUS_PROCESS) String() string {
	const format = "{ServiceType: %q, CurrentState: %q, ControlsAccepted: %q, " +
		"Win32ExitCode: %d, ServiceSpecificExitCode: %d, CheckPoint: %d, " +
		"WaitHint: %d, ProcessId: %d, ServiceFlags: %d}"

	return fmt.Sprintf(format, s.ServiceType, s.CurrentState, s.ControlsAccepted,
		s.Win32ExitCode, s.ServiceSpecificExitCode, s.CheckPoint,
		s.WaitHint, s.ProcessId, s.ServiceFlags)
}

func (s SERVICE_STATUS_PROCESS) MarshalJSON() ([]byte, error) {
	type status struct {
		ServiceType             string
		CurrentState            string
		ControlsAccepted        string
		Win32ExitCode           uint32
		ServiceSpecificExitCode uint32
		CheckPoint              uint32
		WaitHint                uint32
		ProcessId               uint32
		ServiceFlags            uint32
	}
	st := status{
		ServiceType:             s.ServiceType.String(),
		CurrentState:            s.CurrentState.String(),
		ControlsAccepted:        s.ControlsAccepted.String(),
		Win32ExitCode:           s.Win32ExitCode,
		ServiceSpecificExitCode: s.ServiceSpecificExitCode,
		CheckPoint:              s.CheckPoint,
		WaitHint:                s.WaitHint,
		ProcessId:               s.ProcessId,
		ServiceFlags:            s.ServiceFlags,
	}
	return json.Marshal(st)
}

type ServiceEnumState uint32

const (
	SERVICE_ACTIVE    ServiceEnumState = 1
	SERVICE_INACTIVE                   = 2
	SERVICE_STATE_ALL                  = 3
)

var serviceEnumStateMap = map[ServiceEnumState]string{
	SERVICE_ACTIVE:    "SERVICE_ACTIVE",
	SERVICE_INACTIVE:  "SERVICE_INACTIVE",
	SERVICE_STATE_ALL: "SERVICE_STATE_ALL",
}

func (s ServiceEnumState) String() string { return serviceEnumStateMap[s] }

type ENUM_SERVICE_STATUS_PROCESS struct {
	ServiceName          *uint16
	DisplayName          *uint16
	ServiceStatusProcess SERVICE_STATUS_PROCESS
}

func (e ENUM_SERVICE_STATUS_PROCESS) String() string {
	const format = "{ServiceName: %s, DisplayName: %s, ServiceStatusProcess: {%s}}"
	return fmt.Sprintf(format, UTF16ToString(e.ServiceName),
		UTF16ToString(e.DisplayName), e.ServiceStatusProcess)
}

func ListServices(m *mgr.Mgr, typ ServiceType) ([]ENUM_SERVICE_STATUS_PROCESS, error) {
	// TODO: If the GC frees portions of the allocated bufffer
	// the ENUM_SERVICE_STATUS_PROCESS *uint16 fields will be
	// invalid, or worse...

	var (
		bytesNeeded      uint32
		servicesReturned uint32
		resumeHandle     uint32
		groupName        uint32
	)
	_, _, e1 := syscall.Syscall12(
		procEnumServicesStatusExW.Addr(),
		uintptr(10),
		uintptr(m.Handle),
		uintptr(SC_ENUM_PROCESS_INFO),
		uintptr(typ),
		uintptr(SERVICE_STATE_ALL),
		uintptr(0),
		uintptr(0),
		uintptr(unsafe.Pointer(&bytesNeeded)),
		uintptr(unsafe.Pointer(&servicesReturned)),
		uintptr(unsafe.Pointer(&resumeHandle)),
		uintptr(unsafe.Pointer(&groupName)),
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

	var procs []ENUM_SERVICE_STATUS_PROCESS

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
			uintptr(m.Handle),
			uintptr(SC_ENUM_PROCESS_INFO),
			uintptr(typ),
			uintptr(SERVICE_STATE_ALL),
			uintptr(unsafe.Pointer(&buffer[0])),
			uintptr(bytesNeeded),
			uintptr(unsafe.Pointer(&bytesNeeded)),
			uintptr(unsafe.Pointer(&servicesReturned)),
			uintptr(unsafe.Pointer(&resumeHandle)),
			uintptr(unsafe.Pointer(&groupName)),
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

		var p []ENUM_SERVICE_STATUS_PROCESS
		sp := (*sliceHeader)(unsafe.Pointer(&p))
		sp.Data = unsafe.Pointer(&buffer[0])
		sp.Len = int(servicesReturned)
		sp.Cap = int(servicesReturned)

		procs = append(procs, p...)
	}

	return procs, nil
}

type sliceHeader struct {
	Data unsafe.Pointer
	Len  int
	Cap  int
}

func main() {
	m, err := mgr.Connect()
	if err != nil {
		Fatal(err)
	}
	defer m.Disconnect()

	// svcType := SERVICE_WIN32 | SERVICE_WIN32_OWN_PROCESS |
	// 	SERVICE_WIN32_SHARE_PROCESS

	svcType := SERVICE_ALL

	t := time.Now()
	_, err = ServiceStatuses(m, svcType)
	if err != nil {
		Fatal(err)
	}
	d := time.Since(t)
	fmt.Println(d, d/time.Duration(100))
}

// func OpenService(m *mgr.Mgr, name *uint16) (*Service, error) {
// 	h, err := windows.OpenService(m.Handle, name, windows.SERVICE_ALL_ACCESS)
// 	if err != nil {
// 		return nil, err
// 	}
// 	return &mgr.Service{Name: name, Handle: h}, nil
// }

func ServiceStatuses(m *mgr.Mgr, typ ServiceType) ([]svc.Status, error) {
	procs, err := ListServices(m, typ)
	if err != nil {
		return nil, err
	}
	var first error
	stats := make([]svc.Status, 0, len(procs))
	for _, p := range procs {
		name := UTF16ToString(p.ServiceName)
		if name == "" {
			continue
		}
		s, err := m.OpenService(name)
		if err != nil {
			fmt.Println("Error:", name, err)
		}
		if err != nil && err.(syscall.Errno) != 0x5 {
			if first == nil {
				first = fmt.Errorf("opening service (%s): %s", name, err)
				continue
			}
		}
		if s != nil {
			defer s.Close()
			st, err := s.Query()
			if err != nil && first == nil {
				first = fmt.Errorf("querying service (%s): %s", name, err)
				continue
			}
			stats = append(stats, st)
		}
	}
	return stats, first
}

func checkErrno(r1 uintptr, err error) error {
	if r1 == 0 {
		if e, ok := err.(syscall.Errno); ok && e != 0 {
			return err
		}
		return syscall.EINVAL
	}
	return nil
}

type ServiceNotification uint32

const (
	SERVICE_NOTIFY_CREATED          ServiceNotification = 0x00000080
	SERVICE_NOTIFY_CONTINUE_PENDING                     = 0x00000010
	SERVICE_NOTIFY_DELETE_PENDING                       = 0x00000200
	SERVICE_NOTIFY_DELETED                              = 0x00000100
	SERVICE_NOTIFY_PAUSE_PENDING                        = 0x00000020
	SERVICE_NOTIFY_PAUSED                               = 0x00000040
	SERVICE_NOTIFY_RUNNING                              = 0x00000008
	SERVICE_NOTIFY_START_PENDING                        = 0x00000002
	SERVICE_NOTIFY_STOP_PENDING                         = 0x00000004
	SERVICE_NOTIFY_STOPPED                              = 0x00000001
)

var serviceNotificationMap = map[ServiceNotification]string{
	SERVICE_NOTIFY_CREATED:          "SERVICE_NOTIFY_CREATED",
	SERVICE_NOTIFY_CONTINUE_PENDING: "SERVICE_NOTIFY_CONTINUE_PENDING",
	SERVICE_NOTIFY_DELETE_PENDING:   "SERVICE_NOTIFY_DELETE_PENDING",
	SERVICE_NOTIFY_DELETED:          "SERVICE_NOTIFY_DELETED",
	SERVICE_NOTIFY_PAUSE_PENDING:    "SERVICE_NOTIFY_PAUSE_PENDING",
	SERVICE_NOTIFY_PAUSED:           "SERVICE_NOTIFY_PAUSED",
	SERVICE_NOTIFY_RUNNING:          "SERVICE_NOTIFY_RUNNING",
	SERVICE_NOTIFY_START_PENDING:    "SERVICE_NOTIFY_START_PENDING",
	SERVICE_NOTIFY_STOP_PENDING:     "SERVICE_NOTIFY_STOP_PENDING",
	SERVICE_NOTIFY_STOPPED:          "SERVICE_NOTIFY_STOPPED",
}

func (n ServiceNotification) String() string {
	if n == 0 {
		return "0x0"
	}
	b := getBuffer()
	defer putBuffer(b)
	for k, v := range serviceNotificationMap {
		if n&k != 0 {
			if b.Len() != 0 {
				b.WriteByte('|')
			}
			b.WriteString(v)
		}
	}
	return b.String()
}

const SERVICE_NOTIFY_STATUS_CHANGE uint32 = 2

// https://msdn.microsoft.com/en-us/library/windows/desktop/ms685947(v=vs.85).aspx
type SERVICE_NOTIFY struct {
	// Structure version. This member must be SERVICE_NOTIFY_STATUS_CHANGE (2).
	Version uint32

	NotifyCallback        uintptr
	Context               uintptr
	NotificationStatus    uint32
	ServiceStatus         SERVICE_STATUS_PROCESS
	NotificationTriggered ServiceNotification

	// ServiceNames
	//
	// If dwNotificationStatus is ERROR_SUCCESS and the notification is
	// SERVICE_NOTIFY_CREATED or SERVICE_NOTIFY_DELETED, this member is
	// valid and it is a MULTI_SZ string that contains one or more service
	// names. The names of the created services will have a '/' prefix so
	// you can distinguish them from the names of the deleted services.
	//
	// NOTE: If this member is valid, the notification callback function must
	// free the string using the LocalFree function.
	//
	// LocalFree: https://msdn.microsoft.com/en-us/library/windows/desktop/aa366730(v=vs.85).aspx
	//
	ServiceNames *uint16
}

func (s SERVICE_NOTIFY) String() string {
	const format = "{Version: %X, NotifyCallback: %X, Context: %X, " +
		"NotificationStatus: %s, ServiceStatus: %s, " +
		"NotificationTriggered: %s, ServiceNames: %s}"

	return fmt.Sprintf(format, s.Version, s.NotifyCallback, s.Context,
		s.NotificationStatus, s.ServiceStatus, s.NotificationTriggered,
		UTF16ToString(s.ServiceNames))
}

func (s SERVICE_NOTIFY) MarshalJSON() ([]byte, error) {
	type notify struct {
		Version               uint32
		NotifyCallback        uintptr
		Context               uintptr
		NotificationStatus    errno.Errno
		ServiceStatus         SERVICE_STATUS_PROCESS
		NotificationTriggered string
		ServiceNames          *string
	}
	n := notify{
		Version:               s.Version,
		NotifyCallback:        s.NotifyCallback,
		Context:               s.Context,
		NotificationStatus:    errno.Errno(s.NotificationStatus),
		ServiceStatus:         s.ServiceStatus,
		NotificationTriggered: s.NotificationTriggered.String(),
		ServiceNames:          UTF16ToStringPtr(s.ServiceNames),
	}
	return json.Marshal(n)
}

func (s *SERVICE_NOTIFY) Free() {
	procLocalFree.Call(uintptr(unsafe.Pointer(s.ServiceNames)))
}

type ServiceMonitor struct {
	name string
	svc  *mgr.Service
	ch   chan *SERVICE_NOTIFY
	errs chan error
	done chan struct{}
}

func NewServiceMonitor(mgr *mgr.Mgr, name string) (*ServiceMonitor, error) {
	svc, err := mgr.OpenService(name)
	if err != nil {
		return nil, err
	}
	m := &ServiceMonitor{
		name: name,
		svc:  svc,
		ch:   make(chan *SERVICE_NOTIFY, 10),
		errs: make(chan error, 10),
		done: make(chan struct{}),
	}
	go m.Monitor()
	go m.NotifyStatusChange()
	return m, nil
}

func (s *ServiceMonitor) Close() {
	close(s.done)
	s.svc.Close()
}

func (s *ServiceMonitor) Monitor() {
	fmt.Println("Monitor")
	for {
		fmt.Println("Monitor: Loop")
		select {
		case <-s.done:
			return
		case err := <-s.errs:
			fmt.Println("Error:", err)
		case n := <-s.ch:
			switch n.NotificationStatus {
			case errno.ERROR_SUCCESS:
				fmt.Println(n)
			case errno.ERROR_SERVICE_MARKED_FOR_DELETE:
				fmt.Println(n.NotificationStatus.String())
				s.Close()
				return
			}
		}
	}
}

func (s *ServiceMonitor) Callback(p uintptr) uintptr {
	fmt.Println("Callback")
	if p != 0 {
		select {
		case s.ch <- (*SERVICE_NOTIFY)(unsafe.Pointer(p)):
		default:
			// Drop SERVICE_NOTIFY
		}
	}
	return 1
}

func (s *ServiceMonitor) NotifyStatusChange() {
	fmt.Println("NotifyStatusChange")
	const (
		Duration         = MaxUint32
		Alertable uint32 = 1
	)
	const mask = SERVICE_NOTIFY_PAUSE_PENDING |
		SERVICE_NOTIFY_PAUSED |
		SERVICE_NOTIFY_RUNNING |
		SERVICE_NOTIFY_START_PENDING |
		SERVICE_NOTIFY_STOP_PENDING |
		SERVICE_NOTIFY_STOPPED

	statusNotify := SERVICE_NOTIFY{
		Version:        SERVICE_NOTIFY_STATUS_CHANGE,
		NotifyCallback: syscall.NewCallback(s.Callback),
	}

	for {
		select {
		case <-s.done:
			return // Exit
		default:
			r1, _, e1 := procNotifyServiceStatusChange.Call(
				uintptr(s.svc.Handle),
				uintptr(mask),
				uintptr(unsafe.Pointer(&statusNotify)),
			)
			fmt.Println("Go")
			if r1 != 0 {
				fmt.Println("FUCK")
				s.errs <- newErrno(r1, e1)
			}
			fmt.Println("Wait")
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

// func main() {
// 	m, err := mgr.Connect()
// 	if err != nil {
// 		Fatal(err)
// 	}
// 	defer m.Disconnect()
// 	x, err := NewServiceMonitor(m, "FontCache")
// 	if err != nil {
// 		Fatal(err)
// 	}
// 	defer x.Close()
// 	time.Sleep(time.Hour)
// }

func XXX() {
	const mask = SERVICE_NOTIFY_PAUSE_PENDING |
		SERVICE_NOTIFY_PAUSED |
		SERVICE_NOTIFY_RUNNING |
		SERVICE_NOTIFY_START_PENDING |
		SERVICE_NOTIFY_STOP_PENDING |
		SERVICE_NOTIFY_STOPPED

	m, err := mgr.Connect()
	if err != nil {
		Fatal(err)
	}
	defer m.Disconnect()
	s, err := m.OpenService("FontCache")
	if err != nil {
		Fatal(err)
	}
	defer s.Close()

	statusCh := make(chan *SERVICE_NOTIFY, 1)
	fn := syscall.NewCallback(func(p uintptr) uintptr {
		if p != 0 {
			status := (*SERVICE_NOTIFY)(unsafe.Pointer(p))
			statusCh <- status
			status.Free()
		}
		return 1
	})
	statusNotify := SERVICE_NOTIFY{
		Version:        SERVICE_NOTIFY_STATUS_CHANGE,
		NotifyCallback: fn,
	}
	go func() {
		for st := range statusCh {
			fmt.Println(JSON(st))
		}
	}()
	const (
		Duration         = MaxUint32
		Alertable uint32 = 1
	)
	for i := 0; i < 10; i++ {
		t := time.Now()
		r1, _, e1 := procNotifyServiceStatusChange.Call(
			uintptr(s.Handle), // Service
			uintptr(mask),
			uintptr(unsafe.Pointer(&statusNotify)),
		)
		if r1 != uintptr(errno.ERROR_SUCCESS) {
			fmt.Println("Error (r1):", errno.Errno(r1))
			fmt.Println("Error (e1):", error(e1))
		}
		procSleepEx.Call(uintptr(Duration), uintptr(Alertable))
		fmt.Printf("%d: Complete: %s", i, time.Since(t))
	}
	fmt.Println("Done")
}

func UTF16ToStringPtr(p *uint16) *string {
	if p == nil {
		return nil
	}
	s := UTF16ToString(p)
	return &s
}

func UTF16ToString(p *uint16) string {
	if p == nil || *p == 0 {
		return ""
	}
	return syscall.UTF16ToString((*[4096]uint16)(unsafe.Pointer(p))[:])
}

var bufferPool sync.Pool

func getBuffer() *bytes.Buffer {
	if v := bufferPool.Get(); v != nil {
		if b, ok := v.(*bytes.Buffer); ok {
			b.Reset()
			return b
		}
	}
	return new(bytes.Buffer)
}

func putBuffer(b *bytes.Buffer) {
	const max = 1024 * 64
	if b != nil && b.Len() < max {
		bufferPool.Put(b)
	}
}

// Utils

func JSON(v interface{}) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("JSON Error: %s", err)
	}
	return string(b)
}

func Fatal(v interface{}) {
	switch e := v.(type) {
	case nil:
		return // Ignore
	case error, string:
		fmt.Fprintln(os.Stderr, "Error:", e)
	default:
		fmt.Fprintf(os.Stderr, "Error: %#v", e)
	}
	os.Exit(1)
}
