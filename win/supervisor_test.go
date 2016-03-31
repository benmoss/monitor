package win

import (
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/mgr"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Supervisor", func() {
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

	It("monitors pre-existing services that match its filter", func() {
		manager, service, err := buildAndInstall(svcName, config)
		Expect(err).ToNot(HaveOccurred())
		defer func() {
			service.Close()
			deleteService(manager, svcName)
			manager.Disconnect()
		}()

		filter := func(name string, _ *mgr.Config) bool {
			return name == svcName
		}
		s, err := NewSupervisor(filter)
		Expect(err).To(BeNil())
		defer s.Close()

		svcs := s.Services()
		Expect(len(svcs)).To(Equal(1))
		Expect(svcs[0].Name).To(Equal(svcName))
	})

	It("should start monitoring the added service", func() {
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
			if len(svcs) == 1 {
				return svcs[0].Name
			}
			return ""
		}).Should(Equal(svcName))
	})

	Describe("Close", func() {
		It("closes its ServiceControlListener's handle", func() {
			s, err := NewSupervisor(func(_ string, _ *mgr.Config) bool { return true })
			Expect(err).To(BeNil())
			err = s.Close()
			Expect(err).To(BeNil())
			err = windows.CloseServiceHandle(s.scmListener.manager.Handle)
			Expect(err).To(HaveOccurred())
		})

		It("closes its ServiceListeners handles", func() {
			s, err := NewSupervisor(func(_ string, _ *mgr.Config) bool { return true })
			Expect(err).To(BeNil())
			aHandle := s.Services()[0].Service.Handle

			err = s.Close()
			err = windows.CloseServiceHandle(aHandle)
			Expect(err).To(HaveOccurred())
			Expect(s.Services()).To(BeEmpty())
		})
		It("closes its manager's handler", func() {
			s, err := NewSupervisor(func(_ string, _ *mgr.Config) bool { return true })
			Expect(err).To(BeNil())

			err = s.Close()
			err = windows.CloseServiceHandle(s.mgr.Handle)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("closed", func() {
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
