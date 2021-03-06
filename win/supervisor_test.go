package win

import (
	"fmt"

	"golang.org/x/sys/windows/svc/mgr"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = XDescribe("Supervisor", func() {
	const description = "vcap"
	var (
		svcName string
	)
	var config = mgr.Config{
		Description: description,
	}

	BeforeEach(func() {
		svcName = serviceName()
		config.DisplayName = svcName
	})

	XIt("monitors pre-existing services that match its filter", func() {
		fmt.Println("testing!", svcName)
		fmt.Println("test", 1)
		// manager, service, err := buildAndInstall(svcName, config)
		// fmt.Println("test", 2)
		// Expect(err).ToNot(HaveOccurred())
		// fmt.Println("test", 3)
		// defer func() {
		// service.Close()
		// deleteService(manager, svcName)
		// manager.Disconnect()
		// }()
		// fmt.Println("test", 4)

		// filter := func(name string, _ *mgr.Config) bool {
		// return name == svcName
		// }
		// fmt.Println("test", 5)
		// s, err := NewSupervisor(filter)
		// fmt.Println("test", 6)
		// Expect(err).To(BeNil())
		// fmt.Println("test", 7)
		// defer s.Close()
		// fmt.Println("test", 8)

		// svcs := s.Services()
		// fmt.Println("test", 7)
		// Expect(len(svcs)).To(Equal(1))
		// Expect(svcs[0].Name).To(Equal(svcName))
	})

	XIt("starts monitoring added services", func() {
		filter := func(_ string, conf *mgr.Config) bool {
			return conf.Description == "vcap"
		}
		s, err := NewSupervisor(filter)
		Expect(err).To(BeNil())
		defer s.Close()

		manager, service, err := buildAndInstall(svcName, config)
		Expect(err).ToNot(HaveOccurred())
		defer func() {
			service.Close()
			deleteService(manager, svcName)
			manager.Disconnect()
		}()

		Eventually(func() string {
			svcs := s.Services()
			if len(svcs) > 0 {
				return svcs[0].Name
			}
			return ""
		}).Should(Equal(svcName))
	})

	XIt("removes services that have been deleted", func() {
		manager, service, err := buildAndInstall(svcName, config)
		Expect(err).ToNot(HaveOccurred())
		service.Close()

		defer manager.Disconnect()

		filter := func(name string, _ *mgr.Config) bool {
			return name == svcName
		}
		s, err := NewSupervisor(filter)
		Expect(err).To(BeNil())
		defer s.Close()

		Eventually(func() string {
			svcs := s.Services()
			if len(svcs) == 1 {
				return svcs[0].Name
			}
			return ""
		}).Should(Equal(svcName))
		svcs := s.Services()
		handle := svcs[0].Service.Handle

		deleteService(manager, svcName)

		Eventually(s.Services).Should(BeEmpty())
		Expect(isValidHandle(handle)).To(BeFalse())
	})

	XDescribe("Close", func() {
		It("closes its ServiceControlListener's handle", func() {
			s, err := NewSupervisor(func(_ string, _ *mgr.Config) bool { return true })
			Expect(err).To(BeNil())
			err = s.Close()
			Expect(err).To(BeNil())
			Expect(isValidHandle(s.scmListener.manager.Handle)).To(BeFalse())
		})

		It("closes its ServiceListeners handles", func() {
			s, err := NewSupervisor(func(_ string, _ *mgr.Config) bool { return true })
			Expect(err).To(BeNil())
			aHandle := s.Services()[0].Service.Handle

			err = s.Close()
			Expect(isValidHandle(aHandle)).To(BeFalse())
			Expect(s.Services()).To(BeEmpty())
		})

		It("closes its manager's handler", func() {
			s, err := NewSupervisor(func(_ string, _ *mgr.Config) bool { return true })
			Expect(err).To(BeNil())

			err = s.Close()
			Expect(isValidHandle(s.mgr.Handle)).To(BeFalse())
		})
	})

	XDescribe("closed", func() {
		var s *Supervisor
		BeforeEach(func() {
			var err error
			s, err = NewSupervisor(func(_ string, _ *mgr.Config) bool { return true })
			Expect(err).To(BeNil())
		})

		It("is true when the halt channel has been closed", func() {
			Expect(s.Close()).To(Succeed())
			Expect(s.closed()).To(BeTrue())
		})

		It("is false if the halt channel is still open", func() {
			Expect(s.closed()).To(BeFalse())
		})
	})
})
