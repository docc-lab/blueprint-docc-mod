#!/bin/bash

WORKSPACE_NAME="socialgraph_service_v7_ctr"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${OTELCOL_V7_DIAL_ADDR+x}" ]; then
		echo "    OTELCOL_V7_DIAL_ADDR (missing)"
	else
		echo "    OTELCOL_V7_DIAL_ADDR=$OTELCOL_V7_DIAL_ADDR"
	fi
	if [ -z "${SOCIAL_CACHE_V7_DIAL_ADDR+x}" ]; then
		echo "    SOCIAL_CACHE_V7_DIAL_ADDR (missing)"
	else
		echo "    SOCIAL_CACHE_V7_DIAL_ADDR=$SOCIAL_CACHE_V7_DIAL_ADDR"
	fi
	if [ -z "${SOCIAL_DB_V7_DIAL_ADDR+x}" ]; then
		echo "    SOCIAL_DB_V7_DIAL_ADDR (missing)"
	else
		echo "    SOCIAL_DB_V7_DIAL_ADDR=$SOCIAL_DB_V7_DIAL_ADDR"
	fi
	if [ -z "${SOCIALGRAPH_SERVICE_V7_GRPC_BIND_ADDR+x}" ]; then
		echo "    SOCIALGRAPH_SERVICE_V7_GRPC_BIND_ADDR (missing)"
	else
		echo "    SOCIALGRAPH_SERVICE_V7_GRPC_BIND_ADDR=$SOCIALGRAPH_SERVICE_V7_GRPC_BIND_ADDR"
	fi
	if [ -z "${USERID_SERVICE_V7_GRPC_DIAL_ADDR+x}" ]; then
		echo "    USERID_SERVICE_V7_GRPC_DIAL_ADDR (missing)"
	else
		echo "    USERID_SERVICE_V7_GRPC_DIAL_ADDR=$USERID_SERVICE_V7_GRPC_DIAL_ADDR"
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


socialgraph_service_v7_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${SOCIAL_CACHE_V7_DIAL_ADDR+x}" ]; then
		if ! social_cache_v7_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${SOCIAL_DB_V7_DIAL_ADDR+x}" ]; then
		if ! social_db_v7_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${USERID_SERVICE_V7_GRPC_DIAL_ADDR+x}" ]; then
		if ! userid_service_v7_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${OTELCOL_V7_DIAL_ADDR+x}" ]; then
		if ! otelcol_v7_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${SOCIALGRAPH_SERVICE_V7_GRPC_BIND_ADDR+x}" ]; then
		if ! socialgraph_service_v7_grpc_bind_addr; then
			return $?
		fi
	fi

	run_socialgraph_service_v7_proc() {
		
        cd socialgraph_service_v7_proc
        numactl --membind=0 ./socialgraph_service_v7_proc --social_cache_v7.dial_addr=$SOCIAL_CACHE_V7_DIAL_ADDR --social_db_v7.dial_addr=$SOCIAL_DB_V7_DIAL_ADDR --userid_service_v7.grpc.dial_addr=$USERID_SERVICE_V7_GRPC_DIAL_ADDR --otelcol_v7.dial_addr=$OTELCOL_V7_DIAL_ADDR --socialgraph_service_v7.grpc.bind_addr=$SOCIALGRAPH_SERVICE_V7_GRPC_BIND_ADDR &
        SOCIALGRAPH_SERVICE_V7_PROC=$!
        return $?

	}

	if run_socialgraph_service_v7_proc; then
		if [ -z "${SOCIALGRAPH_SERVICE_V7_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting socialgraph_service_v7_proc: function socialgraph_service_v7_proc did not set SOCIALGRAPH_SERVICE_V7_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started socialgraph_service_v7_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting socialgraph_service_v7_proc due to exitcode ${exitcode} from socialgraph_service_v7_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running socialgraph_service_v7_ctr"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${OTELCOL_V7_DIAL_ADDR+x}" ]; then
		echo "  OTELCOL_V7_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  OTELCOL_V7_DIAL_ADDR=$OTELCOL_V7_DIAL_ADDR"
	fi
	
	if [ -z "${SOCIAL_CACHE_V7_DIAL_ADDR+x}" ]; then
		echo "  SOCIAL_CACHE_V7_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  SOCIAL_CACHE_V7_DIAL_ADDR=$SOCIAL_CACHE_V7_DIAL_ADDR"
	fi
	
	if [ -z "${SOCIAL_DB_V7_DIAL_ADDR+x}" ]; then
		echo "  SOCIAL_DB_V7_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  SOCIAL_DB_V7_DIAL_ADDR=$SOCIAL_DB_V7_DIAL_ADDR"
	fi
	
	if [ -z "${SOCIALGRAPH_SERVICE_V7_GRPC_BIND_ADDR+x}" ]; then
		echo "  SOCIALGRAPH_SERVICE_V7_GRPC_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  SOCIALGRAPH_SERVICE_V7_GRPC_BIND_ADDR=$SOCIALGRAPH_SERVICE_V7_GRPC_BIND_ADDR"
	fi
	
	if [ -z "${USERID_SERVICE_V7_GRPC_DIAL_ADDR+x}" ]; then
		echo "  USERID_SERVICE_V7_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USERID_SERVICE_V7_GRPC_DIAL_ADDR=$USERID_SERVICE_V7_GRPC_DIAL_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	socialgraph_service_v7_proc
	
	wait
}

run_all
