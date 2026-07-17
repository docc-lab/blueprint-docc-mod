#!/bin/bash

WORKSPACE_NAME="urlshorten_service_cgpb_es_nw_ctr"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${OTELCOL_CGPB_ES_NW_DIAL_ADDR+x}" ]; then
		echo "    OTELCOL_CGPB_ES_NW_DIAL_ADDR (missing)"
	else
		echo "    OTELCOL_CGPB_ES_NW_DIAL_ADDR=$OTELCOL_CGPB_ES_NW_DIAL_ADDR"
	fi
	if [ -z "${URLSHORTEN_DB_CGPB_ES_NW_DIAL_ADDR+x}" ]; then
		echo "    URLSHORTEN_DB_CGPB_ES_NW_DIAL_ADDR (missing)"
	else
		echo "    URLSHORTEN_DB_CGPB_ES_NW_DIAL_ADDR=$URLSHORTEN_DB_CGPB_ES_NW_DIAL_ADDR"
	fi
	if [ -z "${URLSHORTEN_SERVICE_CGPB_ES_NW_GRPC_BIND_ADDR+x}" ]; then
		echo "    URLSHORTEN_SERVICE_CGPB_ES_NW_GRPC_BIND_ADDR (missing)"
	else
		echo "    URLSHORTEN_SERVICE_CGPB_ES_NW_GRPC_BIND_ADDR=$URLSHORTEN_SERVICE_CGPB_ES_NW_GRPC_BIND_ADDR"
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


urlshorten_service_cgpb_es_nw_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${URLSHORTEN_DB_CGPB_ES_NW_DIAL_ADDR+x}" ]; then
		if ! urlshorten_db_cgpb_es_nw_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${OTELCOL_CGPB_ES_NW_DIAL_ADDR+x}" ]; then
		if ! otelcol_cgpb_es_nw_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${URLSHORTEN_SERVICE_CGPB_ES_NW_GRPC_BIND_ADDR+x}" ]; then
		if ! urlshorten_service_cgpb_es_nw_grpc_bind_addr; then
			return $?
		fi
	fi

	run_urlshorten_service_cgpb_es_nw_proc() {
		
        export GC_INTERVAL_SEC=${GC_INTERVAL_SEC:-0.1}
        export GOGC=${GOGC:-off}
        cd urlshorten_service_cgpb_es_nw_proc
        ./urlshorten_service_cgpb_es_nw_proc --urlshorten_db_cgpb_es_nw.dial_addr=$URLSHORTEN_DB_CGPB_ES_NW_DIAL_ADDR --otelcol_cgpb_es_nw.dial_addr=$OTELCOL_CGPB_ES_NW_DIAL_ADDR --urlshorten_service_cgpb_es_nw.grpc.bind_addr=$URLSHORTEN_SERVICE_CGPB_ES_NW_GRPC_BIND_ADDR &
        URLSHORTEN_SERVICE_CGPB_ES_NW_PROC=$!
        return $?

	}

	if run_urlshorten_service_cgpb_es_nw_proc; then
		if [ -z "${URLSHORTEN_SERVICE_CGPB_ES_NW_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting urlshorten_service_cgpb_es_nw_proc: function urlshorten_service_cgpb_es_nw_proc did not set URLSHORTEN_SERVICE_CGPB_ES_NW_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started urlshorten_service_cgpb_es_nw_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting urlshorten_service_cgpb_es_nw_proc due to exitcode ${exitcode} from urlshorten_service_cgpb_es_nw_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running urlshorten_service_cgpb_es_nw_ctr"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${OTELCOL_CGPB_ES_NW_DIAL_ADDR+x}" ]; then
		echo "  OTELCOL_CGPB_ES_NW_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  OTELCOL_CGPB_ES_NW_DIAL_ADDR=$OTELCOL_CGPB_ES_NW_DIAL_ADDR"
	fi
	
	if [ -z "${URLSHORTEN_DB_CGPB_ES_NW_DIAL_ADDR+x}" ]; then
		echo "  URLSHORTEN_DB_CGPB_ES_NW_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  URLSHORTEN_DB_CGPB_ES_NW_DIAL_ADDR=$URLSHORTEN_DB_CGPB_ES_NW_DIAL_ADDR"
	fi
	
	if [ -z "${URLSHORTEN_SERVICE_CGPB_ES_NW_GRPC_BIND_ADDR+x}" ]; then
		echo "  URLSHORTEN_SERVICE_CGPB_ES_NW_GRPC_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  URLSHORTEN_SERVICE_CGPB_ES_NW_GRPC_BIND_ADDR=$URLSHORTEN_SERVICE_CGPB_ES_NW_GRPC_BIND_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	urlshorten_service_cgpb_es_nw_proc
	
	wait
}

run_all
