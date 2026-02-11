#!/bin/bash

WORKSPACE_NAME="text_service_nt3_ctr"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${TEXT_SERVICE_NT3_GRPC_BIND_ADDR+x}" ]; then
		echo "    TEXT_SERVICE_NT3_GRPC_BIND_ADDR (missing)"
	else
		echo "    TEXT_SERVICE_NT3_GRPC_BIND_ADDR=$TEXT_SERVICE_NT3_GRPC_BIND_ADDR"
	fi
	if [ -z "${URLSHORTEN_SERVICE_NT3_GRPC_DIAL_ADDR+x}" ]; then
		echo "    URLSHORTEN_SERVICE_NT3_GRPC_DIAL_ADDR (missing)"
	else
		echo "    URLSHORTEN_SERVICE_NT3_GRPC_DIAL_ADDR=$URLSHORTEN_SERVICE_NT3_GRPC_DIAL_ADDR"
	fi
	if [ -z "${USERMENTION_SERVICE_NT3_GRPC_DIAL_ADDR+x}" ]; then
		echo "    USERMENTION_SERVICE_NT3_GRPC_DIAL_ADDR (missing)"
	else
		echo "    USERMENTION_SERVICE_NT3_GRPC_DIAL_ADDR=$USERMENTION_SERVICE_NT3_GRPC_DIAL_ADDR"
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


text_service_nt3_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${URLSHORTEN_SERVICE_NT3_GRPC_DIAL_ADDR+x}" ]; then
		if ! urlshorten_service_nt3_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${USERMENTION_SERVICE_NT3_GRPC_DIAL_ADDR+x}" ]; then
		if ! usermention_service_nt3_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${TEXT_SERVICE_NT3_GRPC_BIND_ADDR+x}" ]; then
		if ! text_service_nt3_grpc_bind_addr; then
			return $?
		fi
	fi

	run_text_service_nt3_proc() {
		
        cd text_service_nt3_proc
        ./text_service_nt3_proc --urlshorten_service_nt3.grpc.dial_addr=$URLSHORTEN_SERVICE_NT3_GRPC_DIAL_ADDR --usermention_service_nt3.grpc.dial_addr=$USERMENTION_SERVICE_NT3_GRPC_DIAL_ADDR --text_service_nt3.grpc.bind_addr=$TEXT_SERVICE_NT3_GRPC_BIND_ADDR &
        TEXT_SERVICE_NT3_PROC=$!
        return $?

	}

	if run_text_service_nt3_proc; then
		if [ -z "${TEXT_SERVICE_NT3_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting text_service_nt3_proc: function text_service_nt3_proc did not set TEXT_SERVICE_NT3_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started text_service_nt3_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting text_service_nt3_proc due to exitcode ${exitcode} from text_service_nt3_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running text_service_nt3_ctr"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${TEXT_SERVICE_NT3_GRPC_BIND_ADDR+x}" ]; then
		echo "  TEXT_SERVICE_NT3_GRPC_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  TEXT_SERVICE_NT3_GRPC_BIND_ADDR=$TEXT_SERVICE_NT3_GRPC_BIND_ADDR"
	fi
	
	if [ -z "${URLSHORTEN_SERVICE_NT3_GRPC_DIAL_ADDR+x}" ]; then
		echo "  URLSHORTEN_SERVICE_NT3_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  URLSHORTEN_SERVICE_NT3_GRPC_DIAL_ADDR=$URLSHORTEN_SERVICE_NT3_GRPC_DIAL_ADDR"
	fi
	
	if [ -z "${USERMENTION_SERVICE_NT3_GRPC_DIAL_ADDR+x}" ]; then
		echo "  USERMENTION_SERVICE_NT3_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USERMENTION_SERVICE_NT3_GRPC_DIAL_ADDR=$USERMENTION_SERVICE_NT3_GRPC_DIAL_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	text_service_nt3_proc
	
	wait
}

run_all
