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
		mgrMgr *mgr.Mgr
		mgrSvc *mgr.Service
	)
	BeforeEach(func() {
		svcName = serviceName()
		config.DisplayName = svcName

		var err error
		mgrMgr, mgrSvc, err = buildAndInstall(svcName, config)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		mgrSvc.Close()
		deleteService(mgrMgr, svcName)
		mgrMgr.Disconnect()
	})

	It("should notify for status starting", func() {
		svc := &Svc{
			Name:    svcName,
			Service: mgrSvc,

			updates: make(chan Notification, 1),
			halt:    make(chan struct{}),
		}
		defer svc.Close()

		// Start service
		Expect(mgrSvc.Start()).To(Succeed())

		go svc.notifyStatusChange()

		update := <-svc.updates
		Expect(update.SvcName).To(Equal(svcName))

		Expect(update.Notify.NotificationTriggered).To(Or(
			Equal(SERVICE_NOTIFY_RUNNING),
			Equal(SERVICE_NOTIFY_START_PENDING),
		))

	})

	It("should notify for status deleting", func() {
		svc := &Svc{
			Name:    svcName,
			Service: mgrSvc,

			updates: make(chan Notification, 1),
			halt:    make(chan struct{}),
		}
		defer svc.Close()

		// Start service
		Expect(svc.Service.Start()).To(Succeed())
		go svc.notifyStatusChange()
		update := <-svc.updates
		Expect(update.SvcName).To(Equal(svcName))

		Expect(svc.Service.Delete()).To(Succeed())

		Eventually(func() ServiceNotification {
			update := <-svc.updates
			return update.Notify.NotificationTriggered
		}).Should(Equal(SERVICE_NOTIFY_DELETE_PENDING))

		Expect(int(svc.Service.Handle)).To(Equal(0))

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

// 	testSvc, err := manager.mgr.OpenService(svcName)
// 	Expect(err).ToNot(HaveOccurred())
// 	defer remove(testSvc)
// 	Expect(testSvc.Start()).To(Succeed())

// 	Expect(manager.Monitor(svcName)).To(Succeed())
// 	Eventually(func() ServiceState {
// 		return manager.Services()[0].State
// 	}).Should(Equal(SERVICE_RUNNING))

// 	testSvc.Control(svc.Stop)
// })
