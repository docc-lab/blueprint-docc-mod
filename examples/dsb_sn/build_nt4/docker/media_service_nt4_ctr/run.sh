#!/bin/bash

WORKSPACE_NAME="media_service_nt4_ctr"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${MEDIA_SERVICE_NT4_GRPC_BIND_ADDR+x}" ]; then
		echo "    MEDIA_SERVICE_NT4_GRPC_BIND_ADDR (missing)"
	else
		echo "    MEDIA_SERVICE_NT4_GRPC_BIND_ADDR=$MEDIA_SERVICE_NT4_GRPC_BIND_ADDR"
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


media_service_nt4_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${MEDIA_SERVICE_NT4_GRPC_BIND_ADDR+x}" ]; then
		if ! media_service_nt4_grpc_bind_addr; then
			return $?
		fi
	fi

	run_media_service_nt4_proc() {
		
        cd media_service_nt4_proc
        numactl --membind=0 ./media_service_nt4_proc --media_service_nt4.grpc.bind_addr=$MEDIA_SERVICE_NT4_GRPC_BIND_ADDR &
        MEDIA_SERVICE_NT4_PROC=$!
        return $?

	}

	if run_media_service_nt4_proc; then
		if [ -z "${MEDIA_SERVICE_NT4_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting media_service_nt4_proc: function media_service_nt4_proc did not set MEDIA_SERVICE_NT4_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started media_service_nt4_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting media_service_nt4_proc due to exitcode ${exitcode} from media_service_nt4_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running media_service_nt4_ctr"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${MEDIA_SERVICE_NT4_GRPC_BIND_ADDR+x}" ]; then
		echo "  MEDIA_SERVICE_NT4_GRPC_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  MEDIA_SERVICE_NT4_GRPC_BIND_ADDR=$MEDIA_SERVICE_NT4_GRPC_BIND_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	media_service_nt4_proc
	
	wait
}

run_all
