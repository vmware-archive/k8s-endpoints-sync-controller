// Copyright © 2018 VMware, Inc. All Rights Reserved.
// SPDX-License-Identifier: MIT

package handlers

import (
	c "github.com/vmware/k8s-endpoints-sync-controller/src/config"
	"github.com/vmware/k8s-endpoints-sync-controller/src/log"
	"github.com/vmware/k8s-endpoints-sync-controller/src/utils"
	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"strings"
)

type ClusterDiscoveryHandler struct {
	kubeclient           *kubernetes.Clientset
	label                string
	config               *c.Config
	replicatedNamespaces *utils.ConcurrentMap
	createHandler        HandlerFunc
	updateHandler        HandlerFunc
	deleteHandler        HandlerFunc
}

type HandlerFunc struct {
	handle func(obj interface{})
}

func (s *ClusterDiscoveryHandler) Init(conf *c.Config) error {
	config, configErr := rest.InClusterConfig()
	if configErr != nil {
		log.Errorf("Error fetching incluster config %s", configErr)
		return configErr
	}
	kubeclient, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Errorf("Error creating client with inclusterConfig, %s", err)
		return err
	}
	s.kubeclient = kubeclient
	s.config = conf
	s.replicatedNamespaces = utils.NewConcurrentMap()
	s.prepareCreateHandler()
	s.prepareUpdateHandler()
	s.prepareDeleteHandler()
	return nil
}

func (s *ClusterDiscoveryHandler) prepareCreateHandler() {
	s.createHandler = HandlerFunc{
		handle: func(obj interface{}) {
			switch v := obj.(type) {
			case *v1.Namespace:
				s.handleNamespaceCreate(v)
			case *v1.Endpoints:
				s.handleEnpointCreateOrUpdate(v)
			case *v1.Service:
				s.handleServiceCreate(v, false)
			}
		},
	}
}

func (s *ClusterDiscoveryHandler) prepareUpdateHandler() {
	s.updateHandler = HandlerFunc{
		handle: func(obj interface{}) {
			switch v := obj.(type) {
			case *v1.Namespace:
				s.handleNamespaceUpdate(v)
			case *v1.Endpoints:
				s.handleEnpointCreateOrUpdate(v)
			case *v1.Service:
				s.handleServiceUpdate(v)
			}
		},
	}
}

func (s *ClusterDiscoveryHandler) prepareDeleteHandler() {
	s.deleteHandler = HandlerFunc{
		handle: func(obj interface{}) {
			switch v := obj.(type) {
			case *v1.Namespace:
				s.handleNamespaceDelete(v)
			case *v1.Endpoints:
				s.handleEnpointDelete(v)
			case *v1.Service:
				s.handleServiceDelete(v)
			}
		},
	}
}

func (s *ClusterDiscoveryHandler) ObjectCreated(obj interface{}) {
	if s.shouldProcessEvent(obj) {
		s.handleEvent(obj, s.createHandler)
	}
}

func (s *ClusterDiscoveryHandler) handleEvent(obj interface{}, handler HandlerFunc) {
	handler.handle(obj)
}

func (s *ClusterDiscoveryHandler) ObjectDeleted(obj interface{}) {
	if s.shouldProcessEvent(obj) {
		s.handleEvent(obj, s.deleteHandler)
	}
}

func (s *ClusterDiscoveryHandler) ObjectUpdated(oldObj, newObj interface{}) {
	if s.shouldProcessEvent(newObj) {
		s.handleEvent(newObj, s.updateHandler)
	}
}

func (s *ClusterDiscoveryHandler) handleEnpointCreateOrUpdate(endpoints *v1.Endpoints) {
	log.Debugf("updating endpoints %s namespace %s", endpoints.Name, endpoints.Namespace)
	/*b, _ := json.MarshalIndent(endpoints, "", "  ")
	fmt.Println("In endpoint before update :", string(b))*/
	var endpointsToApply v1.Endpoints
	clusterCIDR := ""
	syndicate_ep := false
	if strings.HasSuffix(endpoints.Name, "-syndicate") || strings.HasSuffix(endpoints.SelfLink, "-syndicate") {
		endpoints.Name = strings.TrimSuffix(endpoints.Name, "-syndicate")
		syndicate_ep = true
	}
	endpointsToApply.Name = endpoints.Name
	endpointsToApply.Labels = endpoints.Labels
	if endpointsToApply.Labels == nil {
		endpointsToApply.Labels = map[string]string{}
	}
	endpointsToApply.Labels[c.REPLICATED_LABEL_KEY] = s.config.ReplicatedLabelVal

	for _, v := range endpoints.Subsets {
		var endpointset v1.EndpointSubset
		for _, address := range v.Addresses {
			if address.IP != "" {
				if clusterCIDR == "" {
					clusterCIDR = address.IP[0:6]
				}
				endpointAddress := v1.EndpointAddress{IP: address.IP}
				if address.Hostname != "" {
					endpointAddress.Hostname = address.Hostname
				}
				endpointset.Addresses = append(endpointset.Addresses, endpointAddress)
			}
		}
		if len(endpointset.Addresses) > 0 {
			for _, port := range v.Ports {
				endpointPort := v1.EndpointPort{Name: port.Name, Port: port.Port, Protocol: port.Protocol}
				endpointset.Ports = append(endpointset.Ports, endpointPort)
			}
			endpointsToApply.Subsets = append(endpointsToApply.Subsets, endpointset)
		}
	}
	unionSvcEndpoint, singularSvcEndpoint := s.checkIfUnionorSingularSvcEndpoint(endpoints)
	if singularSvcEndpoint {
		return
	}
	existingEndpoints, _ := s.kubeclient.CoreV1().Endpoints(endpoints.Namespace).Get(endpoints.Name, meta_v1.GetOptions{})
	if existingEndpoints != nil && existingEndpoints.Name == "" {
		if _, eErr := s.kubeclient.CoreV1().Endpoints(endpoints.Namespace).Create(&endpointsToApply); eErr != nil {
			log.Errorf("Error creating endpoint %s", eErr)
			return
		}
	} else {
		if !syndicate_ep && unionSvcEndpoint {
			if !s.changeInEndpoints(existingEndpoints, &endpointsToApply) {
				log.Infof("No change in endpoints %s namespace %s", existingEndpoints.Name, existingEndpoints.Namespace)
				return
			}
		} else if syndicate_ep {
			for _, v := range existingEndpoints.Subsets {
				var endpointset v1.EndpointSubset
				for _, address := range v.Addresses {
					if clusterCIDR != "" {
						if !strings.HasPrefix(address.IP, clusterCIDR) {
							endpointAddress := v1.EndpointAddress{IP: address.IP}
							if address.Hostname != "" {
								endpointAddress.Hostname = address.Hostname
							}
							endpointset.Addresses = append(endpointset.Addresses, endpointAddress)
						}
					} else {
						endpointAddress := v1.EndpointAddress{IP: address.IP}
						if address.Hostname != "" {
							endpointAddress.Hostname = address.Hostname
						}
						endpointset.Addresses = append(endpointset.Addresses, endpointAddress)
					}
				}
				if len(endpointset.Addresses) > 0 {
					for _, port := range v.Ports {
						endpointPort := v1.EndpointPort{Name: port.Name, Port: port.Port, Protocol: port.Protocol}
						endpointset.Ports = append(endpointset.Ports, endpointPort)
					}
					endpointsToApply.Subsets = append(endpointsToApply.Subsets, endpointset)
				}
			}
		}
		if unionSvcEndpoint {
			endpointsToApply.Labels[c.REPLICATED_LABEL_KEY] = "false"
		}
		if _, eErr := s.kubeclient.CoreV1().Endpoints(endpoints.Namespace).Update(&endpointsToApply); eErr != nil {
			log.Errorf("Error updating endpoint %s", eErr)
			return
		}
	}
}

func (s *ClusterDiscoveryHandler) changeInEndpoints(existingEndpoints *v1.Endpoints, endpointsToApply *v1.Endpoints) bool {
	ipmap := make(map[string]bool)
	for _, v := range existingEndpoints.Subsets {
		for _, address := range v.Addresses {
			ipmap[address.IP] = true
		}
	}
	count := 0
	for _, v := range endpointsToApply.Subsets {
		for _, address := range v.Addresses {
			if _, ok := ipmap[address.IP]; ok {
				count++
			} else {
				return true
			}
		}
	}
	return count != len(ipmap)
}

func (s *ClusterDiscoveryHandler) handleServiceCreate(svc *v1.Service, syndicate_svc bool) {
	log.Infof("creating service %s, namespace %s", svc.Name, svc.Namespace)
	if syndicate_svc {
		svc.Name = svc.Name + "-syndicate"
	}
	existingService, _ := s.kubeclient.CoreV1().Services(svc.Namespace).Get(svc.Name, meta_v1.GetOptions{})
	if existingService != nil && existingService.Name == "" {
		if svc.Annotations[c.SVC_ANNOTATION_SYNDICATE_KEY] == c.SVC_ANNOTATION_SINGULAR {
			return
		}
		service := v1.Service{}
		service.Name = svc.Name
		service.Namespace = svc.Namespace
		service.Spec.Ports = []v1.ServicePort{}
		service.Labels = svc.Labels
		if service.Labels == nil {
			service.Labels = map[string]string{}
		}
		if syndicate_svc {
			service.Spec.Selector = svc.Spec.Selector
			service.Labels[c.REPLICATED_LABEL_KEY] = "false"
		} else {
			service.Labels[c.REPLICATED_LABEL_KEY] = s.config.ReplicatedLabelVal
		}
		for _, port := range svc.Spec.Ports {
			service.Spec.Ports = append(service.Spec.Ports, v1.ServicePort{Protocol: port.Protocol, Name: port.Name, Port: port.Port, TargetPort: port.TargetPort})
		}
		if _, err := s.kubeclient.CoreV1().Services(svc.Namespace).Create(&service); err != nil {
			log.Errorf("Error creating service %s", err)
			return
		}
	} else {
		existingService.Spec.Ports = []v1.ServicePort{}
		for _, port := range svc.Spec.Ports {
			existingService.Spec.Ports = append(existingService.Spec.Ports, v1.ServicePort{Protocol: port.Protocol, Name: port.Name, Port: port.Port, TargetPort: port.TargetPort})
		}
		existingService.Labels = svc.Labels
		if existingService.Labels == nil {
			existingService.Labels = map[string]string{}
		}
		if svc.Annotations[c.SVC_ANNOTATION_SYNDICATE_KEY] == c.SVC_ANNOTATION_SINGULAR {
			if existingService.Labels[c.REPLICATED_LABEL_KEY] == "true" &&
				existingService.Annotations[c.SVC_ANNOTATION_SYNDICATE_KEY] != c.SVC_ANNOTATION_SINGULAR {
				s.handleServiceDelete(existingService)
			}
			return
		}
		existingService.Labels[c.REPLICATED_LABEL_KEY] = s.config.ReplicatedLabelVal
		if _, err := s.kubeclient.CoreV1().Services(svc.Namespace).Update(existingService); err != nil {
			log.Errorf("Error updating service %s", err)
			return
		}
	}
}

func (s *ClusterDiscoveryHandler) handleServiceUpdate(service *v1.Service) {
	log.Infof("updating service %s namespace %s", service.Name, service.Namespace)

	existingService, err := s.kubeclient.CoreV1().Services(service.Namespace).Get(service.Name, meta_v1.GetOptions{})
	if err != nil {
		log.Errorf("Error retrieving service obj, err %s", err)
		return
	}
	if service.Annotations[c.SVC_ANNOTATION_SYNDICATE_KEY] == c.SVC_ANNOTATION_SINGULAR {
		if existingService.Labels[c.REPLICATED_LABEL_KEY] == "true" &&
			existingService.Annotations[c.SVC_ANNOTATION_SYNDICATE_KEY] != c.SVC_ANNOTATION_SINGULAR {
			s.handleServiceDelete(existingService)
		}
		return
	}
	if service.Annotations[c.SVC_ANNOTATION_SYNDICATE_KEY] == c.SVC_ANNOTATION_UNION {

		if existingService.Annotations[c.SVC_ANNOTATION_SYNDICATE_KEY] != c.SVC_ANNOTATION_UNION {
			if existingService.Annotations == nil {
				existingService.Annotations = map[string]string{}
			}
			existingService.Annotations[c.SVC_ANNOTATION_SYNDICATE_KEY] = c.SVC_ANNOTATION_UNION
			if existingService.Labels == nil {
				existingService.Labels = map[string]string{}
			}
			existingService.Labels[c.REPLICATED_LABEL_KEY] = "false"

		} else if existingService.Annotations[c.SVC_ANNOTATION_SYNDICATE_KEY] == c.SVC_ANNOTATION_UNION {
			if existingService.Labels == nil {
				existingService.Labels = map[string]string{}
			}
			existingService.Labels[c.REPLICATED_LABEL_KEY] = "true"
		}
		existingEndpoints, _ := s.kubeclient.CoreV1().Endpoints(service.Namespace).Get(service.Name, meta_v1.GetOptions{})
		if existingEndpoints.Labels == nil {
			existingEndpoints.Labels = map[string]string{}
		}
		existingEndpoints.Labels[c.REPLICATED_LABEL_KEY] = "false"
		existingEndpoints.ResourceVersion = ""
		if _, err := s.kubeclient.CoreV1().Endpoints(service.Namespace).Update(existingEndpoints); err != nil {
			log.Errorf("Error updating endpoints %s", err)
			return
		}
		s.handleServiceCreate(service, true)
		existingService.Spec.Selector = nil
		if _, err := s.kubeclient.CoreV1().Services(service.Namespace).Update(existingService); err != nil {
			log.Errorf("Error updating service %s", err)
			return
		}
		return
	}

	if service.Annotations[c.SVC_ANNOTATION_SYNDICATE_KEY] == c.SVC_ANNOTATION_SOURCE {
		if existingService.Annotations[c.SVC_ANNOTATION_SYNDICATE_KEY] != c.SVC_ANNOTATION_RECEIVER {
			existingEndpoints, err := s.kubeclient.CoreV1().Endpoints(service.Namespace).Get(service.Name, meta_v1.GetOptions{})
			if err != nil {
				log.Errorf("Error retrieving endpoints obj, err %s", err)
				return
			}
			if existingEndpoints.Labels == nil {
				existingEndpoints.Labels = map[string]string{}
			}
			existingEndpoints.Labels[c.REPLICATED_LABEL_KEY] = "false"
			existingEndpoints.ResourceVersion = ""
			if _, eErr := s.kubeclient.CoreV1().Endpoints(service.Namespace).Update(existingEndpoints); eErr != nil {
				log.Errorf("Error updating endpoint %s", eErr)
				return
			}
			if existingService.Labels == nil {
				existingService.Labels = map[string]string{}
			}
			existingService.Labels[c.REPLICATED_LABEL_KEY] = "false"
			if existingService.Annotations == nil {
				existingService.Annotations = map[string]string{}
			}
			existingService.Annotations[c.SVC_ANNOTATION_SYNDICATE_KEY] = c.SVC_ANNOTATION_RECEIVER
			if _, err := s.kubeclient.CoreV1().Services(service.Namespace).Update(existingService); err != nil {
				log.Errorf("Error updating service %s", err)
				return
			}
			service.Name = service.Name + "-syndicate"
			s.handleServiceDelete(service)
			return
		} else if existingService.Annotations[c.SVC_ANNOTATION_SYNDICATE_KEY] == c.SVC_ANNOTATION_RECEIVER {
			existingEndpoints, err := s.kubeclient.CoreV1().Endpoints(service.Namespace).Get(service.Name, meta_v1.GetOptions{})
			if err != nil {
				log.Errorf("Error retrieving endpoints obj, err %s", err)
				return
			}
			if existingEndpoints.Labels == nil {
				existingEndpoints.Labels = map[string]string{}
			}
			existingEndpoints.Labels[c.REPLICATED_LABEL_KEY] = "true"
			existingEndpoints.ResourceVersion = ""
			if _, eErr := s.kubeclient.CoreV1().Endpoints(service.Namespace).Update(existingEndpoints); eErr != nil {
				log.Errorf("Error updating endpoint %s", eErr)
				return
			}
			if existingService.Labels == nil {
				existingService.Labels = map[string]string{}
			}
			existingService.Labels = service.Labels
			existingService.Labels[c.REPLICATED_LABEL_KEY] = "true"
			existingService.Spec.Selector = nil
			if _, err := s.kubeclient.CoreV1().Services(service.Namespace).Update(existingService); err != nil {
				log.Errorf("Error updating service %s", err)
				return
			}
			return
		}
	}

	if service.Annotations[c.SVC_ANNOTATION_SYNDICATE_KEY] == c.SVC_ANNOTATION_RECEIVER {

		if existingService.Annotations[c.SVC_ANNOTATION_SYNDICATE_KEY] != c.SVC_ANNOTATION_SOURCE {

			existingEndpoints, err := s.kubeclient.CoreV1().Endpoints(service.Namespace).Get(service.Name, meta_v1.GetOptions{})
			if err != nil {
				log.Errorf("Error retrieving endpoints obj, err %s", err)
				return
			}
			if existingEndpoints.Labels == nil {
				existingEndpoints.Labels = map[string]string{}
			}
			existingEndpoints.Labels[c.REPLICATED_LABEL_KEY] = "false"
			existingEndpoints.ResourceVersion = ""
			if _, eErr := s.kubeclient.CoreV1().Endpoints(service.Namespace).Update(existingEndpoints); eErr != nil {
				log.Errorf("Error updating endpoint %s", eErr)
				return
			}
			if existingService.Labels == nil {
				existingService.Labels = map[string]string{}
			}
			existingService.Labels[c.REPLICATED_LABEL_KEY] = "false"
			if existingService.Annotations == nil {
				existingService.Annotations = map[string]string{}
			}
			existingService.Annotations[c.SVC_ANNOTATION_SYNDICATE_KEY] = c.SVC_ANNOTATION_SOURCE
			if _, err := s.kubeclient.CoreV1().Services(service.Namespace).Update(existingService); err != nil {
				log.Errorf("Error updating service %s", err)
				return
			}
			service.Name = service.Name + "-syndicate"
			s.handleServiceDelete(service)
			return

		}

		SelectorForSvc := s.getSelectorfromSyndicateSvc(service)
		service.Name = service.Name + "-syndicate"
		s.handleServiceDelete(service)
		if SelectorForSvc != nil {
			existingService.Spec.Selector = SelectorForSvc
		}
		if existingService.Labels == nil {
			existingService.Labels = map[string]string{}
		}
		existingService.Labels[c.REPLICATED_LABEL_KEY] = "false"
		if _, err := s.kubeclient.CoreV1().Services(service.Namespace).Update(existingService); err != nil {
			log.Errorf("Error updating service %s", err)
			return
		}
		existingEndpoints, err := s.kubeclient.CoreV1().Endpoints(service.Namespace).Get(service.Name, meta_v1.GetOptions{})
		if err != nil {
			log.Errorf("Error retrieving endpoints obj, err %s", err)
			return
		}
		if existingEndpoints.Labels == nil {
			existingEndpoints.Labels = map[string]string{}
		}
		existingEndpoints.Labels[c.REPLICATED_LABEL_KEY] = "false"
		existingEndpoints.ResourceVersion = ""
		if _, eErr := s.kubeclient.CoreV1().Endpoints(service.Namespace).Update(existingEndpoints); eErr != nil {
			log.Errorf("Error updating endpoint %s", eErr)
			return
		}
		return
	}

	if existingService != nil && existingService.Name == "" {
		s.handleServiceCreate(service, false)
		return
	}

	existingService.Spec.Ports = []v1.ServicePort{}
	for _, port := range service.Spec.Ports {
		existingService.Spec.Ports = append(existingService.Spec.Ports, v1.ServicePort{Protocol: port.Protocol, Name: port.Name, Port: port.Port, TargetPort: port.TargetPort})
	}
	existingService.Labels = service.Labels
	if existingService.Labels == nil {
		existingService.Labels = map[string]string{}
	}
	existingService.Labels[c.REPLICATED_LABEL_KEY] = s.config.ReplicatedLabelVal
	if _, err := s.kubeclient.CoreV1().Services(service.Namespace).Update(existingService); err != nil {
		log.Errorf("Error updating service %s", err)
		return
	}
}

func (s *ClusterDiscoveryHandler) handleEnpointDelete(endpoints *v1.Endpoints) {
	log.Infof("deleting endpoints %s namespace %s", endpoints.Name, endpoints.Namespace)
	existingService, err := s.kubeclient.CoreV1().Services(endpoints.Namespace).Get(endpoints.Name, meta_v1.GetOptions{})
	if err != nil {
		log.Errorf("Error retrieving service obj, err %s", err)
		return
	}
	if existingService.Annotations[c.SVC_ANNOTATION_SYNDICATE_KEY] == c.SVC_ANNOTATION_SINGULAR {
		return
	}

	if eErr := s.kubeclient.CoreV1().Endpoints(endpoints.Namespace).Delete(endpoints.Name, &meta_v1.DeleteOptions{}); eErr != nil {
		log.Errorf("Error deleting endpoint %s", eErr)
		return
	}
}

func (s *ClusterDiscoveryHandler) handleServiceDelete(service *v1.Service) {
	log.Infof("deleting service %s namespace %s", service.Name, service.Namespace)
	if service.Annotations[c.SVC_ANNOTATION_SYNDICATE_KEY] == c.SVC_ANNOTATION_SINGULAR {
		return
	}
	if eErr := s.kubeclient.CoreV1().Services(service.Namespace).Delete(service.Name, &meta_v1.DeleteOptions{}); eErr != nil {
		log.Errorf("Error deleting service %v", eErr)
		return
	}
}

func (s *ClusterDiscoveryHandler) handleNamespaceCreate(n *v1.Namespace) {
	log.Infof("creating namespace %s", n.Name)
	existingNamespace, _ := s.kubeclient.CoreV1().Namespaces().Get(n.Name, meta_v1.GetOptions{})

	if existingNamespace != nil && existingNamespace.Name == "" {
		ns := v1.Namespace{}
		ns.Name = n.Name
		ns.Labels = n.Labels
		if ns.Labels == nil {
			ns.Labels = map[string]string{}
		}
		ns.Labels[c.REPLICATED_LABEL_KEY] = s.config.ReplicatedLabelVal
		if _, err := s.kubeclient.CoreV1().Namespaces().Create(&ns); err != nil {
			log.Errorf("Error creating namespace %v", err)
			return
		}
	} else {
		existingNamespace.Labels = n.Labels
		if existingNamespace.Labels == nil {
			existingNamespace.Labels = map[string]string{}
		}
		if n.Labels[c.REPLICATED_LABEL_KEY] == s.config.ReplicatedLabelVal {
			existingNamespace.Labels[c.REPLICATED_LABEL_KEY] = "false"
		} else {
			existingNamespace.Labels[c.REPLICATED_LABEL_KEY] = s.config.ReplicatedLabelVal
		}
		if _, err := s.kubeclient.CoreV1().Namespaces().Update(existingNamespace); err != nil {
			log.Errorf("Error updating namespace %v", err)
			return
		}
	}
	s.replicatedNamespaces.Store(n.Name, true)
}

func (s *ClusterDiscoveryHandler) handleNamespaceUpdate(n *v1.Namespace) {
	log.Infof("updating namespace %s", n.Name)

	if s.replicatedNamespaces.Load(n.Name) {
		return
	}

	existingNamespace, _ := s.kubeclient.CoreV1().Namespaces().Get(n.Name, meta_v1.GetOptions{})
	if existingNamespace != nil && existingNamespace.Name == "" {
		s.handleNamespaceCreate(n)
		return
	}

	existingNamespace.Labels = n.Labels
	if existingNamespace.Labels == nil {
		existingNamespace.Labels = map[string]string{}
	}
	if n.Labels[c.REPLICATED_LABEL_KEY] == s.config.ReplicatedLabelVal {
		existingNamespace.Labels[c.REPLICATED_LABEL_KEY] = "false"
	} else {
		existingNamespace.Labels[c.REPLICATED_LABEL_KEY] = s.config.ReplicatedLabelVal
	}
	if _, err := s.kubeclient.CoreV1().Namespaces().Update(existingNamespace); err != nil {
		log.Errorf("Error updating namespace %v", err)
		return
	}
	s.replicatedNamespaces.Store(n.Name, true)
}

func (s *ClusterDiscoveryHandler) handleNamespaceDelete(n *v1.Namespace) {

	log.Infof("deleting namespace %s", n.Name)
	if err := s.kubeclient.CoreV1().Namespaces().Delete(n.Name, &meta_v1.DeleteOptions{}); err != nil {
		log.Errorf("Error deleting namespace %v", err)
		return
	}
	s.replicatedNamespaces.Delete(n.Name)
}

func (s *ClusterDiscoveryHandler) checkIfReplicatedNamespace(namespace string, labels map[string]string) bool {

	if utils.ContainsKeyVal(labels, s.config.ReplicatedLabelVal) {
		if !s.replicatedNamespaces.Load(namespace) {
			s.replicatedNamespaces.Store(namespace, true)
		}
		return true
	}
	return false
}

func (s *ClusterDiscoveryHandler) getSelectorfromSyndicateSvc(service *v1.Service) map[string]string {
	existingService, err := s.kubeclient.CoreV1().Services(service.Namespace).Get(service.Name+"-syndicate", meta_v1.GetOptions{})
	if err != nil {
		log.Errorf("Error retrieving service obj, err %v", err)
		return nil
	}
	return existingService.Spec.Selector
}

func (s *ClusterDiscoveryHandler) checkIfUnionorSingularSvcEndpoint(endpoints *v1.Endpoints) (bool, bool) {
	existingService, err := s.kubeclient.CoreV1().Services(endpoints.Namespace).Get(endpoints.Name, meta_v1.GetOptions{})
	if err != nil {
		log.Errorf("Error retrieving service obj, err %v", err)
		return false, false
	}
	return existingService.Annotations[c.SVC_ANNOTATION_SYNDICATE_KEY] == c.SVC_ANNOTATION_UNION,
		existingService.Annotations[c.SVC_ANNOTATION_SYNDICATE_KEY] == c.SVC_ANNOTATION_SINGULAR
}

func (s *ClusterDiscoveryHandler) shouldProcessEvent(obj interface{}) bool {
	switch v := obj.(type) {
	case *v1.Namespace:
		if s.checkIfReplicatedNamespace(v.Name, v.Labels) || utils.ContainsInArray(s.config.NamespacesToExclude, v.Name) || !utils.CanReplicateNamespace(v.Labels) {
			return false
		}
		return true
	case *v1.Endpoints:
		if utils.ContainsKeyVal(v.Labels, s.config.ReplicatedLabelVal) || !s.replicatedNamespaces.Load(v.Namespace) || v.Name == c.KUBERNETES {
			return false
		}
		return true
	case *v1.Service:
		if strings.HasSuffix(v.Name, "-syndicate") {
			return false
		}
		if utils.ContainsKeyVal(v.Labels, s.config.ReplicatedLabelVal) || !s.replicatedNamespaces.Load(v.Namespace) || v.Name == c.KUBERNETES {
			return false
		}
		return true
	}
	return false
}
