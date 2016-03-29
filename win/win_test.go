package win

import (
	"fmt"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func serviceName() string {
	return fmt.Sprintf("test-%d", time.Now().UnixNano())
}

var _ = Describe("Manager", func() {
	It("can monitor an existing service", func() {
		svcName := serviceName()
		displayName := "display name"
		const description = "a description"

		manager, err := NewManager()
		Expect(err).ToNot(HaveOccurred())
		defer manager.Close()

		err = manager.Monitor(svcName)
		Expect(err).To(HaveOccurred())

		exePath, err := build(svcName)
		Expect(err).ToNot(HaveOccurred())

		err = install(manager.mgr, svcName, exePath, mgr.Config{
			DisplayName: displayName,
			Description: description,
		})
		Expect(err).ToNot(HaveOccurred())
		defer deleteService(manager.mgr, svcName)

		Expect(manager.Monitor(svcName)).To(Succeed())
		svcs := manager.Services()
		Expect(len(svcs)).To(Equal(1))
		Expect(svcs[0].Name).To(Equal(svcName))
		Expect(svcs[0].Config).ToNot(BeNil())
		Expect(svcs[0].Config.DisplayName).To(Equal(displayName))
		Expect(svcs[0].Config.Description).To(Equal(description))
	})

	It("keeps in sync with state changes to the service", func() {
		svcName := serviceName()
		displayName := "display name"
		const description = "a description"

		manager, err := NewManager()
		Expect(err).ToNot(HaveOccurred())
		defer manager.Close()

		err = manager.Monitor(svcName)
		Expect(err).To(HaveOccurred())

		exePath, err := build(svcName)
		Expect(err).ToNot(HaveOccurred())

		err = install(manager.mgr, svcName, exePath, mgr.Config{
			DisplayName: displayName,
			Description: description,
		})
		Expect(err).ToNot(HaveOccurred())

		testSvc, err := manager.mgr.OpenService(svcName)
		Expect(err).ToNot(HaveOccurred())
		defer remove(testSvc)
		Expect(testSvc.Start()).To(Succeed())

		Expect(manager.Monitor(svcName)).To(Succeed())
		Eventually(func() ServiceState {
			return manager.Services()[0].State
		}).Should(Equal(SERVICE_RUNNING))

		testSvc.Control(svc.Stop)
	})

})
