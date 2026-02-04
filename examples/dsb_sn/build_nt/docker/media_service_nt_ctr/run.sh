#!/bin/bash

WORKSPACE_NAME="media_service_nt_ctr"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${MEDIA_SERVICE_NT_GRPC_BIND_ADDR+x}" ]; then
		echo "    MEDIA_SERVICE_NT_GRPC_BIND_ADDR (missing)"
	else
		echo "    MEDIA_SERVICE_NT_GRPC_BIND_ADDR=$MEDIA_SERVICE_NT_GRPC_BIND_ADDR"
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


media_service_nt_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${MEDIA_SERVICE_NT_GRPC_BIND_ADDR+x}" ]; then
		if ! media_service_nt_grpc_bind_addr; then
			return $?
		fi
	fi

	run_media_service_nt_proc() {
		
        cd media_service_nt_proc
        ./media_service_nt_proc --media_service_nt.grpc.bind_addr=$MEDIA_SERVICE_NT_GRPC_BIND_ADDR &
        MEDIA_SERVICE_NT_PROC=$!
        return $?

	}

	if run_media_service_nt_proc; then
		if [ -z "${MEDIA_SERVICE_NT_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting media_service_nt_proc: function media_service_nt_proc did not set MEDIA_SERVICE_NT_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started media_service_nt_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting media_service_nt_proc due to exitcode ${exitcode} from media_service_nt_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running media_service_nt_ctr"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${MEDIA_SERVICE_NT_GRPC_BIND_ADDR+x}" ]; then
		echo "  MEDIA_SERVICE_NT_GRPC_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  MEDIA_SERVICE_NT_GRPC_BIND_ADDR=$MEDIA_SERVICE_NT_GRPC_BIND_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	media_service_nt_proc
	
	wait
}

run_all
