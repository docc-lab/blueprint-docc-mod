#!/bin/bash

WORKSPACE_NAME="uniqueid_service_v8_ctr"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${OTELCOL_V8_DIAL_ADDR+x}" ]; then
		echo "    OTELCOL_V8_DIAL_ADDR (missing)"
	else
		echo "    OTELCOL_V8_DIAL_ADDR=$OTELCOL_V8_DIAL_ADDR"
	fi
	if [ -z "${UNIQUEID_SERVICE_V8_GRPC_BIND_ADDR+x}" ]; then
		echo "    UNIQUEID_SERVICE_V8_GRPC_BIND_ADDR (missing)"
	else
		echo "    UNIQUEID_SERVICE_V8_GRPC_BIND_ADDR=$UNIQUEID_SERVICE_V8_GRPC_BIND_ADDR"
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


uniqueid_service_v8_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${OTELCOL_V8_DIAL_ADDR+x}" ]; then
		if ! otelcol_v8_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${UNIQUEID_SERVICE_V8_GRPC_BIND_ADDR+x}" ]; then
		if ! uniqueid_service_v8_grpc_bind_addr; then
			return $?
		fi
	fi

	run_uniqueid_service_v8_proc() {
		export GC_INTERVAL_SEC=${GC_INTERVAL_SEC:-0.1}
		export GOGC=off
        cd uniqueid_service_v8_proc
        ./uniqueid_service_v8_proc --otelcol_v8.dial_addr=$OTELCOL_V8_DIAL_ADDR --uniqueid_service_v8.grpc.bind_addr=$UNIQUEID_SERVICE_V8_GRPC_BIND_ADDR &
        UNIQUEID_SERVICE_V8_PROC=$!
        return $?

	}

	if run_uniqueid_service_v8_proc; then
		if [ -z "${UNIQUEID_SERVICE_V8_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting uniqueid_service_v8_proc: function uniqueid_service_v8_proc did not set UNIQUEID_SERVICE_V8_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started uniqueid_service_v8_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting uniqueid_service_v8_proc due to exitcode ${exitcode} from uniqueid_service_v8_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running uniqueid_service_v8_ctr"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${OTELCOL_V8_DIAL_ADDR+x}" ]; then
		echo "  OTELCOL_V8_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  OTELCOL_V8_DIAL_ADDR=$OTELCOL_V8_DIAL_ADDR"
	fi
	
	if [ -z "${UNIQUEID_SERVICE_V8_GRPC_BIND_ADDR+x}" ]; then
		echo "  UNIQUEID_SERVICE_V8_GRPC_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  UNIQUEID_SERVICE_V8_GRPC_BIND_ADDR=$UNIQUEID_SERVICE_V8_GRPC_BIND_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	uniqueid_service_v8_proc
	
	wait
}

run_all
