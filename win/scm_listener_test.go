package win

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/mgr"
)

var _ = Describe("SCMListener", func() {
	Describe("closed", func() {
		var scl *SCMListener
		BeforeEach(func() {
			var err error
			scl, err = newSCMListener()
			Expect(err).To(BeNil())
		})

		It("is true when the halt channel has been closed", func() {
			Expect(scl.Close()).To(Succeed())
			Expect(scl.closed()).To(BeTrue())

		})

		It("is false if the halt channel is still open", func() {
			Expect(scl.manager.Handle).ToNot(Equal(windows.InvalidHandle))
			Expect(scl.closed()).To(BeFalse())
		})
	})

	Describe("close", func() {
		It("closes its manager", func() {
			scl, err := newSCMListener()
			Expect(err).To(BeNil())
			Expect(scl.Close()).To(Succeed())
			err = windows.CloseServiceHandle(scl.manager.Handle)
			Expect(err).To(HaveOccurred())
		})
	})

	It("should notify for status creating", func(done Done) {
		scl, err := newSCMListener()
		Expect(err).To(BeNil())
		defer scl.Close()

		svcName := serviceName()
		config := mgr.Config{
			Description: "vcap",
			DisplayName: svcName,
		}
		manager, service, err := buildAndInstall(svcName, config)
		Expect(err).To(BeNil())
		defer func() {
			service.Close()
			deleteService(manager, svcName)
			manager.Disconnect()
		}()

		update := <-scl.updates
		Expect(update.SvcName).To(Equal(svcName))
		Expect(update.Notify.NotificationTriggered).To(Equal(SERVICE_NOTIFY_CREATED))
		close(done)
	}, 5.0)
})
