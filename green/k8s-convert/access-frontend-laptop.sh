#!/bin/bash

set -e

# Helper: require kubectl
if ! command -v kubectl >/dev/null 2>&1; then
  echo "[ERROR] kubectl not found in PATH"
  exit 1
fi

echo "==== Discovering Services ===="

# Check if services exist (suppress output, only check exit code)
JAEGER_EXISTS=$(kubectl get svc jaeger-ctr >/dev/null 2>&1 && echo "yes" || echo "no")
FRONTEND_EXISTS=$(kubectl get svc frontend-ctr >/dev/null 2>&1 && echo "yes" || echo "no")

if [ "$FRONTEND_EXISTS" = "no" ]; then
  echo "[ERROR] frontend-ctr service not found"
  exit 1
fi

# Get service ports dynamically (find the main port, not hardcode targetPort)
# For frontend: find port that targets 2000, or use first port if not found
FRONTEND_PORT=$(kubectl get svc frontend-ctr -o jsonpath='{.spec.ports[?(@.targetPort==2000)].port}' 2>/dev/null | head -1)
if [ -z "$FRONTEND_PORT" ]; then
  # Fallback: use first port
  FRONTEND_PORT=$(kubectl get svc frontend-ctr -o jsonpath='{.spec.ports[0].port}' 2>/dev/null)
fi
FRONTEND_TARGET_PORT=$(kubectl get svc frontend-ctr -o jsonpath='{.spec.ports[?(@.targetPort==2000)].targetPort}' 2>/dev/null | head -1 || echo "2000")

# For jaeger: find port that targets 16686 (UI), or use first port if not found
if [ "$JAEGER_EXISTS" = "yes" ]; then
  JAEGER_UI_PORT=$(kubectl get svc jaeger-ctr -o jsonpath='{.spec.ports[?(@.targetPort==16686)].port}' 2>/dev/null | head -1)
  if [ -z "$JAEGER_UI_PORT" ]; then
    # Fallback: use first port
    JAEGER_UI_PORT=$(kubectl get svc jaeger-ctr -o jsonpath='{.spec.ports[0].port}' 2>/dev/null)
  fi
  JAEGER_UI_TARGET_PORT=$(kubectl get svc jaeger-ctr -o jsonpath='{.spec.ports[?(@.targetPort==16686)].targetPort}' 2>/dev/null | head -1 || echo "16686")
fi

# Get service type
JAEGER_TYPE=$(kubectl get svc jaeger-ctr -o jsonpath='{.spec.type}' 2>/dev/null || echo "")
FRONTEND_TYPE=$(kubectl get svc frontend-ctr -o jsonpath='{.spec.type}' 2>/dev/null || echo "")

echo "[INFO] Frontend service found: port $FRONTEND_PORT (type: $FRONTEND_TYPE)"
if [ "$JAEGER_EXISTS" = "yes" ]; then
  echo "[INFO] Jaeger service found: UI port $JAEGER_UI_PORT (type: $JAEGER_TYPE)"
else
  echo "[INFO] Jaeger service not found (skipping)"
fi
echo ""

# Convert to NodePort if not already
if [ "$FRONTEND_TYPE" != "NodePort" ]; then
  echo "[INFO] Converting frontend-ctr service to NodePort..."
  kubectl patch service frontend-ctr -p '{"spec":{"type":"NodePort"}}'
  FRONTEND_TYPE="NodePort"
  # Wait a moment for the change to propagate
  sleep 1
fi

if [ "$JAEGER_EXISTS" = "yes" ] && [ "$JAEGER_TYPE" != "NodePort" ]; then
  echo "[INFO] Converting jaeger-ctr service to NodePort..."
  kubectl patch service jaeger-ctr -p '{"spec":{"type":"NodePort"}}'
  JAEGER_TYPE="NodePort"
  # Wait a moment for the change to propagate
  sleep 1
fi

echo ""

# Get node IPs for direct access (if NodePort)
NODE_IPS=$(kubectl get nodes -o jsonpath='{range .items[*]}{.status.addresses[?(@.type=="InternalIP")].address}{"\n"}{end}' | head -1)

# Get public hostname automatically (try hostname -f, fallback to env var or default)
# Can be overridden via NODE_HOSTNAME env var
if [ -z "$NODE_HOSTNAME" ]; then
  NODE_HOSTNAME=$(hostname -f 2>/dev/null || hostname 2>/dev/null || echo "")
  if [ -z "$NODE_HOSTNAME" ] || [ "$NODE_HOSTNAME" = "localhost" ] || [ "$NODE_HOSTNAME" = "localhost.localdomain" ]; then
    # Fallback to default if hostname is not useful
    NODE_HOSTNAME="c220g2-011309.wisc.cloudlab.us"
  fi
fi

echo "==== Access URLs ===="
echo "[INFO] Using hostname: $NODE_HOSTNAME (override with NODE_HOSTNAME env var)"
echo ""

# Get NodePorts (refresh after potential conversion)
if [ "$FRONTEND_TYPE" = "NodePort" ]; then
  # Get NodePort for the port that targets the frontend targetPort
  FRONTEND_NODEPORT=$(kubectl get svc frontend-ctr -o jsonpath="{.spec.ports[?(@.targetPort==$FRONTEND_TARGET_PORT)].nodePort}" 2>/dev/null | awk '{print $1}')
  if [ -z "$FRONTEND_NODEPORT" ]; then
    # Fallback: get NodePort for the service port we found
    FRONTEND_NODEPORT=$(kubectl get svc frontend-ctr -o jsonpath="{.spec.ports[?(@.port==$FRONTEND_PORT)].nodePort}" 2>/dev/null | awk '{print $1}')
  fi
  if [ -z "$FRONTEND_NODEPORT" ]; then
    # Final fallback: get first NodePort
    FRONTEND_NODEPORT=$(kubectl get svc frontend-ctr -o jsonpath='{.spec.ports[0].nodePort}' 2>/dev/null)
  fi
  echo "Frontend (NodePort: $FRONTEND_NODEPORT):"
  echo ""
  echo "  Public URL (if accessible):"
  echo "    • http://$NODE_HOSTNAME:$FRONTEND_NODEPORT"
  echo "    • http://$NODE_HOSTNAME:$FRONTEND_NODEPORT/ListItems?pageSize=100&pageNum=1"
  echo "    • http://$NODE_HOSTNAME:$FRONTEND_NODEPORT/LoadCatalogue"
  echo ""
  echo "  Internal URL (from within cluster):"
  echo "    • http://$NODE_IPS:$FRONTEND_NODEPORT"
  echo "    • http://$NODE_IPS:$FRONTEND_NODEPORT/ListItems?pageSize=100&pageNum=1"
  echo "    • http://$NODE_IPS:$FRONTEND_NODEPORT/LoadCatalogue"
  echo ""
else
  # Use a dynamic local port based on service port to avoid conflicts
  FRONTEND_LOCAL_PORT=${FRONTEND_PORT:-8080}
  echo "Frontend (ClusterIP - use port-forward):"
  echo "  Run: kubectl port-forward svc/frontend-ctr $FRONTEND_LOCAL_PORT:$FRONTEND_PORT"
  echo "  Then access:"
  echo "    • http://localhost:$FRONTEND_LOCAL_PORT/ListItems?pageSize=100&pageNum=1"
  echo "    • http://localhost:$FRONTEND_LOCAL_PORT/LoadCatalogue"
  echo ""
fi

if [ "$JAEGER_EXISTS" = "yes" ]; then
  if [ "$JAEGER_TYPE" = "NodePort" ]; then
    # Get NodePort for the port that targets the jaeger UI targetPort
    JAEGER_NODEPORT=$(kubectl get svc jaeger-ctr -o jsonpath="{.spec.ports[?(@.targetPort==$JAEGER_UI_TARGET_PORT)].nodePort}" 2>/dev/null | awk '{print $1}')
    if [ -z "$JAEGER_NODEPORT" ]; then
      # Fallback: get NodePort for the service port we found
      JAEGER_NODEPORT=$(kubectl get svc jaeger-ctr -o jsonpath="{.spec.ports[?(@.port==$JAEGER_UI_PORT)].nodePort}" 2>/dev/null | awk '{print $1}')
    fi
    if [ -z "$JAEGER_NODEPORT" ]; then
      # Final fallback: get first NodePort
      JAEGER_NODEPORT=$(kubectl get svc jaeger-ctr -o jsonpath='{.spec.ports[0].nodePort}' 2>/dev/null)
    fi
    echo "Jaeger UI (NodePort: $JAEGER_NODEPORT):"
    echo ""
    echo "  Public URL (if accessible):"
    echo "    • http://$NODE_HOSTNAME:$JAEGER_NODEPORT"
    echo ""
    echo "  Internal URL (from within cluster):"
    echo "    • http://$NODE_IPS:$JAEGER_NODEPORT"
    echo ""
  else
    # Use the target port as local port for jaeger (standard UI port)
    JAEGER_LOCAL_PORT=${JAEGER_UI_TARGET_PORT:-16686}
    echo "Jaeger UI (ClusterIP - use port-forward):"
    echo "  Run: kubectl port-forward svc/jaeger-ctr $JAEGER_LOCAL_PORT:$JAEGER_UI_PORT"
    echo "  Then access: http://localhost:$JAEGER_LOCAL_PORT"
    echo ""
  fi
fi
