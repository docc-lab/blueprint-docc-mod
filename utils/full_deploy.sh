cd /users/tomislav/opentelemetry-collector-contrib && ./build-and-push.sh 10.10.1.1:30000

source ~/.profile && source ~/.bashrc && cd /users/tomislav/blueprint-docc-mod/examples/sockshop && rm -rf build && go run wiring/main.go -w docker -o build

cd /users/tomislav/blueprint-docc-mod/examples/sockshop && cp build/.local.env build/docker/.env && export $(cat build/docker/.env | xargs) && python3 /users/tomislav/blueprint-docc-mod/d2k8s/d2k8s.py --registry 10.10.1.1:30000 --daemon-services otelcol-ctr build/docker/docker-compose.yml build/k8s

cd /users/tomislav/blueprint-docc-mod/examples/sockshop && kubectl delete -f build/k8s/ --ignore-not-found=true && sleep 60 && kubectl apply -f build/k8s/

kubectl patch service frontend-ctr -p '{"spec":{"type":"NodePort"}}' && kubectl patch service jaeger-ctr -p '{"spec":{"type":"NodePort"}}'
