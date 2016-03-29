package win

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"monitor/errno"
)

const MaxUint32 = 1<<32 - 1

type ServiceType uint32

const (
	SERVICE_KERNEL_DRIVER       ServiceType = 0x00000001
	SERVICE_FILE_SYSTEM_DRIVER  ServiceType = 0x00000002
	SERVICE_DRIVER              ServiceType = 0x0000000B
	SERVICE_WIN32_OWN_PROCESS   ServiceType = 0x00000010
	SERVICE_WIN32_SHARE_PROCESS ServiceType = 0x00000020
	SERVICE_WIN32               ServiceType = 0x00000030
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

type StartType uint32

const (
	SERVICE_BOOT_START   StartType = iota // 0x0
	SERVICE_SYSTEM_START                  // 0x1
	SERVICE_AUTO_START                    // 0x2
	SERVICE_DEMAND_START                  // 0x3
	SERVICE_DISABLED                      // 0x4
)

var startTypeStr = [...]string{
	"SERVICE_BOOT_START",
	"SERVICE_SYSTEM_START",
	"SERVICE_AUTO_START",
	"SERVICE_DEMAND_START",
	"SERVICE_DISABLED",
}

func (t StartType) String() (s string) {
	if int(t) < len(startTypeStr) {
		return startTypeStr[t]
	}
	return strconv.FormatUint(uint64(t), 16)
}

type ServiceState uint32

const (
	SERVICE_STOPPED          ServiceState = 1 + iota // 1
	SERVICE_START_PENDING                            // 2
	SERVICE_STOP_PENDING                             // 3
	SERVICE_RUNNING                                  // 4
	SERVICE_CONTINUE_PENDING                         // 5
	SERVICE_PAUSE_PENDING                            // 6
	SERVICE_PAUSED                                   // 7
	SERVICE_NO_CHANGE        ServiceState = 0xffffffff
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

type ErrorControl uint32

const (
	SERVICE_ERROR_IGNORE   ErrorControl = iota // 0x00
	SERVICE_ERROR_NORMAL                       // 0x01
	SERVICE_ERROR_SEVERE                       // 0x02
	SERVICE_ERROR_CRITICAL                     // 0x03
)

var errorControlStr = [...]string{
	"SERVICE_ERROR_IGNORE",
	"SERVICE_ERROR_NORMAL",
	"SERVICE_ERROR_SEVERE",
	"SERVICE_ERROR_CRITICAL",
}

func (e ErrorControl) String() string {
	if int(e) < len(startTypeStr) {
		return startTypeStr[e]
	}
	return strconv.FormatUint(uint64(e), 16)
}

type ServiceControl uint32

const (
	SERVICE_ACCEPT_STOP                  ServiceControl = 1 << iota // 1
	SERVICE_ACCEPT_PAUSE_CONTINUE                                   // 2
	SERVICE_ACCEPT_SHUTDOWN                                         // 4
	SERVICE_ACCEPT_PARAMCHANGE                                      // 8
	SERVICE_ACCEPT_NETBINDCHANGE                                    // 16
	SERVICE_ACCEPT_HARDWAREPROFILECHANGE                            // 32
	SERVICE_ACCEPT_POWEREVENT                                       // 64
	SERVICE_ACCEPT_SESSIONCHANGE                                    // 128
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
	const format = "{ServiceType: %s, CurrentState: %s, ControlsAccepted: %s, " +
		"Win32ExitCode: %d, ServiceSpecificExitCode: %d, CheckPoint: %d, " +
		"WaitHint: %d, ProcessId: %d, ServiceFlags: %d}"

	return fmt.Sprintf(format, s.ServiceType, s.CurrentState, s.ControlsAccepted,
		s.Win32ExitCode, s.ServiceSpecificExitCode, s.CheckPoint,
		s.WaitHint, s.ProcessId, s.ServiceFlags)
}

type QUERY_SERVICE_CONFIG struct {
	ServiceType      ServiceType
	StartType        StartType
	ErrorControl     ErrorControl
	BinaryPathName   *uint16
	LoadOrderGroup   *uint16
	TagId            uint32
	Dependencies     *uint16
	ServiceStartName *uint16
	DisplayName      *uint16
}

type QueryServiceConfig struct {
	ServiceType      ServiceType
	StartType        StartType
	ErrorControl     ErrorControl
	BinaryPathName   string
	LoadOrderGroup   string
	TagId            uint32
	ServiceStartName string
	DisplayName      string
}

func NewQueryServiceConfig(s *QUERY_SERVICE_CONFIG) *QueryServiceConfig {
	return &QueryServiceConfig{
		ServiceType:      s.ServiceType,
		StartType:        s.StartType,
		ErrorControl:     s.ErrorControl,
		BinaryPathName:   UTF16ToString(s.BinaryPathName),
		LoadOrderGroup:   UTF16ToString(s.LoadOrderGroup),
		TagId:            s.TagId,
		ServiceStartName: UTF16ToString(s.ServiceStartName),
		DisplayName:      UTF16ToString(s.DisplayName),
	}
}

type SERVICE_DESCRIPTION struct {
	Description *uint16
}

type ServiceEnumState uint32

const (
	SERVICE_ACTIVE    ServiceEnumState = 1 + iota // 0x01
	SERVICE_INACTIVE                              // 0x02
	SERVICE_STATE_ALL                             // 0x03
)

var serviceEnumStateMap = map[ServiceEnumState]string{
	SERVICE_ACTIVE:    "SERVICE_ACTIVE",
	SERVICE_INACTIVE:  "SERVICE_INACTIVE",
	SERVICE_STATE_ALL: "SERVICE_STATE_ALL",
}

func (s ServiceEnumState) String() string { return serviceEnumStateMap[s] }

const SC_ENUM_PROCESS_INFO uint32 = 0

// https://msdn.microsoft.com/en-us/library/windows/desktop/ms682648(v=vs.85).aspx
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

type EnumServiceStatusProcess struct {
	ServiceName          string
	DisplayName          string
	ServiceStatusProcess SERVICE_STATUS_PROCESS
}

func (e EnumServiceStatusProcess) String() string {
	const format = "{ServiceName: %s, DisplayName: %s, ServiceStatusProcess: {%s}}"
	return fmt.Sprintf(format, e.ServiceName, e.DisplayName, e.ServiceStatusProcess)
}

// https://msdn.microsoft.com/en-us/library/windows/desktop/ms684276(v=vs.85).aspx
type ServiceNotification uint32

const (

	// SCM handle only.
	SERVICE_NOTIFY_CREATED ServiceNotification = 0x00000080
	SERVICE_NOTIFY_DELETED ServiceNotification = 0x00000100

	// Service handle only.
	SERVICE_NOTIFY_CONTINUE_PENDING ServiceNotification = 0x00000010
	SERVICE_NOTIFY_DELETE_PENDING   ServiceNotification = 0x00000200
	SERVICE_NOTIFY_PAUSE_PENDING    ServiceNotification = 0x00000020
	SERVICE_NOTIFY_PAUSED           ServiceNotification = 0x00000040
	SERVICE_NOTIFY_RUNNING          ServiceNotification = 0x00000008
	SERVICE_NOTIFY_START_PENDING    ServiceNotification = 0x00000002
	SERVICE_NOTIFY_STOP_PENDING     ServiceNotification = 0x00000004
	SERVICE_NOTIFY_STOPPED          ServiceNotification = 0x00000001
)

var serviceNotificationMap = map[ServiceNotification]string{
	SERVICE_NOTIFY_CREATED:          "SERVICE_NOTIFY_CREATED",
	SERVICE_NOTIFY_DELETED:          "SERVICE_NOTIFY_DELETED",
	SERVICE_NOTIFY_CONTINUE_PENDING: "SERVICE_NOTIFY_CONTINUE_PENDING",
	SERVICE_NOTIFY_DELETE_PENDING:   "SERVICE_NOTIFY_DELETE_PENDING",
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

func (s *SERVICE_NOTIFY) Free() {
	procLocalFree.Call(uintptr(unsafe.Pointer(s.ServiceNames)))
}

func (s SERVICE_NOTIFY) String() string {
	const format = "{Version: %X, NotifyCallback: %X, Context: %X, " +
		"NotificationStatus: %X, ServiceStatus: {%s}, " +
		"NotificationTriggered: %s, ServiceNames: %s}"

	return fmt.Sprintf(format, s.Version, s.NotifyCallback, s.Context,
		s.NotificationStatus, s.ServiceStatus, s.NotificationTriggered,
		UTF16ToString(s.ServiceNames))
}

type ServiceNotify struct {
	NotificationStatus    errno.Errno
	ServiceStatus         SERVICE_STATUS_PROCESS
	NotificationTriggered ServiceNotification
	ServiceNames          []string
}

func (s ServiceNotify) String() string {
	const format = "NotificationStatus: %s, ServiceStatus: {%s}, " +
		"NotificationTriggered: %s, ServiceNames: [%s]}"

	return fmt.Sprintf(format, s.NotificationStatus, s.ServiceStatus,
		s.NotificationTriggered, strings.Join(s.ServiceNames, ", "))
}

func newServiceNotify(n *SERVICE_NOTIFY) *ServiceNotify {
	if n == nil {
		return nil
	}
	s := &ServiceNotify{
		NotificationStatus:    errno.Errno(n.NotificationStatus),
		ServiceStatus:         n.ServiceStatus,
		NotificationTriggered: n.NotificationTriggered,
		ServiceNames:          toStringSlice(n.ServiceNames),
	}
	if n.ServiceNames != nil {
		procLocalFree.Call(uintptr(unsafe.Pointer(n.ServiceNames)))
	}
	// The names of the created services have a '/' prefix to
	// distinguish them from the names of the deleted services.
	if s.NotificationTriggered == SERVICE_NOTIFY_CREATED {
		for i := 0; i < len(s.ServiceNames); i++ {
			s.ServiceNames[i] = strings.TrimPrefix(s.ServiceNames[i], "/")
		}
	}
	return s
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
