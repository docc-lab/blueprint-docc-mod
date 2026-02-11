#!/bin/bash

WORKSPACE_NAME="usermention_service_v6_ctr"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${OTELCOL_V6_DIAL_ADDR+x}" ]; then
		echo "    OTELCOL_V6_DIAL_ADDR (missing)"
	else
		echo "    OTELCOL_V6_DIAL_ADDR=$OTELCOL_V6_DIAL_ADDR"
	fi
	if [ -z "${USER_CACHE_V6_DIAL_ADDR+x}" ]; then
		echo "    USER_CACHE_V6_DIAL_ADDR (missing)"
	else
		echo "    USER_CACHE_V6_DIAL_ADDR=$USER_CACHE_V6_DIAL_ADDR"
	fi
	if [ -z "${USER_DB_V6_DIAL_ADDR+x}" ]; then
		echo "    USER_DB_V6_DIAL_ADDR (missing)"
	else
		echo "    USER_DB_V6_DIAL_ADDR=$USER_DB_V6_DIAL_ADDR"
	fi
	if [ -z "${USERMENTION_SERVICE_V6_GRPC_BIND_ADDR+x}" ]; then
		echo "    USERMENTION_SERVICE_V6_GRPC_BIND_ADDR (missing)"
	else
		echo "    USERMENTION_SERVICE_V6_GRPC_BIND_ADDR=$USERMENTION_SERVICE_V6_GRPC_BIND_ADDR"
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


usermention_service_v6_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${USER_CACHE_V6_DIAL_ADDR+x}" ]; then
		if ! user_cache_v6_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${USER_DB_V6_DIAL_ADDR+x}" ]; then
		if ! user_db_v6_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${OTELCOL_V6_DIAL_ADDR+x}" ]; then
		if ! otelcol_v6_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${USERMENTION_SERVICE_V6_GRPC_BIND_ADDR+x}" ]; then
		if ! usermention_service_v6_grpc_bind_addr; then
			return $?
		fi
	fi

	run_usermention_service_v6_proc() {
		
        cd usermention_service_v6_proc
        numactl --membind=0 ./usermention_service_v6_proc --user_cache_v6.dial_addr=$USER_CACHE_V6_DIAL_ADDR --user_db_v6.dial_addr=$USER_DB_V6_DIAL_ADDR --otelcol_v6.dial_addr=$OTELCOL_V6_DIAL_ADDR --usermention_service_v6.grpc.bind_addr=$USERMENTION_SERVICE_V6_GRPC_BIND_ADDR &
        USERMENTION_SERVICE_V6_PROC=$!
        return $?

	}

	if run_usermention_service_v6_proc; then
		if [ -z "${USERMENTION_SERVICE_V6_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting usermention_service_v6_proc: function usermention_service_v6_proc did not set USERMENTION_SERVICE_V6_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started usermention_service_v6_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting usermention_service_v6_proc due to exitcode ${exitcode} from usermention_service_v6_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running usermention_service_v6_ctr"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${OTELCOL_V6_DIAL_ADDR+x}" ]; then
		echo "  OTELCOL_V6_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  OTELCOL_V6_DIAL_ADDR=$OTELCOL_V6_DIAL_ADDR"
	fi
	
	if [ -z "${USER_CACHE_V6_DIAL_ADDR+x}" ]; then
		echo "  USER_CACHE_V6_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USER_CACHE_V6_DIAL_ADDR=$USER_CACHE_V6_DIAL_ADDR"
	fi
	
	if [ -z "${USER_DB_V6_DIAL_ADDR+x}" ]; then
		echo "  USER_DB_V6_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USER_DB_V6_DIAL_ADDR=$USER_DB_V6_DIAL_ADDR"
	fi
	
	if [ -z "${USERMENTION_SERVICE_V6_GRPC_BIND_ADDR+x}" ]; then
		echo "  USERMENTION_SERVICE_V6_GRPC_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USERMENTION_SERVICE_V6_GRPC_BIND_ADDR=$USERMENTION_SERVICE_V6_GRPC_BIND_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	usermention_service_v6_proc
	
	wait
}

run_all
