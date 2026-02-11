#!/bin/bash

WORKSPACE_NAME="socialgraph_service_sb3_ctr"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${OTELCOL_SB3_DIAL_ADDR+x}" ]; then
		echo "    OTELCOL_SB3_DIAL_ADDR (missing)"
	else
		echo "    OTELCOL_SB3_DIAL_ADDR=$OTELCOL_SB3_DIAL_ADDR"
	fi
	if [ -z "${SOCIAL_CACHE_SB3_DIAL_ADDR+x}" ]; then
		echo "    SOCIAL_CACHE_SB3_DIAL_ADDR (missing)"
	else
		echo "    SOCIAL_CACHE_SB3_DIAL_ADDR=$SOCIAL_CACHE_SB3_DIAL_ADDR"
	fi
	if [ -z "${SOCIAL_DB_SB3_DIAL_ADDR+x}" ]; then
		echo "    SOCIAL_DB_SB3_DIAL_ADDR (missing)"
	else
		echo "    SOCIAL_DB_SB3_DIAL_ADDR=$SOCIAL_DB_SB3_DIAL_ADDR"
	fi
	if [ -z "${SOCIALGRAPH_SERVICE_SB3_GRPC_BIND_ADDR+x}" ]; then
		echo "    SOCIALGRAPH_SERVICE_SB3_GRPC_BIND_ADDR (missing)"
	else
		echo "    SOCIALGRAPH_SERVICE_SB3_GRPC_BIND_ADDR=$SOCIALGRAPH_SERVICE_SB3_GRPC_BIND_ADDR"
	fi
	if [ -z "${USERID_SERVICE_SB3_GRPC_DIAL_ADDR+x}" ]; then
		echo "    USERID_SERVICE_SB3_GRPC_DIAL_ADDR (missing)"
	else
		echo "    USERID_SERVICE_SB3_GRPC_DIAL_ADDR=$USERID_SERVICE_SB3_GRPC_DIAL_ADDR"
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


socialgraph_service_sb3_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${SOCIAL_CACHE_SB3_DIAL_ADDR+x}" ]; then
		if ! social_cache_sb3_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${SOCIAL_DB_SB3_DIAL_ADDR+x}" ]; then
		if ! social_db_sb3_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${USERID_SERVICE_SB3_GRPC_DIAL_ADDR+x}" ]; then
		if ! userid_service_sb3_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${OTELCOL_SB3_DIAL_ADDR+x}" ]; then
		if ! otelcol_sb3_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${SOCIALGRAPH_SERVICE_SB3_GRPC_BIND_ADDR+x}" ]; then
		if ! socialgraph_service_sb3_grpc_bind_addr; then
			return $?
		fi
	fi

	run_socialgraph_service_sb3_proc() {
		
        cd socialgraph_service_sb3_proc
        numactl --membind=0 ./socialgraph_service_sb3_proc --social_cache_sb3.dial_addr=$SOCIAL_CACHE_SB3_DIAL_ADDR --social_db_sb3.dial_addr=$SOCIAL_DB_SB3_DIAL_ADDR --userid_service_sb3.grpc.dial_addr=$USERID_SERVICE_SB3_GRPC_DIAL_ADDR --otelcol_sb3.dial_addr=$OTELCOL_SB3_DIAL_ADDR --socialgraph_service_sb3.grpc.bind_addr=$SOCIALGRAPH_SERVICE_SB3_GRPC_BIND_ADDR &
        SOCIALGRAPH_SERVICE_SB3_PROC=$!
        return $?

	}

	if run_socialgraph_service_sb3_proc; then
		if [ -z "${SOCIALGRAPH_SERVICE_SB3_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting socialgraph_service_sb3_proc: function socialgraph_service_sb3_proc did not set SOCIALGRAPH_SERVICE_SB3_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started socialgraph_service_sb3_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting socialgraph_service_sb3_proc due to exitcode ${exitcode} from socialgraph_service_sb3_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running socialgraph_service_sb3_ctr"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${OTELCOL_SB3_DIAL_ADDR+x}" ]; then
		echo "  OTELCOL_SB3_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  OTELCOL_SB3_DIAL_ADDR=$OTELCOL_SB3_DIAL_ADDR"
	fi
	
	if [ -z "${SOCIAL_CACHE_SB3_DIAL_ADDR+x}" ]; then
		echo "  SOCIAL_CACHE_SB3_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  SOCIAL_CACHE_SB3_DIAL_ADDR=$SOCIAL_CACHE_SB3_DIAL_ADDR"
	fi
	
	if [ -z "${SOCIAL_DB_SB3_DIAL_ADDR+x}" ]; then
		echo "  SOCIAL_DB_SB3_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  SOCIAL_DB_SB3_DIAL_ADDR=$SOCIAL_DB_SB3_DIAL_ADDR"
	fi
	
	if [ -z "${SOCIALGRAPH_SERVICE_SB3_GRPC_BIND_ADDR+x}" ]; then
		echo "  SOCIALGRAPH_SERVICE_SB3_GRPC_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  SOCIALGRAPH_SERVICE_SB3_GRPC_BIND_ADDR=$SOCIALGRAPH_SERVICE_SB3_GRPC_BIND_ADDR"
	fi
	
	if [ -z "${USERID_SERVICE_SB3_GRPC_DIAL_ADDR+x}" ]; then
		echo "  USERID_SERVICE_SB3_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USERID_SERVICE_SB3_GRPC_DIAL_ADDR=$USERID_SERVICE_SB3_GRPC_DIAL_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	socialgraph_service_sb3_proc
	
	wait
}

run_all
