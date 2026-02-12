#!/bin/bash

WORKSPACE_NAME="hometimeline_service_cgpb_ctr"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${HOMETIMELINE_CACHE_CGPB_DIAL_ADDR+x}" ]; then
		echo "    HOMETIMELINE_CACHE_CGPB_DIAL_ADDR (missing)"
	else
		echo "    HOMETIMELINE_CACHE_CGPB_DIAL_ADDR=$HOMETIMELINE_CACHE_CGPB_DIAL_ADDR"
	fi
	if [ -z "${HOMETIMELINE_SERVICE_CGPB_GRPC_BIND_ADDR+x}" ]; then
		echo "    HOMETIMELINE_SERVICE_CGPB_GRPC_BIND_ADDR (missing)"
	else
		echo "    HOMETIMELINE_SERVICE_CGPB_GRPC_BIND_ADDR=$HOMETIMELINE_SERVICE_CGPB_GRPC_BIND_ADDR"
	fi
	if [ -z "${OTELCOL_CGPB_DIAL_ADDR+x}" ]; then
		echo "    OTELCOL_CGPB_DIAL_ADDR (missing)"
	else
		echo "    OTELCOL_CGPB_DIAL_ADDR=$OTELCOL_CGPB_DIAL_ADDR"
	fi
	if [ -z "${POST_STORAGE_SERVICE_CGPB_GRPC_DIAL_ADDR+x}" ]; then
		echo "    POST_STORAGE_SERVICE_CGPB_GRPC_DIAL_ADDR (missing)"
	else
		echo "    POST_STORAGE_SERVICE_CGPB_GRPC_DIAL_ADDR=$POST_STORAGE_SERVICE_CGPB_GRPC_DIAL_ADDR"
	fi
	if [ -z "${SOCIALGRAPH_SERVICE_CGPB_GRPC_DIAL_ADDR+x}" ]; then
		echo "    SOCIALGRAPH_SERVICE_CGPB_GRPC_DIAL_ADDR (missing)"
	else
		echo "    SOCIALGRAPH_SERVICE_CGPB_GRPC_DIAL_ADDR=$SOCIALGRAPH_SERVICE_CGPB_GRPC_DIAL_ADDR"
	fi
		
	exit 1; 
}

while getopts "h" flag; do
	case $flag in
		*)
		usage
		;;
	esac
done


hometimeline_service_cgpb_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${HOMETIMELINE_CACHE_CGPB_DIAL_ADDR+x}" ]; then
		if ! hometimeline_cache_cgpb_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${POST_STORAGE_SERVICE_CGPB_GRPC_DIAL_ADDR+x}" ]; then
		if ! post_storage_service_cgpb_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${OTELCOL_CGPB_DIAL_ADDR+x}" ]; then
		if ! otelcol_cgpb_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${SOCIALGRAPH_SERVICE_CGPB_GRPC_DIAL_ADDR+x}" ]; then
		if ! socialgraph_service_cgpb_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${HOMETIMELINE_SERVICE_CGPB_GRPC_BIND_ADDR+x}" ]; then
		if ! hometimeline_service_cgpb_grpc_bind_addr; then
			return $?
		fi
	fi

	run_hometimeline_service_cgpb_proc() {
		export GC_INTERVAL_SEC=${GC_INTERVAL_SEC:-0.01}
		export GOGC=off
        cd hometimeline_service_cgpb_proc
        ./hometimeline_service_cgpb_proc --hometimeline_cache_cgpb.dial_addr=$HOMETIMELINE_CACHE_CGPB_DIAL_ADDR --post_storage_service_cgpb.grpc.dial_addr=$POST_STORAGE_SERVICE_CGPB_GRPC_DIAL_ADDR --otelcol_cgpb.dial_addr=$OTELCOL_CGPB_DIAL_ADDR --socialgraph_service_cgpb.grpc.dial_addr=$SOCIALGRAPH_SERVICE_CGPB_GRPC_DIAL_ADDR --hometimeline_service_cgpb.grpc.bind_addr=$HOMETIMELINE_SERVICE_CGPB_GRPC_BIND_ADDR &
        HOMETIMELINE_SERVICE_CGPB_PROC=$!
        return $?

	}

	if run_hometimeline_service_cgpb_proc; then
		if [ -z "${HOMETIMELINE_SERVICE_CGPB_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting hometimeline_service_cgpb_proc: function hometimeline_service_cgpb_proc did not set HOMETIMELINE_SERVICE_CGPB_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started hometimeline_service_cgpb_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting hometimeline_service_cgpb_proc due to exitcode ${exitcode} from hometimeline_service_cgpb_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running hometimeline_service_cgpb_ctr"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${HOMETIMELINE_CACHE_CGPB_DIAL_ADDR+x}" ]; then
		echo "  HOMETIMELINE_CACHE_CGPB_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  HOMETIMELINE_CACHE_CGPB_DIAL_ADDR=$HOMETIMELINE_CACHE_CGPB_DIAL_ADDR"
	fi
	
	if [ -z "${HOMETIMELINE_SERVICE_CGPB_GRPC_BIND_ADDR+x}" ]; then
		echo "  HOMETIMELINE_SERVICE_CGPB_GRPC_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  HOMETIMELINE_SERVICE_CGPB_GRPC_BIND_ADDR=$HOMETIMELINE_SERVICE_CGPB_GRPC_BIND_ADDR"
	fi
	
	if [ -z "${OTELCOL_CGPB_DIAL_ADDR+x}" ]; then
		echo "  OTELCOL_CGPB_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  OTELCOL_CGPB_DIAL_ADDR=$OTELCOL_CGPB_DIAL_ADDR"
	fi
	
	if [ -z "${POST_STORAGE_SERVICE_CGPB_GRPC_DIAL_ADDR+x}" ]; then
		echo "  POST_STORAGE_SERVICE_CGPB_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  POST_STORAGE_SERVICE_CGPB_GRPC_DIAL_ADDR=$POST_STORAGE_SERVICE_CGPB_GRPC_DIAL_ADDR"
	fi
	
	if [ -z "${SOCIALGRAPH_SERVICE_CGPB_GRPC_DIAL_ADDR+x}" ]; then
		echo "  SOCIALGRAPH_SERVICE_CGPB_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  SOCIALGRAPH_SERVICE_CGPB_GRPC_DIAL_ADDR=$SOCIALGRAPH_SERVICE_CGPB_GRPC_DIAL_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	hometimeline_service_cgpb_proc
	
	wait
}

run_all
