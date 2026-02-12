TARGET_BUILD=$1
LOOP_COUNT=$2

THIS_DIR=$(pwd)

echo "Running $LOOP_COUNT loops of $TARGET_BUILD"

echo
echo "=================================================="
echo "=================================================="
echo "=================================================="
echo

for iternum in $(seq 0 $LOOP_COUNT); do
    echo "Loop $iternum"
    echo "--------------------------------------------------"
    echo "--------------------------------------------------"
    echo

    sleep 120
    cd /users/tomislav/blueprint-docc-mod/examples/dsb_sn
    kubectl delete -f build_$TARGET_BUILD/k8s/ --ignore-not-found=true
    sleep 60
    kubectl apply -f build_$TARGET_BUILD/k8s/
    sleep 120
    kubectl patch service wrk2api-service-$TARGET_BUILD-ctr -p '{"spec":{"type":"NodePort"}}'
    kubectl patch service jaeger-$TARGET_BUILD-ctr -p '{"spec":{"type":"NodePort"}}' || true
    NODEPORT=$(kubectl get services wrk2api-service-$TARGET_BUILD-ctr jaeger-$TARGET_BUILD-ctr -o wide 2>/dev/null | sed -n 's/.*2000:\([0-9]*\)\/TCP.*/\1/p' | head -1)

    echo "Nodeport: $NODEPORT"
    echo "--------------------------------------------------"
    echo

    cd /users/tomislav/DeathStarBench/socialNetwork
    python3 /users/tomislav/blueprint-docc-mod/examples/dsb_sn/scripts/init_social_graph.py --ip 10.10.1.1 --port $NODEPORT
    sleep 60
    RANDOM_SEED=42
    wrk -t 1 -c 5 -d 100s -L -s /users/tomislav/blueprint-docc-mod/examples/dsb_sn/scripts/compose-post.lua http://10.10.1.1:$NODEPORT -R 100
    sleep 60
    N=20
    for i in $(seq 5 $N); do
        rps=$((100*i))
        C=$(((rps*rps+19999)/20000))
        T=$((($C+9)/10))
        printf "\n\n"; echo "$((100*i)):"
        RANDOM_SEED=$((42*$iternum+$T*$i))
        wrk -t $T -c $C -d 60s -L -s /users/tomislav/blueprint-docc-mod/examples/dsb_sn/scripts/compose-post.lua http://10.10.1.1:$NODEPORT -R $rps 2>&1 | grep -A 2 -e "Thread Stats" -e "Mean"
        sleep 30
    done

    echo
    echo
    echo "--------------------------------------------------"
    echo "--------------------------------------------------"
    echo
    echo
    echo
    echo
    echo
done

cd $THIS_DIR