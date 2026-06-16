#!/bin/bash

WORKSPACE_NAME="tracepressure_service_rc_esrev2_ctr"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${OTELCOL_RC_ESREV2_DIAL_ADDR+x}" ]; then
		echo "    OTELCOL_RC_ESREV2_DIAL_ADDR (missing)"
	else
		echo "    OTELCOL_RC_ESREV2_DIAL_ADDR=$OTELCOL_RC_ESREV2_DIAL_ADDR"
	fi
	if [ -z "${TRACEPRESSURE_SERVICE_RC_ESREV2_HTTP_BIND_ADDR+x}" ]; then
		echo "    TRACEPRESSURE_SERVICE_RC_ESREV2_HTTP_BIND_ADDR (missing)"
	else
		echo "    TRACEPRESSURE_SERVICE_RC_ESREV2_HTTP_BIND_ADDR=$TRACEPRESSURE_SERVICE_RC_ESREV2_HTTP_BIND_ADDR"
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


tracepressure_service_rc_esrev2_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${OTELCOL_RC_ESREV2_DIAL_ADDR+x}" ]; then
		if ! otelcol_rc_esrev2_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${TRACEPRESSURE_SERVICE_RC_ESREV2_HTTP_BIND_ADDR+x}" ]; then
		if ! tracepressure_service_rc_esrev2_http_bind_addr; then
			return $?
		fi
	fi

	run_tracepressure_service_rc_esrev2_proc() {
		
        export GC_INTERVAL_SEC=${GC_INTERVAL_SEC:-0.1}
        export GOGC=${GOGC:-off}
        cd tracepressure_service_rc_esrev2_proc
        ./tracepressure_service_rc_esrev2_proc --otelcol_rc_esrev2.dial_addr=$OTELCOL_RC_ESREV2_DIAL_ADDR --tracepressure_service_rc_esrev2.http.bind_addr=$TRACEPRESSURE_SERVICE_RC_ESREV2_HTTP_BIND_ADDR &
        TRACEPRESSURE_SERVICE_RC_ESREV2_PROC=$!
        return $?

	}

	if run_tracepressure_service_rc_esrev2_proc; then
		if [ -z "${TRACEPRESSURE_SERVICE_RC_ESREV2_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting tracepressure_service_rc_esrev2_proc: function tracepressure_service_rc_esrev2_proc did not set TRACEPRESSURE_SERVICE_RC_ESREV2_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started tracepressure_service_rc_esrev2_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting tracepressure_service_rc_esrev2_proc due to exitcode ${exitcode} from tracepressure_service_rc_esrev2_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running tracepressure_service_rc_esrev2_ctr"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${OTELCOL_RC_ESREV2_DIAL_ADDR+x}" ]; then
		echo "  OTELCOL_RC_ESREV2_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  OTELCOL_RC_ESREV2_DIAL_ADDR=$OTELCOL_RC_ESREV2_DIAL_ADDR"
	fi
	
	if [ -z "${TRACEPRESSURE_SERVICE_RC_ESREV2_HTTP_BIND_ADDR+x}" ]; then
		echo "  TRACEPRESSURE_SERVICE_RC_ESREV2_HTTP_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  TRACEPRESSURE_SERVICE_RC_ESREV2_HTTP_BIND_ADDR=$TRACEPRESSURE_SERVICE_RC_ESREV2_HTTP_BIND_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	tracepressure_service_rc_esrev2_proc
	
	wait
}

run_all
