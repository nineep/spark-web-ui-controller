package main

import (
	"flag"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
	"time"
)

var (
	masterURL      string
	kubeConfig     string
	hostSuffix     string
	requestTimeout string
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	// set up signals, so we handle the first shutdown signal gracefully
	stopCh := make(chan struct{})
	defer close(stopCh)

	cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeConfig)
	if err != nil {
		klog.Fatalf("Error building kubeConfig: %s", err.Error())
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error building kubernetes client set: %s", err.Error())
	}

	informerFactory := informers.NewSharedInformerFactory(kubeClient, time.Second*30)
	serviceInformer := informerFactory.Core().V1().Services()

	controller := NewController(hostSuffix, requestTimeout, kubeClient, serviceInformer)

	//notice that there is no need to run Start methods in a separate goroutine. (i.e. go kubeInformerFactory.Start(
	// stopCh)
	//Start method is non-blocking and runs all registered informers in a dedicated goroutine.
	informerFactory.Start(stopCh)

	if err = controller.Run(2, stopCh); err != nil {
		klog.Fatalf("Error running controller: %s", err.Error())
	}
}

func init() {
	flag.StringVar(&kubeConfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&hostSuffix, "hostsuffix", ".spark-ui.example.com", "the host suffix ,"+"ingress address eg: driverPodName.spark-ui.example.com")
	flag.StringVar(&requestTimeout, "request_timeout", "60s", "re"+
		"quest spark ui timeout.")
}
