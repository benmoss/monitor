package win

import (
	"fmt"
	"time"

	svcpkg "golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = svcpkg.Stop

func serviceName() string {
	return fmt.Sprintf("test-%d", time.Now().UnixNano())
}

var _ = Describe("Manager", func() {
	const description = "vcap"
	var (
		svcName string
	)
	var config = mgr.Config{
		Description: description,
	}

	var (
		mgrMgr             *mgr.Mgr
		mgrServiceListener *mgr.Service
	)
	BeforeEach(func() {
		svcName = serviceName()
		config.DisplayName = svcName

		var err error
		mgrMgr, mgrServiceListener, err = buildAndInstall(svcName, config)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		mgrServiceListener.Close()
		deleteService(mgrMgr, svcName)
		mgrMgr.Disconnect()
	})

	It("should notify for status starting", func() {
		svc := &ServiceListener{
			Name:    svcName,
			Service: mgrServiceListener,

			updates: make(chan Notification, 1),
			halt:    make(chan struct{}),
		}
		defer svc.Close()

		// Start service
		Expect(mgrServiceListener.Start()).To(Succeed())

		go svc.notifyStatusChange()

		update := <-svc.updates
		Expect(update.Name).To(Equal(svcName))

		Expect(update.Notify.NotificationTriggered).To(Or(
			Equal(SERVICE_NOTIFY_RUNNING),
			Equal(SERVICE_NOTIFY_START_PENDING),
		))

	})

	It("should notify for status deleting", func() {
		svc := &ServiceListener{
			Name:    svcName,
			Service: mgrServiceListener,

			updates: make(chan Notification, 1),
			halt:    make(chan struct{}),
		}
		defer svc.Close()

		// Start service
		Expect(svc.Service.Start()).To(Succeed())
		go svc.notifyStatusChange()
		update := <-svc.updates
		Expect(update.Name).To(Equal(svcName))

		Expect(svc.Service.Delete()).To(Succeed())
		Eventually(func() ServiceNotification {
			update := <-svc.updates
			return update.Notify.NotificationTriggered
		}).Should(Equal(SERVICE_NOTIFY_DELETE_PENDING))

		Eventually(func() bool {
			return isValidHandle(svc.Service.Handle)
		}).Should(BeFalse())

		Expect(svc.closed()).To(BeTrue())
	})

})

// It("keeps in sync with state changes to the service", func() {
// 	svcName := serviceName()
// 	displayName := "display name"
// 	const description = "a description"

// 	manager, err := NewManager()
// 	Expect(err).ToNot(HaveOccurred())
// 	defer manager.Close()

// 	err = manager.Monitor(svcName)
// 	Expect(err).To(HaveOccurred())

// 	exePath, err := build(svcName)
// 	Expect(err).ToNot(HaveOccurred())

// 	err = install(manager.mgr, svcName, exePath, mgr.Config{
// 		DisplayName: displayName,
// 		Description: description,
// 	})
// 	Expect(err).ToNot(HaveOccurred())

// 	testServiceListener, err := manager.mgr.OpenService(svcName)
// 	Expect(err).ToNot(HaveOccurred())
// 	defer remove(testServiceListener)
// 	Expect(testServiceListener.Start()).To(Succeed())

// 	Expect(manager.Monitor(svcName)).To(Succeed())
// 	Eventually(func() ServiceState {
// 		return manager.Services()[0].State
// 	}).Should(Equal(SERVICE_RUNNING))

// 	testServiceListener.Control(svc.Stop)
// })
