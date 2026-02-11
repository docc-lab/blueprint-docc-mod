#!/bin/bash

WORKSPACE_NAME="user_service_v5_ctr"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${OTELCOL_V5_DIAL_ADDR+x}" ]; then
		echo "    OTELCOL_V5_DIAL_ADDR (missing)"
	else
		echo "    OTELCOL_V5_DIAL_ADDR=$OTELCOL_V5_DIAL_ADDR"
	fi
	if [ -z "${SOCIALGRAPH_SERVICE_V5_GRPC_DIAL_ADDR+x}" ]; then
		echo "    SOCIALGRAPH_SERVICE_V5_GRPC_DIAL_ADDR (missing)"
	else
		echo "    SOCIALGRAPH_SERVICE_V5_GRPC_DIAL_ADDR=$SOCIALGRAPH_SERVICE_V5_GRPC_DIAL_ADDR"
	fi
	if [ -z "${USER_CACHE_V5_DIAL_ADDR+x}" ]; then
		echo "    USER_CACHE_V5_DIAL_ADDR (missing)"
	else
		echo "    USER_CACHE_V5_DIAL_ADDR=$USER_CACHE_V5_DIAL_ADDR"
	fi
	if [ -z "${USER_DB_V5_DIAL_ADDR+x}" ]; then
		echo "    USER_DB_V5_DIAL_ADDR (missing)"
	else
		echo "    USER_DB_V5_DIAL_ADDR=$USER_DB_V5_DIAL_ADDR"
	fi
	if [ -z "${USER_SERVICE_V5_GRPC_BIND_ADDR+x}" ]; then
		echo "    USER_SERVICE_V5_GRPC_BIND_ADDR (missing)"
	else
		echo "    USER_SERVICE_V5_GRPC_BIND_ADDR=$USER_SERVICE_V5_GRPC_BIND_ADDR"
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


user_service_v5_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${USER_CACHE_V5_DIAL_ADDR+x}" ]; then
		if ! user_cache_v5_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${USER_DB_V5_DIAL_ADDR+x}" ]; then
		if ! user_db_v5_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${SOCIALGRAPH_SERVICE_V5_GRPC_DIAL_ADDR+x}" ]; then
		if ! socialgraph_service_v5_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${OTELCOL_V5_DIAL_ADDR+x}" ]; then
		if ! otelcol_v5_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${USER_SERVICE_V5_GRPC_BIND_ADDR+x}" ]; then
		if ! user_service_v5_grpc_bind_addr; then
			return $?
		fi
	fi

	run_user_service_v5_proc() {
		
        cd user_service_v5_proc
        ./user_service_v5_proc --user_cache_v5.dial_addr=$USER_CACHE_V5_DIAL_ADDR --user_db_v5.dial_addr=$USER_DB_V5_DIAL_ADDR --socialgraph_service_v5.grpc.dial_addr=$SOCIALGRAPH_SERVICE_V5_GRPC_DIAL_ADDR --otelcol_v5.dial_addr=$OTELCOL_V5_DIAL_ADDR --user_service_v5.grpc.bind_addr=$USER_SERVICE_V5_GRPC_BIND_ADDR &
        USER_SERVICE_V5_PROC=$!
        return $?

	}

	if run_user_service_v5_proc; then
		if [ -z "${USER_SERVICE_V5_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting user_service_v5_proc: function user_service_v5_proc did not set USER_SERVICE_V5_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started user_service_v5_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting user_service_v5_proc due to exitcode ${exitcode} from user_service_v5_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running user_service_v5_ctr"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${OTELCOL_V5_DIAL_ADDR+x}" ]; then
		echo "  OTELCOL_V5_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  OTELCOL_V5_DIAL_ADDR=$OTELCOL_V5_DIAL_ADDR"
	fi
	
	if [ -z "${SOCIALGRAPH_SERVICE_V5_GRPC_DIAL_ADDR+x}" ]; then
		echo "  SOCIALGRAPH_SERVICE_V5_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  SOCIALGRAPH_SERVICE_V5_GRPC_DIAL_ADDR=$SOCIALGRAPH_SERVICE_V5_GRPC_DIAL_ADDR"
	fi
	
	if [ -z "${USER_CACHE_V5_DIAL_ADDR+x}" ]; then
		echo "  USER_CACHE_V5_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USER_CACHE_V5_DIAL_ADDR=$USER_CACHE_V5_DIAL_ADDR"
	fi
	
	if [ -z "${USER_DB_V5_DIAL_ADDR+x}" ]; then
		echo "  USER_DB_V5_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USER_DB_V5_DIAL_ADDR=$USER_DB_V5_DIAL_ADDR"
	fi
	
	if [ -z "${USER_SERVICE_V5_GRPC_BIND_ADDR+x}" ]; then
		echo "  USER_SERVICE_V5_GRPC_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USER_SERVICE_V5_GRPC_BIND_ADDR=$USER_SERVICE_V5_GRPC_BIND_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	user_service_v5_proc
	
	wait
}

run_all
