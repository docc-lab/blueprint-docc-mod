#!/bin/bash

WORKSPACE_NAME="composepost_service_v4_ctr"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${COMPOSEPOST_SERVICE_V4_GRPC_BIND_ADDR+x}" ]; then
		echo "    COMPOSEPOST_SERVICE_V4_GRPC_BIND_ADDR (missing)"
	else
		echo "    COMPOSEPOST_SERVICE_V4_GRPC_BIND_ADDR=$COMPOSEPOST_SERVICE_V4_GRPC_BIND_ADDR"
	fi
	if [ -z "${HOMETIMELINE_SERVICE_V4_GRPC_DIAL_ADDR+x}" ]; then
		echo "    HOMETIMELINE_SERVICE_V4_GRPC_DIAL_ADDR (missing)"
	else
		echo "    HOMETIMELINE_SERVICE_V4_GRPC_DIAL_ADDR=$HOMETIMELINE_SERVICE_V4_GRPC_DIAL_ADDR"
	fi
	if [ -z "${MEDIA_SERVICE_V4_GRPC_DIAL_ADDR+x}" ]; then
		echo "    MEDIA_SERVICE_V4_GRPC_DIAL_ADDR (missing)"
	else
		echo "    MEDIA_SERVICE_V4_GRPC_DIAL_ADDR=$MEDIA_SERVICE_V4_GRPC_DIAL_ADDR"
	fi
	if [ -z "${OTELCOL_V4_DIAL_ADDR+x}" ]; then
		echo "    OTELCOL_V4_DIAL_ADDR (missing)"
	else
		echo "    OTELCOL_V4_DIAL_ADDR=$OTELCOL_V4_DIAL_ADDR"
	fi
	if [ -z "${POST_STORAGE_SERVICE_V4_GRPC_DIAL_ADDR+x}" ]; then
		echo "    POST_STORAGE_SERVICE_V4_GRPC_DIAL_ADDR (missing)"
	else
		echo "    POST_STORAGE_SERVICE_V4_GRPC_DIAL_ADDR=$POST_STORAGE_SERVICE_V4_GRPC_DIAL_ADDR"
	fi
	if [ -z "${TEXT_SERVICE_V4_GRPC_DIAL_ADDR+x}" ]; then
		echo "    TEXT_SERVICE_V4_GRPC_DIAL_ADDR (missing)"
	else
		echo "    TEXT_SERVICE_V4_GRPC_DIAL_ADDR=$TEXT_SERVICE_V4_GRPC_DIAL_ADDR"
	fi
	if [ -z "${UNIQUEID_SERVICE_V4_GRPC_DIAL_ADDR+x}" ]; then
		echo "    UNIQUEID_SERVICE_V4_GRPC_DIAL_ADDR (missing)"
	else
		echo "    UNIQUEID_SERVICE_V4_GRPC_DIAL_ADDR=$UNIQUEID_SERVICE_V4_GRPC_DIAL_ADDR"
	fi
	if [ -z "${USER_SERVICE_V4_GRPC_DIAL_ADDR+x}" ]; then
		echo "    USER_SERVICE_V4_GRPC_DIAL_ADDR (missing)"
	else
		echo "    USER_SERVICE_V4_GRPC_DIAL_ADDR=$USER_SERVICE_V4_GRPC_DIAL_ADDR"
	fi
	if [ -z "${USERTIMELINE_SERVICE_V4_GRPC_DIAL_ADDR+x}" ]; then
		echo "    USERTIMELINE_SERVICE_V4_GRPC_DIAL_ADDR (missing)"
	else
		echo "    USERTIMELINE_SERVICE_V4_GRPC_DIAL_ADDR=$USERTIMELINE_SERVICE_V4_GRPC_DIAL_ADDR"
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


composepost_service_v4_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${POST_STORAGE_SERVICE_V4_GRPC_DIAL_ADDR+x}" ]; then
		if ! post_storage_service_v4_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${OTELCOL_V4_DIAL_ADDR+x}" ]; then
		if ! otelcol_v4_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${USERTIMELINE_SERVICE_V4_GRPC_DIAL_ADDR+x}" ]; then
		if ! usertimeline_service_v4_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${USER_SERVICE_V4_GRPC_DIAL_ADDR+x}" ]; then
		if ! user_service_v4_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${UNIQUEID_SERVICE_V4_GRPC_DIAL_ADDR+x}" ]; then
		if ! uniqueid_service_v4_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${MEDIA_SERVICE_V4_GRPC_DIAL_ADDR+x}" ]; then
		if ! media_service_v4_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${TEXT_SERVICE_V4_GRPC_DIAL_ADDR+x}" ]; then
		if ! text_service_v4_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${HOMETIMELINE_SERVICE_V4_GRPC_DIAL_ADDR+x}" ]; then
		if ! hometimeline_service_v4_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${COMPOSEPOST_SERVICE_V4_GRPC_BIND_ADDR+x}" ]; then
		if ! composepost_service_v4_grpc_bind_addr; then
			return $?
		fi
	fi

	run_composepost_service_v4_proc() {
		
        cd composepost_service_v4_proc
        ./composepost_service_v4_proc --post_storage_service_v4.grpc.dial_addr=$POST_STORAGE_SERVICE_V4_GRPC_DIAL_ADDR --otelcol_v4.dial_addr=$OTELCOL_V4_DIAL_ADDR --usertimeline_service_v4.grpc.dial_addr=$USERTIMELINE_SERVICE_V4_GRPC_DIAL_ADDR --user_service_v4.grpc.dial_addr=$USER_SERVICE_V4_GRPC_DIAL_ADDR --uniqueid_service_v4.grpc.dial_addr=$UNIQUEID_SERVICE_V4_GRPC_DIAL_ADDR --media_service_v4.grpc.dial_addr=$MEDIA_SERVICE_V4_GRPC_DIAL_ADDR --text_service_v4.grpc.dial_addr=$TEXT_SERVICE_V4_GRPC_DIAL_ADDR --hometimeline_service_v4.grpc.dial_addr=$HOMETIMELINE_SERVICE_V4_GRPC_DIAL_ADDR --composepost_service_v4.grpc.bind_addr=$COMPOSEPOST_SERVICE_V4_GRPC_BIND_ADDR &
        COMPOSEPOST_SERVICE_V4_PROC=$!
        return $?

	}

	if run_composepost_service_v4_proc; then
		if [ -z "${COMPOSEPOST_SERVICE_V4_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting composepost_service_v4_proc: function composepost_service_v4_proc did not set COMPOSEPOST_SERVICE_V4_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started composepost_service_v4_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting composepost_service_v4_proc due to exitcode ${exitcode} from composepost_service_v4_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running composepost_service_v4_ctr"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${COMPOSEPOST_SERVICE_V4_GRPC_BIND_ADDR+x}" ]; then
		echo "  COMPOSEPOST_SERVICE_V4_GRPC_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  COMPOSEPOST_SERVICE_V4_GRPC_BIND_ADDR=$COMPOSEPOST_SERVICE_V4_GRPC_BIND_ADDR"
	fi
	
	if [ -z "${HOMETIMELINE_SERVICE_V4_GRPC_DIAL_ADDR+x}" ]; then
		echo "  HOMETIMELINE_SERVICE_V4_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  HOMETIMELINE_SERVICE_V4_GRPC_DIAL_ADDR=$HOMETIMELINE_SERVICE_V4_GRPC_DIAL_ADDR"
	fi
	
	if [ -z "${MEDIA_SERVICE_V4_GRPC_DIAL_ADDR+x}" ]; then
		echo "  MEDIA_SERVICE_V4_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  MEDIA_SERVICE_V4_GRPC_DIAL_ADDR=$MEDIA_SERVICE_V4_GRPC_DIAL_ADDR"
	fi
	
	if [ -z "${OTELCOL_V4_DIAL_ADDR+x}" ]; then
		echo "  OTELCOL_V4_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  OTELCOL_V4_DIAL_ADDR=$OTELCOL_V4_DIAL_ADDR"
	fi
	
	if [ -z "${POST_STORAGE_SERVICE_V4_GRPC_DIAL_ADDR+x}" ]; then
		echo "  POST_STORAGE_SERVICE_V4_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  POST_STORAGE_SERVICE_V4_GRPC_DIAL_ADDR=$POST_STORAGE_SERVICE_V4_GRPC_DIAL_ADDR"
	fi
	
	if [ -z "${TEXT_SERVICE_V4_GRPC_DIAL_ADDR+x}" ]; then
		echo "  TEXT_SERVICE_V4_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  TEXT_SERVICE_V4_GRPC_DIAL_ADDR=$TEXT_SERVICE_V4_GRPC_DIAL_ADDR"
	fi
	
	if [ -z "${UNIQUEID_SERVICE_V4_GRPC_DIAL_ADDR+x}" ]; then
		echo "  UNIQUEID_SERVICE_V4_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  UNIQUEID_SERVICE_V4_GRPC_DIAL_ADDR=$UNIQUEID_SERVICE_V4_GRPC_DIAL_ADDR"
	fi
	
	if [ -z "${USER_SERVICE_V4_GRPC_DIAL_ADDR+x}" ]; then
		echo "  USER_SERVICE_V4_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USER_SERVICE_V4_GRPC_DIAL_ADDR=$USER_SERVICE_V4_GRPC_DIAL_ADDR"
	fi
	
	if [ -z "${USERTIMELINE_SERVICE_V4_GRPC_DIAL_ADDR+x}" ]; then
		echo "  USERTIMELINE_SERVICE_V4_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USERTIMELINE_SERVICE_V4_GRPC_DIAL_ADDR=$USERTIMELINE_SERVICE_V4_GRPC_DIAL_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	composepost_service_v4_proc
	
	wait
}

run_all
