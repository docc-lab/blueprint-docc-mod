#!/bin/bash

set -e

# Helper: require kubectl
if ! command -v kubectl >/dev/null 2>&1; then
  echo "[ERROR] kubectl not found in PATH"
  exit 1
fi

######## TODO: UPDATE this username and hostname #####
# Optional config (override via env): SSH user and node hostname for tunneling
SSH_USER=${SSH_USER:-"maxLliu"}
NODE_HOST=${NODE_HOST:-"c220g1-031128.wisc.cloudlab.us"} # node-0 hostname where Jaeger runs

echo "==== Discovering NodePorts ===="

# Fetch NodePorts for Jaeger UI (16686) and Frontend (12349)
JAEGER_NODEPORT=$(kubectl get svc jaeger-ctr -o jsonpath='{.spec.ports[?(@.port==16686)].nodePort}' 2>/dev/null || true)
FRONTEND_NODEPORT=$(kubectl get svc frontend-ctr -o jsonpath='{.spec.ports[?(@.port==12349)].nodePort}' 2>/dev/null || true)

# Fallback: if selectors by port failed, list and try by name or first matching
if [ -z "$JAEGER_NODEPORT" ]; then
  JAEGER_NODEPORT=$(kubectl get svc jaeger-ctr -o jsonpath='{range .spec.ports[*]}{.port}:{.nodePort}{"\n"}{end}' 2>/dev/null | awk -F: '$1==16686 {print $2; exit}')
fi
if [ -z "$FRONTEND_NODEPORT" ]; then
  FRONTEND_NODEPORT=$(kubectl get svc frontend-ctr -o jsonpath='{range .spec.ports[*]}{.port}:{.nodePort}{"\n"}{end}' 2>/dev/null | awk -F: '$1==12349 {print $2; exit}')
fi

if [ -z "$JAEGER_NODEPORT" ] || [ -z "$FRONTEND_NODEPORT" ]; then
  echo "[ERROR] Could not determine NodePorts. Current service ports:"
  echo "- jaeger-ctr:"; kubectl get svc jaeger-ctr -o jsonpath='{range .spec.ports[*]}{.port}:{.nodePort}{"\n"}{end}' 2>/dev/null || true
  echo "- frontend-ctr:"; kubectl get svc frontend-ctr -o jsonpath='{range .spec.ports[*]}{.port}:{.nodePort}{"\n"}{end}' 2>/dev/null || true
  exit 1
fi

echo "[INFO] Jaeger UI    port 16686 -> NodePort $JAEGER_NODEPORT"
echo "[INFO] Frontend API port 12349 -> NodePort $FRONTEND_NODEPORT"
echo ""

# echo "==== Open in your laptop browser (two options) ===="
# echo ""
# echo "Option A: SSH tunnel (recommended)"
# echo "- Jaeger:   ssh -L 16686:localhost:$JAEGER_NODEPORT $SSH_USER@$NODE_HOST -N"
# echo "  Then open: http://localhost:16686"
# echo "- Frontend: ssh -L 12349:localhost:$FRONTEND_NODEPORT $SSH_USER@$NODE_HOST -N"
# echo "  Then open:"
# echo "    • http://localhost:12349/ListItems?pageSize=100&pageNum=1"
# echo "    • http://localhost:12349/LoadCatalogue"
# echo ""
# echo "Option B: Direct NodePort (if reachable)"
echo "- Jaeger:   http://$NODE_HOST:$JAEGER_NODEPORT"
echo "- Frontend:"
echo "    • http://$NODE_HOST:$FRONTEND_NODEPORT/ListItems?pageSize=100&pageNum=1"
echo "    • http://$NODE_HOST:$FRONTEND_NODEPORT/LoadCatalogue"
echo ""
# echo "[Note] You can override SSH user and node host:"
# echo "       SSH_USER=youruser NODE_HOST=your.host ./access-frontend-laptop.sh"
