# kube-dns-manager
Automatically manage DNS names for Kubernetes Ingress endpoints.  
Currently only support Google Cloud DNS.  

Based on https://github.com/PalmStoneGames/kube-cert-manager

## Usage
Deploy using the deployment template in the k8s/ folder.  
You need to update it with the correct secrets for managing your Google DNS via the API.  

Add an annotation to your Ingress specs to enable DNS management.
```yaml
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: test-example-com
  annotations:
    k8s.lars.dev/dns: "true"
spec:
  rules:
  - host: "test.example.com"
    http:
      paths:
      - path: /
        backend:
          serviceName: my-svc1
          servicePort: 8080
  - host: "test2.example.com"
    http:
      paths:
      - path: /
        backend:
          serviceName: my-svc2
          servicePort: 8080
```