#!/bin/bash

WORKSPACE_NAME="usertimeline_service_v8_ctr"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${OTELCOL_V8_DIAL_ADDR+x}" ]; then
		echo "    OTELCOL_V8_DIAL_ADDR (missing)"
	else
		echo "    OTELCOL_V8_DIAL_ADDR=$OTELCOL_V8_DIAL_ADDR"
	fi
	if [ -z "${POST_STORAGE_SERVICE_V8_GRPC_DIAL_ADDR+x}" ]; then
		echo "    POST_STORAGE_SERVICE_V8_GRPC_DIAL_ADDR (missing)"
	else
		echo "    POST_STORAGE_SERVICE_V8_GRPC_DIAL_ADDR=$POST_STORAGE_SERVICE_V8_GRPC_DIAL_ADDR"
	fi
	if [ -z "${USERTIMELINE_CACHE_V8_DIAL_ADDR+x}" ]; then
		echo "    USERTIMELINE_CACHE_V8_DIAL_ADDR (missing)"
	else
		echo "    USERTIMELINE_CACHE_V8_DIAL_ADDR=$USERTIMELINE_CACHE_V8_DIAL_ADDR"
	fi
	if [ -z "${USERTIMELINE_DB_V8_DIAL_ADDR+x}" ]; then
		echo "    USERTIMELINE_DB_V8_DIAL_ADDR (missing)"
	else
		echo "    USERTIMELINE_DB_V8_DIAL_ADDR=$USERTIMELINE_DB_V8_DIAL_ADDR"
	fi
	if [ -z "${USERTIMELINE_SERVICE_V8_GRPC_BIND_ADDR+x}" ]; then
		echo "    USERTIMELINE_SERVICE_V8_GRPC_BIND_ADDR (missing)"
	else
		echo "    USERTIMELINE_SERVICE_V8_GRPC_BIND_ADDR=$USERTIMELINE_SERVICE_V8_GRPC_BIND_ADDR"
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


usertimeline_service_v8_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${USERTIMELINE_CACHE_V8_DIAL_ADDR+x}" ]; then
		if ! usertimeline_cache_v8_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${USERTIMELINE_DB_V8_DIAL_ADDR+x}" ]; then
		if ! usertimeline_db_v8_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${POST_STORAGE_SERVICE_V8_GRPC_DIAL_ADDR+x}" ]; then
		if ! post_storage_service_v8_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${OTELCOL_V8_DIAL_ADDR+x}" ]; then
		if ! otelcol_v8_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${USERTIMELINE_SERVICE_V8_GRPC_BIND_ADDR+x}" ]; then
		if ! usertimeline_service_v8_grpc_bind_addr; then
			return $?
		fi
	fi

	run_usertimeline_service_v8_proc() {
		export GC_INTERVAL_SEC=${GC_INTERVAL_SEC:-0.1}
		export GOGC=off
        cd usertimeline_service_v8_proc
        ./usertimeline_service_v8_proc --usertimeline_cache_v8.dial_addr=$USERTIMELINE_CACHE_V8_DIAL_ADDR --usertimeline_db_v8.dial_addr=$USERTIMELINE_DB_V8_DIAL_ADDR --post_storage_service_v8.grpc.dial_addr=$POST_STORAGE_SERVICE_V8_GRPC_DIAL_ADDR --otelcol_v8.dial_addr=$OTELCOL_V8_DIAL_ADDR --usertimeline_service_v8.grpc.bind_addr=$USERTIMELINE_SERVICE_V8_GRPC_BIND_ADDR &
        USERTIMELINE_SERVICE_V8_PROC=$!
        return $?

	}

	if run_usertimeline_service_v8_proc; then
		if [ -z "${USERTIMELINE_SERVICE_V8_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting usertimeline_service_v8_proc: function usertimeline_service_v8_proc did not set USERTIMELINE_SERVICE_V8_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started usertimeline_service_v8_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting usertimeline_service_v8_proc due to exitcode ${exitcode} from usertimeline_service_v8_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running usertimeline_service_v8_ctr"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${OTELCOL_V8_DIAL_ADDR+x}" ]; then
		echo "  OTELCOL_V8_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  OTELCOL_V8_DIAL_ADDR=$OTELCOL_V8_DIAL_ADDR"
	fi
	
	if [ -z "${POST_STORAGE_SERVICE_V8_GRPC_DIAL_ADDR+x}" ]; then
		echo "  POST_STORAGE_SERVICE_V8_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  POST_STORAGE_SERVICE_V8_GRPC_DIAL_ADDR=$POST_STORAGE_SERVICE_V8_GRPC_DIAL_ADDR"
	fi
	
	if [ -z "${USERTIMELINE_CACHE_V8_DIAL_ADDR+x}" ]; then
		echo "  USERTIMELINE_CACHE_V8_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USERTIMELINE_CACHE_V8_DIAL_ADDR=$USERTIMELINE_CACHE_V8_DIAL_ADDR"
	fi
	
	if [ -z "${USERTIMELINE_DB_V8_DIAL_ADDR+x}" ]; then
		echo "  USERTIMELINE_DB_V8_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USERTIMELINE_DB_V8_DIAL_ADDR=$USERTIMELINE_DB_V8_DIAL_ADDR"
	fi
	
	if [ -z "${USERTIMELINE_SERVICE_V8_GRPC_BIND_ADDR+x}" ]; then
		echo "  USERTIMELINE_SERVICE_V8_GRPC_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USERTIMELINE_SERVICE_V8_GRPC_BIND_ADDR=$USERTIMELINE_SERVICE_V8_GRPC_BIND_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	usertimeline_service_v8_proc
	
	wait
}

run_all
