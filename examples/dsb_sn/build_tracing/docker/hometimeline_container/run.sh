#!/bin/bash

WORKSPACE_NAME="hometimeline_container"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${HOMETIMELINE_CACHE_DIAL_ADDR+x}" ]; then
		echo "    HOMETIMELINE_CACHE_DIAL_ADDR (missing)"
	else
		echo "    HOMETIMELINE_CACHE_DIAL_ADDR=$HOMETIMELINE_CACHE_DIAL_ADDR"
	fi
	if [ -z "${HOMETIMELINE_SERVICE_GRPC_BIND_ADDR+x}" ]; then
		echo "    HOMETIMELINE_SERVICE_GRPC_BIND_ADDR (missing)"
	else
		echo "    HOMETIMELINE_SERVICE_GRPC_BIND_ADDR=$HOMETIMELINE_SERVICE_GRPC_BIND_ADDR"
	fi
	if [ -z "${OTEL_COLLECTOR_DIAL_ADDR+x}" ]; then
		echo "    OTEL_COLLECTOR_DIAL_ADDR (missing)"
	else
		echo "    OTEL_COLLECTOR_DIAL_ADDR=$OTEL_COLLECTOR_DIAL_ADDR"
	fi
	if [ -z "${POST_STORAGE_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		echo "    POST_STORAGE_SERVICE_GRPC_DIAL_ADDR (missing)"
	else
		echo "    POST_STORAGE_SERVICE_GRPC_DIAL_ADDR=$POST_STORAGE_SERVICE_GRPC_DIAL_ADDR"
	fi
	if [ -z "${SOCIALGRAPH_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		echo "    SOCIALGRAPH_SERVICE_GRPC_DIAL_ADDR (missing)"
	else
		echo "    SOCIALGRAPH_SERVICE_GRPC_DIAL_ADDR=$SOCIALGRAPH_SERVICE_GRPC_DIAL_ADDR"
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


hometimeline_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${HOMETIMELINE_CACHE_DIAL_ADDR+x}" ]; then
		if ! hometimeline_cache_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${POST_STORAGE_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		if ! post_storage_service_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${OTEL_COLLECTOR_DIAL_ADDR+x}" ]; then
		if ! otel_collector_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${SOCIALGRAPH_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		if ! socialgraph_service_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${HOMETIMELINE_SERVICE_GRPC_BIND_ADDR+x}" ]; then
		if ! hometimeline_service_grpc_bind_addr; then
			return $?
		fi
	fi

	run_hometimeline_proc() {
		
        cd hometimeline_proc
        ./hometimeline_proc --hometimeline_cache.dial_addr=$HOMETIMELINE_CACHE_DIAL_ADDR --post_storage_service.grpc.dial_addr=$POST_STORAGE_SERVICE_GRPC_DIAL_ADDR --otel_collector.dial_addr=$OTEL_COLLECTOR_DIAL_ADDR --socialgraph_service.grpc.dial_addr=$SOCIALGRAPH_SERVICE_GRPC_DIAL_ADDR --hometimeline_service.grpc.bind_addr=$HOMETIMELINE_SERVICE_GRPC_BIND_ADDR &
        HOMETIMELINE_PROC=$!
        return $?

	}

	if run_hometimeline_proc; then
		if [ -z "${HOMETIMELINE_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting hometimeline_proc: function hometimeline_proc did not set HOMETIMELINE_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started hometimeline_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting hometimeline_proc due to exitcode ${exitcode} from hometimeline_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running hometimeline_container"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${HOMETIMELINE_CACHE_DIAL_ADDR+x}" ]; then
		echo "  HOMETIMELINE_CACHE_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  HOMETIMELINE_CACHE_DIAL_ADDR=$HOMETIMELINE_CACHE_DIAL_ADDR"
	fi
	
	if [ -z "${HOMETIMELINE_SERVICE_GRPC_BIND_ADDR+x}" ]; then
		echo "  HOMETIMELINE_SERVICE_GRPC_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  HOMETIMELINE_SERVICE_GRPC_BIND_ADDR=$HOMETIMELINE_SERVICE_GRPC_BIND_ADDR"
	fi
	
	if [ -z "${OTEL_COLLECTOR_DIAL_ADDR+x}" ]; then
		echo "  OTEL_COLLECTOR_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  OTEL_COLLECTOR_DIAL_ADDR=$OTEL_COLLECTOR_DIAL_ADDR"
	fi
	
	if [ -z "${POST_STORAGE_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		echo "  POST_STORAGE_SERVICE_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  POST_STORAGE_SERVICE_GRPC_DIAL_ADDR=$POST_STORAGE_SERVICE_GRPC_DIAL_ADDR"
	fi
	
	if [ -z "${SOCIALGRAPH_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		echo "  SOCIALGRAPH_SERVICE_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  SOCIALGRAPH_SERVICE_GRPC_DIAL_ADDR=$SOCIALGRAPH_SERVICE_GRPC_DIAL_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	hometimeline_proc
	
	wait
}

run_all
