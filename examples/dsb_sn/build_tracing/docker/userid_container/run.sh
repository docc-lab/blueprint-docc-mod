#!/bin/bash

WORKSPACE_NAME="userid_container"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${OTEL_COLLECTOR_DIAL_ADDR+x}" ]; then
		echo "    OTEL_COLLECTOR_DIAL_ADDR (missing)"
	else
		echo "    OTEL_COLLECTOR_DIAL_ADDR=$OTEL_COLLECTOR_DIAL_ADDR"
	fi
	if [ -z "${USER_CACHE_DIAL_ADDR+x}" ]; then
		echo "    USER_CACHE_DIAL_ADDR (missing)"
	else
		echo "    USER_CACHE_DIAL_ADDR=$USER_CACHE_DIAL_ADDR"
	fi
	if [ -z "${USER_DB_DIAL_ADDR+x}" ]; then
		echo "    USER_DB_DIAL_ADDR (missing)"
	else
		echo "    USER_DB_DIAL_ADDR=$USER_DB_DIAL_ADDR"
	fi
	if [ -z "${USERID_SERVICE_GRPC_BIND_ADDR+x}" ]; then
		echo "    USERID_SERVICE_GRPC_BIND_ADDR (missing)"
	else
		echo "    USERID_SERVICE_GRPC_BIND_ADDR=$USERID_SERVICE_GRPC_BIND_ADDR"
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


userid_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${USER_CACHE_DIAL_ADDR+x}" ]; then
		if ! user_cache_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${USER_DB_DIAL_ADDR+x}" ]; then
		if ! user_db_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${OTEL_COLLECTOR_DIAL_ADDR+x}" ]; then
		if ! otel_collector_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${USERID_SERVICE_GRPC_BIND_ADDR+x}" ]; then
		if ! userid_service_grpc_bind_addr; then
			return $?
		fi
	fi

	run_userid_proc() {
		
        cd userid_proc
        ./userid_proc --user_cache.dial_addr=$USER_CACHE_DIAL_ADDR --user_db.dial_addr=$USER_DB_DIAL_ADDR --otel_collector.dial_addr=$OTEL_COLLECTOR_DIAL_ADDR --userid_service.grpc.bind_addr=$USERID_SERVICE_GRPC_BIND_ADDR &
        USERID_PROC=$!
        return $?

	}

	if run_userid_proc; then
		if [ -z "${USERID_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting userid_proc: function userid_proc did not set USERID_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started userid_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting userid_proc due to exitcode ${exitcode} from userid_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running userid_container"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${OTEL_COLLECTOR_DIAL_ADDR+x}" ]; then
		echo "  OTEL_COLLECTOR_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  OTEL_COLLECTOR_DIAL_ADDR=$OTEL_COLLECTOR_DIAL_ADDR"
	fi
	
	if [ -z "${USER_CACHE_DIAL_ADDR+x}" ]; then
		echo "  USER_CACHE_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USER_CACHE_DIAL_ADDR=$USER_CACHE_DIAL_ADDR"
	fi
	
	if [ -z "${USER_DB_DIAL_ADDR+x}" ]; then
		echo "  USER_DB_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USER_DB_DIAL_ADDR=$USER_DB_DIAL_ADDR"
	fi
	
	if [ -z "${USERID_SERVICE_GRPC_BIND_ADDR+x}" ]; then
		echo "  USERID_SERVICE_GRPC_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USERID_SERVICE_GRPC_BIND_ADDR=$USERID_SERVICE_GRPC_BIND_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	userid_proc
	
	wait
}

run_all
