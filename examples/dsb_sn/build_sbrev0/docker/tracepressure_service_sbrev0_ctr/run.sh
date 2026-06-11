#!/bin/bash

WORKSPACE_NAME="tracepressure_service_sbrev0_ctr"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${OTELCOL_SBREV0_DIAL_ADDR+x}" ]; then
		echo "    OTELCOL_SBREV0_DIAL_ADDR (missing)"
	else
		echo "    OTELCOL_SBREV0_DIAL_ADDR=$OTELCOL_SBREV0_DIAL_ADDR"
	fi
	if [ -z "${TRACEPRESSURE_SERVICE_SBREV0_HTTP_BIND_ADDR+x}" ]; then
		echo "    TRACEPRESSURE_SERVICE_SBREV0_HTTP_BIND_ADDR (missing)"
	else
		echo "    TRACEPRESSURE_SERVICE_SBREV0_HTTP_BIND_ADDR=$TRACEPRESSURE_SERVICE_SBREV0_HTTP_BIND_ADDR"
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


tracepressure_service_sbrev0_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${OTELCOL_SBREV0_DIAL_ADDR+x}" ]; then
		if ! otelcol_sbrev0_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${TRACEPRESSURE_SERVICE_SBREV0_HTTP_BIND_ADDR+x}" ]; then
		if ! tracepressure_service_sbrev0_http_bind_addr; then
			return $?
		fi
	fi

	run_tracepressure_service_sbrev0_proc() {
		
        export GC_INTERVAL_SEC=${GC_INTERVAL_SEC:-0.1}
        export GOGC=${GOGC:-off}
        cd tracepressure_service_sbrev0_proc
        ./tracepressure_service_sbrev0_proc --otelcol_sbrev0.dial_addr=$OTELCOL_SBREV0_DIAL_ADDR --tracepressure_service_sbrev0.http.bind_addr=$TRACEPRESSURE_SERVICE_SBREV0_HTTP_BIND_ADDR &
        TRACEPRESSURE_SERVICE_SBREV0_PROC=$!
        return $?

	}

	if run_tracepressure_service_sbrev0_proc; then
		if [ -z "${TRACEPRESSURE_SERVICE_SBREV0_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting tracepressure_service_sbrev0_proc: function tracepressure_service_sbrev0_proc did not set TRACEPRESSURE_SERVICE_SBREV0_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started tracepressure_service_sbrev0_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting tracepressure_service_sbrev0_proc due to exitcode ${exitcode} from tracepressure_service_sbrev0_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running tracepressure_service_sbrev0_ctr"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${OTELCOL_SBREV0_DIAL_ADDR+x}" ]; then
		echo "  OTELCOL_SBREV0_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  OTELCOL_SBREV0_DIAL_ADDR=$OTELCOL_SBREV0_DIAL_ADDR"
	fi
	
	if [ -z "${TRACEPRESSURE_SERVICE_SBREV0_HTTP_BIND_ADDR+x}" ]; then
		echo "  TRACEPRESSURE_SERVICE_SBREV0_HTTP_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  TRACEPRESSURE_SERVICE_SBREV0_HTTP_BIND_ADDR=$TRACEPRESSURE_SERVICE_SBREV0_HTTP_BIND_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	tracepressure_service_sbrev0_proc
	
	wait
}

run_all
