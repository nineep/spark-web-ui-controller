# Spark Web UI Controller

### What is Spark Web UI?
Apache Spark provides a suite of web user interfaces (UIs) that you can use to monitor the status and resource consumption of your Spark cluster.

### How to access Spark Web UI?
- Standalone： Access: http://IP:4040
- Cluster mode：Through Spark log server xxxxxx:18088 or yarn UI, and enter the corresponding Spark UI interface.

### Problems accessing Spark Web UI when Spark On K8S
- The Spark Web UI does not have a fixed access address and the address it is internal of EKS and is not directly accessible externally.
- Cannot fix the web UI for each spark task to an external LB.
  
## Compile & Build Image
The process of compiling the go language is contained in the Dockerfile.
Into the directory where the Dockerfile is located and run the below command. 
```Shell
docker build -f Dockerfile -t nineep/spark-web-ui-controller:0.0.1 .
```

## Useage
Upload the image produced by the above steps to your repository, and run below command in your kubectl environment:
```Shell
kubectl apply -f deploy-spark-web-ui-controller.yaml
```
