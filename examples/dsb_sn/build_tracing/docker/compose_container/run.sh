#!/bin/bash

WORKSPACE_NAME="compose_container"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${COMPOSEPOST_SERVICE_GRPC_BIND_ADDR+x}" ]; then
		echo "    COMPOSEPOST_SERVICE_GRPC_BIND_ADDR (missing)"
	else
		echo "    COMPOSEPOST_SERVICE_GRPC_BIND_ADDR=$COMPOSEPOST_SERVICE_GRPC_BIND_ADDR"
	fi
	if [ -z "${HOMETIMELINE_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		echo "    HOMETIMELINE_SERVICE_GRPC_DIAL_ADDR (missing)"
	else
		echo "    HOMETIMELINE_SERVICE_GRPC_DIAL_ADDR=$HOMETIMELINE_SERVICE_GRPC_DIAL_ADDR"
	fi
	if [ -z "${MEDIA_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		echo "    MEDIA_SERVICE_GRPC_DIAL_ADDR (missing)"
	else
		echo "    MEDIA_SERVICE_GRPC_DIAL_ADDR=$MEDIA_SERVICE_GRPC_DIAL_ADDR"
	fi
	if [ -z "${OTEL_COLLECTOR_DIAL_ADDR+x}" ]; then
		echo "    OTEL_COLLECTOR_DIAL_ADDR (missing)"
	else
		echo "    OTEL_COLLECTOR_DIAL_ADDR=$OTEL_COLLECTOR_DIAL_ADDR"
	fi
	if [ -z "${POST_STORAGE_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		echo "    POST_STORAGE_SERVICE_GRPC_DIAL_ADDR (missing)"
	else
		echo "    POST_STORAGE_SERVICE_GRPC_DIAL_ADDR=$POST_STORAGE_SERVICE_GRPC_DIAL_ADDR"
	fi
	if [ -z "${TEXT_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		echo "    TEXT_SERVICE_GRPC_DIAL_ADDR (missing)"
	else
		echo "    TEXT_SERVICE_GRPC_DIAL_ADDR=$TEXT_SERVICE_GRPC_DIAL_ADDR"
	fi
	if [ -z "${UNIQUEID_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		echo "    UNIQUEID_SERVICE_GRPC_DIAL_ADDR (missing)"
	else
		echo "    UNIQUEID_SERVICE_GRPC_DIAL_ADDR=$UNIQUEID_SERVICE_GRPC_DIAL_ADDR"
	fi
	if [ -z "${USER_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		echo "    USER_SERVICE_GRPC_DIAL_ADDR (missing)"
	else
		echo "    USER_SERVICE_GRPC_DIAL_ADDR=$USER_SERVICE_GRPC_DIAL_ADDR"
	fi
	if [ -z "${USERTIMELINE_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		echo "    USERTIMELINE_SERVICE_GRPC_DIAL_ADDR (missing)"
	else
		echo "    USERTIMELINE_SERVICE_GRPC_DIAL_ADDR=$USERTIMELINE_SERVICE_GRPC_DIAL_ADDR"
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


compose_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${POST_STORAGE_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		if ! post_storage_service_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${OTEL_COLLECTOR_DIAL_ADDR+x}" ]; then
		if ! otel_collector_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${USERTIMELINE_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		if ! usertimeline_service_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${USER_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		if ! user_service_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${UNIQUEID_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		if ! uniqueid_service_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${MEDIA_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		if ! media_service_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${TEXT_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		if ! text_service_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${HOMETIMELINE_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		if ! hometimeline_service_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${COMPOSEPOST_SERVICE_GRPC_BIND_ADDR+x}" ]; then
		if ! composepost_service_grpc_bind_addr; then
			return $?
		fi
	fi

	run_compose_proc() {
		
        cd compose_proc
        ./compose_proc --post_storage_service.grpc.dial_addr=$POST_STORAGE_SERVICE_GRPC_DIAL_ADDR --otel_collector.dial_addr=$OTEL_COLLECTOR_DIAL_ADDR --usertimeline_service.grpc.dial_addr=$USERTIMELINE_SERVICE_GRPC_DIAL_ADDR --user_service.grpc.dial_addr=$USER_SERVICE_GRPC_DIAL_ADDR --uniqueid_service.grpc.dial_addr=$UNIQUEID_SERVICE_GRPC_DIAL_ADDR --media_service.grpc.dial_addr=$MEDIA_SERVICE_GRPC_DIAL_ADDR --text_service.grpc.dial_addr=$TEXT_SERVICE_GRPC_DIAL_ADDR --hometimeline_service.grpc.dial_addr=$HOMETIMELINE_SERVICE_GRPC_DIAL_ADDR --composepost_service.grpc.bind_addr=$COMPOSEPOST_SERVICE_GRPC_BIND_ADDR &
        COMPOSE_PROC=$!
        return $?

	}

	if run_compose_proc; then
		if [ -z "${COMPOSE_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting compose_proc: function compose_proc did not set COMPOSE_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started compose_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting compose_proc due to exitcode ${exitcode} from compose_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running compose_container"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${COMPOSEPOST_SERVICE_GRPC_BIND_ADDR+x}" ]; then
		echo "  COMPOSEPOST_SERVICE_GRPC_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  COMPOSEPOST_SERVICE_GRPC_BIND_ADDR=$COMPOSEPOST_SERVICE_GRPC_BIND_ADDR"
	fi
	
	if [ -z "${HOMETIMELINE_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		echo "  HOMETIMELINE_SERVICE_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  HOMETIMELINE_SERVICE_GRPC_DIAL_ADDR=$HOMETIMELINE_SERVICE_GRPC_DIAL_ADDR"
	fi
	
	if [ -z "${MEDIA_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		echo "  MEDIA_SERVICE_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  MEDIA_SERVICE_GRPC_DIAL_ADDR=$MEDIA_SERVICE_GRPC_DIAL_ADDR"
	fi
	
	if [ -z "${OTEL_COLLECTOR_DIAL_ADDR+x}" ]; then
		echo "  OTEL_COLLECTOR_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  OTEL_COLLECTOR_DIAL_ADDR=$OTEL_COLLECTOR_DIAL_ADDR"
	fi
	
	if [ -z "${POST_STORAGE_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		echo "  POST_STORAGE_SERVICE_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  POST_STORAGE_SERVICE_GRPC_DIAL_ADDR=$POST_STORAGE_SERVICE_GRPC_DIAL_ADDR"
	fi
	
	if [ -z "${TEXT_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		echo "  TEXT_SERVICE_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  TEXT_SERVICE_GRPC_DIAL_ADDR=$TEXT_SERVICE_GRPC_DIAL_ADDR"
	fi
	
	if [ -z "${UNIQUEID_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		echo "  UNIQUEID_SERVICE_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  UNIQUEID_SERVICE_GRPC_DIAL_ADDR=$UNIQUEID_SERVICE_GRPC_DIAL_ADDR"
	fi
	
	if [ -z "${USER_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		echo "  USER_SERVICE_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USER_SERVICE_GRPC_DIAL_ADDR=$USER_SERVICE_GRPC_DIAL_ADDR"
	fi
	
	if [ -z "${USERTIMELINE_SERVICE_GRPC_DIAL_ADDR+x}" ]; then
		echo "  USERTIMELINE_SERVICE_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USERTIMELINE_SERVICE_GRPC_DIAL_ADDR=$USERTIMELINE_SERVICE_GRPC_DIAL_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	compose_proc
	
	wait
}

run_all
