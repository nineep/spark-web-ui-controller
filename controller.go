package main

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	coreinformerv1 "k8s.io/client-go/informers/core/v1"
	networkingv1informerv1 "k8s.io/client-go/informers/networking/v1"
	"k8s.io/client-go/kubernetes"
	corelisterv1 "k8s.io/client-go/listers/core/v1"
	networkinglisterv1 "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
	"strings"
	"time"
)

const (
	driverServiceSuffix = "-driver-svc"
	ingressSuffix       = "-ingress"
	// spark ui ingress backend path
	sparkUIIngressPath = "/"
	// spark ui ingress class
	sparkUIIngressClass = "nginx"
)

type Controller struct {
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface

	servicesSynced cache.InformerSynced
	servicesLister corelisterv1.ServiceLister
	ingressSynced  cache.InformerSynced
	ingressLister  networkinglisterv1.IngressLister

	workqueue workqueue.RateLimitingInterface

	hostsuffix     string
	requestTimeout string
}

// NewController returns a new sample controller
func NewController(
	hostsuffix string,
	requestTimeout string,
	kubeclientset kubernetes.Interface,
	servicesInformer coreinformerv1.ServiceInformer,
	ingressInformer networkingv1informerv1.IngressInformer) *Controller {

	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	servicesInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			klog.Infof("Add service: %s", key)
			if err == nil {
				queue.Add(key)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(newObj)
			klog.Infof("Update service: %s", key)
			if err == nil {
				queue.Add(key)
			}
		},
		DeleteFunc: func(obj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			klog.Infof("Delete service: %s", key)
			if err == nil {
				queue.Add(key)
			}
		},
	})
	controller := &Controller{
		kubeclientset:  kubeclientset,
		servicesSynced: servicesInformer.Informer().HasSynced,
		servicesLister: servicesInformer.Lister(),
		ingressSynced:  ingressInformer.Informer().HasSynced,
		ingressLister:  ingressInformer.Lister(),
		workqueue:      queue,
		hostsuffix:     hostsuffix,
		requestTimeout: requestTimeout,
	}
	return controller
}

// Run is the main path of execution for the controller loop
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	// do the initial synchronization (one time) to populate resources
	if ok := cache.WaitForCacheSync(stopCh, c.HasSynced); !ok {
		return fmt.Errorf("Error syncing cache")
	}
	klog.Info("Starting workers")
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}
	klog.Info("Started workers")
	<-stopCh
	klog.Info("Shutting down workers")
	return nil
}

func (c *Controller) HasSynced() bool {
	return c.servicesSynced() && c.ingressSynced()
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the workqueue.
func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()
	if shutdown {
		return false
	}
	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.workqueue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form namespace/name. We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date that when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.workqueue.Forget(obj)
			runtime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the Service
		// resource to be synced.
		if err := c.syncHandler(key); err != nil {
			// Put the item back on the workqueue to handle any transient errors.
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		klog.Infof("Successfully synced '%s'", key)
		return nil
	}(obj)

	if err != nil {
		runtime.HandleError(err)
		return true
	}
	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the ? resource
// with the current status of the resource.
func (c *Controller) syncHandler(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)

	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	// spark driver svc should end with driverServiceSuffix
	if !strings.HasSuffix(name, driverServiceSuffix) {
		klog.Infof("Get namespace %s's service: %s, not end with %s, ignoring it", namespace, name, driverServiceSuffix)
		return nil
	}
	// Get the service resource with this namespace/name
	service, err := c.servicesLister.Services(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			runtime.HandleError(fmt.Errorf("service '%s' in work queue no longer exists", key))
			// this should a spark driver service deleted event.
			// service not found depend on owner reference to garbage collect ui service and ingress
			return nil
		}
		return err
	}
	// spark driver svc should has a selector spark-role: driver
	if service.Spec.Selector["spark-role"] != "driver" {
		klog.Infof("Get service: %s, has not a selector spark-role: driver, ignoring it", service)
		return nil
	}

	// check and create spark UI ingress
	err = c.createSparkUIIngressFromService(service)
	if err != nil {
		return err
	}
	return nil
}

func isSparkUIServices(serviceName string) bool {
	//TODO: 根据label或者annotation二次验证service是否属于spark driver
	if strings.HasSuffix(serviceName, driverServiceSuffix) {
		klog.Infof("is spark driver service")
		return true
	} else {
		klog.Infof("is not spark driver service")
		return false
	}
}

func getPodNameFromService(service *corev1.Service) string {
	// spark on k8s driver的service的唯一owner为其podName
	podName := service.OwnerReferences[0].Name
	return podName
}

func getIngressNameFromService(service *corev1.Service) string {
	ingressName := service.Name + ingressSuffix
	return ingressName
}

func getSparkDriverUIPort(service *corev1.Service) int32 {
	//- name: spark-ui
	//  port: 4040
	//  protocol: TCP
	//  targetPort: 4040
	for _, port := range service.Spec.Ports {
		if port.Name == "spark-ui" {
			sparkDriverUIPort := port.Port
			return sparkDriverUIPort
		}
	}
	return 4040
}

func (c *Controller) getHostOfIngressFromService(service *corev1.Service) string {
	// host子域名为spark driver pod name，即driverPod.Name + hostSuffix
	// 注意host域名解析需要配置成泛解析
	return getPodNameFromService(service) + c.hostsuffix
}

func (c *Controller) CreateSparkUIIngress(sparkUIService *corev1.Service) (*networkingv1.Ingress, error) {
	// 定义Ingress对象
	sparkUIIC := sparkUIIngressClass
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:            getIngressNameFromService(sparkUIService),
			Namespace:       sparkUIService.Namespace,
			OwnerReferences: sparkUIService.OwnerReferences,
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &sparkUIIC,
			Rules: []networkingv1.IngressRule{
				{
					Host: c.getHostOfIngressFromService(sparkUIService),
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path: sparkUIIngressPath,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: getIngressNameFromService(sparkUIService),
											Port: networkingv1.ServiceBackendPort{
												Number: getSparkDriverUIPort(sparkUIService),
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// 创建或更新Ingress对象
	ctx := context.TODO()
	_, err := c.kubeclientset.NetworkingV1().Ingresses(sparkUIService.Namespace).Create(ctx, ingress, metav1.CreateOptions{})
	if err != nil {
		_, err = c.kubeclientset.NetworkingV1().Ingresses(sparkUIService.Namespace).Update(ctx, ingress, metav1.UpdateOptions{})
		if err != nil {
			panic(err.Error())
		}
		klog.Infof("Ingress %s updated successfully", ingress.Name)
	}
	klog.Infof("Ingress %s created successfully", ingress.Name)
	return ingress, nil
}

// TODO: remove
func (c *Controller) DeleteSparkUIIngress(sparkUIIngressName, sparkUIIngressNS string) error {
	// 删除Ingress对象
	ctx := context.TODO()
	err := c.kubeclientset.NetworkingV1().Ingresses(sparkUIIngressNS).Delete(ctx, sparkUIIngressName, metav1.DeleteOptions{})
	if err != nil {
		panic(err.Error())
	}
	klog.Infof("Ingress %s deleted successfully", sparkUIIngressName)
	return nil
}

// TODO: remove
func (c *Controller) WatchServiceToOperateIngress(namespace string) error {
	ctx := context.TODO()
	// watch all namespace, when namespace=""
	watcher, err := c.kubeclientset.CoreV1().Services(namespace).Watch(ctx, metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}

	for event := range watcher.ResultChan() {
		service, ok := event.Object.(*corev1.Service)
		if !ok {
			continue
		}

		switch event.Type {
		case watch.Added:
			klog.Infof("Service added: %s\n", service.Name)
			// 处理Service创建事件
			// 判断是否为spark driver service
			if isSparkUIServices(service.Name) {
				ingress, err := c.CreateSparkUIIngress(service)
				if err != nil {
					return err
				}
				klog.Infof("spark driver pod service %s 关联的ingress %s 创建成功", service.Name, ingress.Name)
			}
		case watch.Modified:
			klog.Infof("Service modified: %s\n", service.Name)
			// 处理Service更新事件
		case watch.Deleted:
			klog.Infof("Service deleted: %s\n", service.Name)
			// 处理Service删除事件
			if isSparkUIServices(service.Name) {
				ingressName := getIngressNameFromService(service)
				err := c.DeleteSparkUIIngress(ingressName, service.Namespace)
				if err != nil {
					return err
				}
				klog.Infof("spark driver pod service %s 关联的ingress %s 删除成功", service.Name, ingressName)
			}
		}
	}
	return nil
}

// create spark ui ingress from driver svc, if ingress is not exist
func (c *Controller) createSparkUIIngressFromService(service *corev1.Service) error {
	ingressName := getIngressNameFromService(service)
	ingress, err := c.ingressLister.Ingresses(service.Namespace).Get(ingressName)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Infof("spark ui ingress with name: %s is not found, now create one ...", ingress.Name)
			_, err = c.CreateSparkUIIngress(service)
			if err != nil {
				return err
			}
		}
	} else {
		klog.Infof("spark ui ingress with name: %s already exists", ingress.Name)
	}
	return nil
}
