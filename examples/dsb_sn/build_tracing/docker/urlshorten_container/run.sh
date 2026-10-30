#!/bin/bash

WORKSPACE_NAME="urlshorten_container"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${OTEL_COLLECTOR_DIAL_ADDR+x}" ]; then
		echo "    OTEL_COLLECTOR_DIAL_ADDR (missing)"
	else
		echo "    OTEL_COLLECTOR_DIAL_ADDR=$OTEL_COLLECTOR_DIAL_ADDR"
	fi
	if [ -z "${URLSHORTEN_DB_DIAL_ADDR+x}" ]; then
		echo "    URLSHORTEN_DB_DIAL_ADDR (missing)"
	else
		echo "    URLSHORTEN_DB_DIAL_ADDR=$URLSHORTEN_DB_DIAL_ADDR"
	fi
	if [ -z "${URLSHORTEN_SERVICE_GRPC_BIND_ADDR+x}" ]; then
		echo "    URLSHORTEN_SERVICE_GRPC_BIND_ADDR (missing)"
	else
		echo "    URLSHORTEN_SERVICE_GRPC_BIND_ADDR=$URLSHORTEN_SERVICE_GRPC_BIND_ADDR"
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


urlshorten_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${URLSHORTEN_DB_DIAL_ADDR+x}" ]; then
		if ! urlshorten_db_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${OTEL_COLLECTOR_DIAL_ADDR+x}" ]; then
		if ! otel_collector_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${URLSHORTEN_SERVICE_GRPC_BIND_ADDR+x}" ]; then
		if ! urlshorten_service_grpc_bind_addr; then
			return $?
		fi
	fi

	run_urlshorten_proc() {
		
        cd urlshorten_proc
        ./urlshorten_proc --urlshorten_db.dial_addr=$URLSHORTEN_DB_DIAL_ADDR --otel_collector.dial_addr=$OTEL_COLLECTOR_DIAL_ADDR --urlshorten_service.grpc.bind_addr=$URLSHORTEN_SERVICE_GRPC_BIND_ADDR &
        URLSHORTEN_PROC=$!
        return $?

	}

	if run_urlshorten_proc; then
		if [ -z "${URLSHORTEN_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting urlshorten_proc: function urlshorten_proc did not set URLSHORTEN_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started urlshorten_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting urlshorten_proc due to exitcode ${exitcode} from urlshorten_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running urlshorten_container"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${OTEL_COLLECTOR_DIAL_ADDR+x}" ]; then
		echo "  OTEL_COLLECTOR_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  OTEL_COLLECTOR_DIAL_ADDR=$OTEL_COLLECTOR_DIAL_ADDR"
	fi
	
	if [ -z "${URLSHORTEN_DB_DIAL_ADDR+x}" ]; then
		echo "  URLSHORTEN_DB_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  URLSHORTEN_DB_DIAL_ADDR=$URLSHORTEN_DB_DIAL_ADDR"
	fi
	
	if [ -z "${URLSHORTEN_SERVICE_GRPC_BIND_ADDR+x}" ]; then
		echo "  URLSHORTEN_SERVICE_GRPC_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  URLSHORTEN_SERVICE_GRPC_BIND_ADDR=$URLSHORTEN_SERVICE_GRPC_BIND_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	urlshorten_proc
	
	wait
}

run_all
