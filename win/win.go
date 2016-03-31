package win

import "syscall"

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

/*
const (
	AccessDenied ServiceListenerStatus = iota
	Ignored
	Watched
)

var svcStatusStr = [...]string{
	"AccessDenied",
	"Ignored",
	"Watched",
}

func (s ServiceListenerStatus) String() string {
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
	Name   string // Only used for service notifications.
	Notify *ServiceNotify
	Action MonitorAction
}
