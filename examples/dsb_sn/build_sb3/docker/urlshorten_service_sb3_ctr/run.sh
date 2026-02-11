#!/bin/bash

WORKSPACE_NAME="urlshorten_service_sb3_ctr"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${OTELCOL_SB3_DIAL_ADDR+x}" ]; then
		echo "    OTELCOL_SB3_DIAL_ADDR (missing)"
	else
		echo "    OTELCOL_SB3_DIAL_ADDR=$OTELCOL_SB3_DIAL_ADDR"
	fi
	if [ -z "${URLSHORTEN_DB_SB3_DIAL_ADDR+x}" ]; then
		echo "    URLSHORTEN_DB_SB3_DIAL_ADDR (missing)"
	else
		echo "    URLSHORTEN_DB_SB3_DIAL_ADDR=$URLSHORTEN_DB_SB3_DIAL_ADDR"
	fi
	if [ -z "${URLSHORTEN_SERVICE_SB3_GRPC_BIND_ADDR+x}" ]; then
		echo "    URLSHORTEN_SERVICE_SB3_GRPC_BIND_ADDR (missing)"
	else
		echo "    URLSHORTEN_SERVICE_SB3_GRPC_BIND_ADDR=$URLSHORTEN_SERVICE_SB3_GRPC_BIND_ADDR"
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


urlshorten_service_sb3_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${URLSHORTEN_DB_SB3_DIAL_ADDR+x}" ]; then
		if ! urlshorten_db_sb3_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${OTELCOL_SB3_DIAL_ADDR+x}" ]; then
		if ! otelcol_sb3_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${URLSHORTEN_SERVICE_SB3_GRPC_BIND_ADDR+x}" ]; then
		if ! urlshorten_service_sb3_grpc_bind_addr; then
			return $?
		fi
	fi

	run_urlshorten_service_sb3_proc() {
		
        cd urlshorten_service_sb3_proc
        numactl --membind=0 ./urlshorten_service_sb3_proc --urlshorten_db_sb3.dial_addr=$URLSHORTEN_DB_SB3_DIAL_ADDR --otelcol_sb3.dial_addr=$OTELCOL_SB3_DIAL_ADDR --urlshorten_service_sb3.grpc.bind_addr=$URLSHORTEN_SERVICE_SB3_GRPC_BIND_ADDR &
        URLSHORTEN_SERVICE_SB3_PROC=$!
        return $?

	}

	if run_urlshorten_service_sb3_proc; then
		if [ -z "${URLSHORTEN_SERVICE_SB3_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting urlshorten_service_sb3_proc: function urlshorten_service_sb3_proc did not set URLSHORTEN_SERVICE_SB3_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started urlshorten_service_sb3_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting urlshorten_service_sb3_proc due to exitcode ${exitcode} from urlshorten_service_sb3_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running urlshorten_service_sb3_ctr"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${OTELCOL_SB3_DIAL_ADDR+x}" ]; then
		echo "  OTELCOL_SB3_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  OTELCOL_SB3_DIAL_ADDR=$OTELCOL_SB3_DIAL_ADDR"
	fi
	
	if [ -z "${URLSHORTEN_DB_SB3_DIAL_ADDR+x}" ]; then
		echo "  URLSHORTEN_DB_SB3_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  URLSHORTEN_DB_SB3_DIAL_ADDR=$URLSHORTEN_DB_SB3_DIAL_ADDR"
	fi
	
	if [ -z "${URLSHORTEN_SERVICE_SB3_GRPC_BIND_ADDR+x}" ]; then
		echo "  URLSHORTEN_SERVICE_SB3_GRPC_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  URLSHORTEN_SERVICE_SB3_GRPC_BIND_ADDR=$URLSHORTEN_SERVICE_SB3_GRPC_BIND_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	urlshorten_service_sb3_proc
	
	wait
}

run_all
