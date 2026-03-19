先进masterpod：

kubectl-nluna-edgegetpod-lapp=luna-master
kubectl-nluna-edgeexec-itdeploy/luna-master--sh

进到pod里后，确认lnctl在：

lnctl--help

然后插入一个同时有web和websecure的域名，后端指向luna-edge/nginx-gateway:80。这里用l7-http-both，它会要求HTTP+HTTPS，同时need_cert=true：

lnctl --master http://127.0.0.1:8080 build create nginx-lnctl -d '{"Hostname":"nginx-lnctl.cluster-1.app238.com","DomainEndpoints":[{"Action":"create","Desired":{"id":"domain:nginx-lnctl.cluster-1.app238.com","hostname":"nginx-lnctl.cluster-1.app238.com","need_cert":true,"backend_type":"l7-http-both"}}],"ServiceBackendRefs":[{"Action":"create","Desired":{"id":"backend:nginx-gateway.cluster-1.app238.com:root","type":"SVC","service_namespace":"luna-edge","service_name":"nginx-gateway","service_port":80}}],"HTTPRoutes":[{"Action":"create","Desired":{"id":"route:nginx-lnctl.cluster-1.app238.com:root","domain_endpoint_id":"domain:nginx-gateway.cluster-1.app238.com","path":"/","priority":1,"backend_ref_id":"backend:nginx-gateway.cluster-1.app238.com:root"}}]}'


应用：

lnctl --master http://127.0.0.1:8080 apply nginx-lnctl

查询结果：

lnctl --master http://127.0.0.1:8080 query domain --hostname nginx-lnctl.cluster-1.app238.com

注意两点：

1.这个lnctl是写luna-edge自己的domain/backend/route资源，不会自动创建KubernetesIngress或Gateway.
2.l7-http-both会走证书链路，但前提是你现在运行的master已经是包含我刚才补丁的版本。
