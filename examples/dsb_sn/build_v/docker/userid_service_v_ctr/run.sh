#!/bin/bash

WORKSPACE_NAME="userid_service_v_ctr"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${OTELCOL_V_DIAL_ADDR+x}" ]; then
		echo "    OTELCOL_V_DIAL_ADDR (missing)"
	else
		echo "    OTELCOL_V_DIAL_ADDR=$OTELCOL_V_DIAL_ADDR"
	fi
	if [ -z "${USER_CACHE_V_DIAL_ADDR+x}" ]; then
		echo "    USER_CACHE_V_DIAL_ADDR (missing)"
	else
		echo "    USER_CACHE_V_DIAL_ADDR=$USER_CACHE_V_DIAL_ADDR"
	fi
	if [ -z "${USER_DB_V_DIAL_ADDR+x}" ]; then
		echo "    USER_DB_V_DIAL_ADDR (missing)"
	else
		echo "    USER_DB_V_DIAL_ADDR=$USER_DB_V_DIAL_ADDR"
	fi
	if [ -z "${USERID_SERVICE_V_GRPC_BIND_ADDR+x}" ]; then
		echo "    USERID_SERVICE_V_GRPC_BIND_ADDR (missing)"
	else
		echo "    USERID_SERVICE_V_GRPC_BIND_ADDR=$USERID_SERVICE_V_GRPC_BIND_ADDR"
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


userid_service_v_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${USER_CACHE_V_DIAL_ADDR+x}" ]; then
		if ! user_cache_v_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${USER_DB_V_DIAL_ADDR+x}" ]; then
		if ! user_db_v_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${OTELCOL_V_DIAL_ADDR+x}" ]; then
		if ! otelcol_v_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${USERID_SERVICE_V_GRPC_BIND_ADDR+x}" ]; then
		if ! userid_service_v_grpc_bind_addr; then
			return $?
		fi
	fi

	run_userid_service_v_proc() {
		
        cd userid_service_v_proc
        ./userid_service_v_proc --user_cache_v.dial_addr=$USER_CACHE_V_DIAL_ADDR --user_db_v.dial_addr=$USER_DB_V_DIAL_ADDR --otelcol_v.dial_addr=$OTELCOL_V_DIAL_ADDR --userid_service_v.grpc.bind_addr=$USERID_SERVICE_V_GRPC_BIND_ADDR &
        USERID_SERVICE_V_PROC=$!
        return $?

	}

	if run_userid_service_v_proc; then
		if [ -z "${USERID_SERVICE_V_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting userid_service_v_proc: function userid_service_v_proc did not set USERID_SERVICE_V_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started userid_service_v_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting userid_service_v_proc due to exitcode ${exitcode} from userid_service_v_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running userid_service_v_ctr"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${OTELCOL_V_DIAL_ADDR+x}" ]; then
		echo "  OTELCOL_V_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  OTELCOL_V_DIAL_ADDR=$OTELCOL_V_DIAL_ADDR"
	fi
	
	if [ -z "${USER_CACHE_V_DIAL_ADDR+x}" ]; then
		echo "  USER_CACHE_V_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USER_CACHE_V_DIAL_ADDR=$USER_CACHE_V_DIAL_ADDR"
	fi
	
	if [ -z "${USER_DB_V_DIAL_ADDR+x}" ]; then
		echo "  USER_DB_V_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USER_DB_V_DIAL_ADDR=$USER_DB_V_DIAL_ADDR"
	fi
	
	if [ -z "${USERID_SERVICE_V_GRPC_BIND_ADDR+x}" ]; then
		echo "  USERID_SERVICE_V_GRPC_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USERID_SERVICE_V_GRPC_BIND_ADDR=$USERID_SERVICE_V_GRPC_BIND_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	userid_service_v_proc
	
	wait
}

run_all
