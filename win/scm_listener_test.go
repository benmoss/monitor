package win

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/mgr"
)

var _ = Describe("SCMListener", func() {
	XDescribe("closed", func() {
		var scl *SCMListener
		BeforeEach(func() {
			var err error
			scl, err = newSCMListener()
			Expect(err).To(BeNil())
		})

		AfterEach(func() {
			scl.Close()
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

	XDescribe("close", func() {
		It("closes its manager", func() {
			scl, err := newSCMListener()
			Expect(err).To(BeNil())
			Expect(scl.Close()).To(Succeed())
			Expect(isValidHandle(scl.manager.Handle)).To(BeFalse())
		})
	})

	FIt("should notify for status creating", func() {
		fmt.Println("before newSCMListener: ", time.Now().Unix())
		scl, err := newSCMListener()
		fmt.Println("after newSCMListener: ", time.Now().Unix())
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
		Expect(update.Name).To(Equal(svcName))
		Expect(update.Notify.NotificationTriggered).To(Equal(SERVICE_NOTIFY_CREATED))
	})

	XIt("should notify for status deleting", func() {
		svcName := serviceName()
		config := mgr.Config{
			Description: "vcap",
			DisplayName: svcName,
		}
		_, service, err := buildAndInstall(svcName, config)
		Expect(err).To(BeNil())
		service.Close()
		// defer func() {
		// manager.Disconnect()
		// }()

		// scl, err := newSCMListener()
		// Expect(err).To(BeNil())
		// defer scl.Close()

		// deleteService(manager, svcName)

		// update := <-scl.updates
		// Expect(update.Name).To(Equal(svcName))
		// Expect(update.Notify.NotificationTriggered).To(Equal(SERVICE_NOTIFY_DELETED))
	})
})
