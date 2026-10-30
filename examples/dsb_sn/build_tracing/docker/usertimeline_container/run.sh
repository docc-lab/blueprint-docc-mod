#!/bin/bash

WORKSPACE_NAME="usertimeline_container"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
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
	if [ -z "${USERTIMELINE_CACHE_DIAL_ADDR+x}" ]; then
		echo "    USERTIMELINE_CACHE_DIAL_ADDR (missing)"
	else
		echo "    USERTIMELINE_CACHE_DIAL_ADDR=$USERTIMELINE_CACHE_DIAL_ADDR"
	fi
	if [ -z "${USERTIMELINE_DB_DIAL_ADDR+x}" ]; then
		echo "    USERTIMELINE_DB_DIAL_ADDR (missing)"
	else
		echo "    USERTIMELINE_DB_DIAL_ADDR=$USERTIMELINE_DB_DIAL_ADDR"
	fi
	if [ -z "${USERTIMELINE_SERVICE_GRPC_BIND_ADDR+x}" ]; then
		echo "    USERTIMELINE_SERVICE_GRPC_BIND_ADDR (missing)"
	else
		echo "    USERTIMELINE_SERVICE_GRPC_BIND_ADDR=$USERTIMELINE_SERVICE_GRPC_BIND_ADDR"
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


usertimeline_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${USERTIMELINE_CACHE_DIAL_ADDR+x}" ]; then
		if ! usertimeline_cache_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${USERTIMELINE_DB_DIAL_ADDR+x}" ]; then
		if ! usertimeline_db_dial_addr; then
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

	if [ -z "${USERTIMELINE_SERVICE_GRPC_BIND_ADDR+x}" ]; then
		if ! usertimeline_service_grpc_bind_addr; then
			return $?
		fi
	fi

	run_usertimeline_proc() {
		
        cd usertimeline_proc
        ./usertimeline_proc --usertimeline_cache.dial_addr=$USERTIMELINE_CACHE_DIAL_ADDR --usertimeline_db.dial_addr=$USERTIMELINE_DB_DIAL_ADDR --post_storage_service.grpc.dial_addr=$POST_STORAGE_SERVICE_GRPC_DIAL_ADDR --otel_collector.dial_addr=$OTEL_COLLECTOR_DIAL_ADDR --usertimeline_service.grpc.bind_addr=$USERTIMELINE_SERVICE_GRPC_BIND_ADDR &
        USERTIMELINE_PROC=$!
        return $?

	}

	if run_usertimeline_proc; then
		if [ -z "${USERTIMELINE_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting usertimeline_proc: function usertimeline_proc did not set USERTIMELINE_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started usertimeline_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting usertimeline_proc due to exitcode ${exitcode} from usertimeline_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running usertimeline_container"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
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
	
	if [ -z "${USERTIMELINE_CACHE_DIAL_ADDR+x}" ]; then
		echo "  USERTIMELINE_CACHE_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USERTIMELINE_CACHE_DIAL_ADDR=$USERTIMELINE_CACHE_DIAL_ADDR"
	fi
	
	if [ -z "${USERTIMELINE_DB_DIAL_ADDR+x}" ]; then
		echo "  USERTIMELINE_DB_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USERTIMELINE_DB_DIAL_ADDR=$USERTIMELINE_DB_DIAL_ADDR"
	fi
	
	if [ -z "${USERTIMELINE_SERVICE_GRPC_BIND_ADDR+x}" ]; then
		echo "  USERTIMELINE_SERVICE_GRPC_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USERTIMELINE_SERVICE_GRPC_BIND_ADDR=$USERTIMELINE_SERVICE_GRPC_BIND_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	usertimeline_proc
	
	wait
}

run_all
