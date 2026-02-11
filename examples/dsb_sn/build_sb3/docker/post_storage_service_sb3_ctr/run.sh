#!/bin/bash

WORKSPACE_NAME="post_storage_service_sb3_ctr"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${OTELCOL_SB3_DIAL_ADDR+x}" ]; then
		echo "    OTELCOL_SB3_DIAL_ADDR (missing)"
	else
		echo "    OTELCOL_SB3_DIAL_ADDR=$OTELCOL_SB3_DIAL_ADDR"
	fi
	if [ -z "${POST_CACHE_SB3_DIAL_ADDR+x}" ]; then
		echo "    POST_CACHE_SB3_DIAL_ADDR (missing)"
	else
		echo "    POST_CACHE_SB3_DIAL_ADDR=$POST_CACHE_SB3_DIAL_ADDR"
	fi
	if [ -z "${POST_DB_SB3_DIAL_ADDR+x}" ]; then
		echo "    POST_DB_SB3_DIAL_ADDR (missing)"
	else
		echo "    POST_DB_SB3_DIAL_ADDR=$POST_DB_SB3_DIAL_ADDR"
	fi
	if [ -z "${POST_STORAGE_SERVICE_SB3_GRPC_BIND_ADDR+x}" ]; then
		echo "    POST_STORAGE_SERVICE_SB3_GRPC_BIND_ADDR (missing)"
	else
		echo "    POST_STORAGE_SERVICE_SB3_GRPC_BIND_ADDR=$POST_STORAGE_SERVICE_SB3_GRPC_BIND_ADDR"
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


post_storage_service_sb3_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${POST_CACHE_SB3_DIAL_ADDR+x}" ]; then
		if ! post_cache_sb3_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${POST_DB_SB3_DIAL_ADDR+x}" ]; then
		if ! post_db_sb3_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${OTELCOL_SB3_DIAL_ADDR+x}" ]; then
		if ! otelcol_sb3_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${POST_STORAGE_SERVICE_SB3_GRPC_BIND_ADDR+x}" ]; then
		if ! post_storage_service_sb3_grpc_bind_addr; then
			return $?
		fi
	fi

	run_post_storage_service_sb3_proc() {
		
        cd post_storage_service_sb3_proc
        numactl --membind=0 ./post_storage_service_sb3_proc --post_cache_sb3.dial_addr=$POST_CACHE_SB3_DIAL_ADDR --post_db_sb3.dial_addr=$POST_DB_SB3_DIAL_ADDR --otelcol_sb3.dial_addr=$OTELCOL_SB3_DIAL_ADDR --post_storage_service_sb3.grpc.bind_addr=$POST_STORAGE_SERVICE_SB3_GRPC_BIND_ADDR &
        POST_STORAGE_SERVICE_SB3_PROC=$!
        return $?

	}

	if run_post_storage_service_sb3_proc; then
		if [ -z "${POST_STORAGE_SERVICE_SB3_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting post_storage_service_sb3_proc: function post_storage_service_sb3_proc did not set POST_STORAGE_SERVICE_SB3_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started post_storage_service_sb3_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting post_storage_service_sb3_proc due to exitcode ${exitcode} from post_storage_service_sb3_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running post_storage_service_sb3_ctr"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${OTELCOL_SB3_DIAL_ADDR+x}" ]; then
		echo "  OTELCOL_SB3_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  OTELCOL_SB3_DIAL_ADDR=$OTELCOL_SB3_DIAL_ADDR"
	fi
	
	if [ -z "${POST_CACHE_SB3_DIAL_ADDR+x}" ]; then
		echo "  POST_CACHE_SB3_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  POST_CACHE_SB3_DIAL_ADDR=$POST_CACHE_SB3_DIAL_ADDR"
	fi
	
	if [ -z "${POST_DB_SB3_DIAL_ADDR+x}" ]; then
		echo "  POST_DB_SB3_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  POST_DB_SB3_DIAL_ADDR=$POST_DB_SB3_DIAL_ADDR"
	fi
	
	if [ -z "${POST_STORAGE_SERVICE_SB3_GRPC_BIND_ADDR+x}" ]; then
		echo "  POST_STORAGE_SERVICE_SB3_GRPC_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  POST_STORAGE_SERVICE_SB3_GRPC_BIND_ADDR=$POST_STORAGE_SERVICE_SB3_GRPC_BIND_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	post_storage_service_sb3_proc
	
	wait
}

run_all
