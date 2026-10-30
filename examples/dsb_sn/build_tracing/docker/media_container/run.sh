#!/bin/bash

WORKSPACE_NAME="media_container"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${MEDIA_SERVICE_GRPC_BIND_ADDR+x}" ]; then
		echo "    MEDIA_SERVICE_GRPC_BIND_ADDR (missing)"
	else
		echo "    MEDIA_SERVICE_GRPC_BIND_ADDR=$MEDIA_SERVICE_GRPC_BIND_ADDR"
	fi
	if [ -z "${OTEL_COLLECTOR_DIAL_ADDR+x}" ]; then
		echo "    OTEL_COLLECTOR_DIAL_ADDR (missing)"
	else
		echo "    OTEL_COLLECTOR_DIAL_ADDR=$OTEL_COLLECTOR_DIAL_ADDR"
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


media_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${OTEL_COLLECTOR_DIAL_ADDR+x}" ]; then
		if ! otel_collector_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${MEDIA_SERVICE_GRPC_BIND_ADDR+x}" ]; then
		if ! media_service_grpc_bind_addr; then
			return $?
		fi
	fi

	run_media_proc() {
		
        cd media_proc
        ./media_proc --otel_collector.dial_addr=$OTEL_COLLECTOR_DIAL_ADDR --media_service.grpc.bind_addr=$MEDIA_SERVICE_GRPC_BIND_ADDR &
        MEDIA_PROC=$!
        return $?

	}

	if run_media_proc; then
		if [ -z "${MEDIA_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting media_proc: function media_proc did not set MEDIA_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started media_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting media_proc due to exitcode ${exitcode} from media_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running media_container"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${MEDIA_SERVICE_GRPC_BIND_ADDR+x}" ]; then
		echo "  MEDIA_SERVICE_GRPC_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  MEDIA_SERVICE_GRPC_BIND_ADDR=$MEDIA_SERVICE_GRPC_BIND_ADDR"
	fi
	
	if [ -z "${OTEL_COLLECTOR_DIAL_ADDR+x}" ]; then
		echo "  OTEL_COLLECTOR_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  OTEL_COLLECTOR_DIAL_ADDR=$OTEL_COLLECTOR_DIAL_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	media_proc
	
	wait
}

run_all
