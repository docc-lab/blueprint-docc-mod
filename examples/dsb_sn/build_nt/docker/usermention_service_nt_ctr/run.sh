#!/bin/bash

WORKSPACE_NAME="usermention_service_nt_ctr"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${USER_CACHE_NT_DIAL_ADDR+x}" ]; then
		echo "    USER_CACHE_NT_DIAL_ADDR (missing)"
	else
		echo "    USER_CACHE_NT_DIAL_ADDR=$USER_CACHE_NT_DIAL_ADDR"
	fi
	if [ -z "${USER_DB_NT_DIAL_ADDR+x}" ]; then
		echo "    USER_DB_NT_DIAL_ADDR (missing)"
	else
		echo "    USER_DB_NT_DIAL_ADDR=$USER_DB_NT_DIAL_ADDR"
	fi
	if [ -z "${USERMENTION_SERVICE_NT_GRPC_BIND_ADDR+x}" ]; then
		echo "    USERMENTION_SERVICE_NT_GRPC_BIND_ADDR (missing)"
	else
		echo "    USERMENTION_SERVICE_NT_GRPC_BIND_ADDR=$USERMENTION_SERVICE_NT_GRPC_BIND_ADDR"
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


usermention_service_nt_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${USER_CACHE_NT_DIAL_ADDR+x}" ]; then
		if ! user_cache_nt_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${USER_DB_NT_DIAL_ADDR+x}" ]; then
		if ! user_db_nt_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${USERMENTION_SERVICE_NT_GRPC_BIND_ADDR+x}" ]; then
		if ! usermention_service_nt_grpc_bind_addr; then
			return $?
		fi
	fi

	run_usermention_service_nt_proc() {
		
        cd usermention_service_nt_proc
        ./usermention_service_nt_proc --user_cache_nt.dial_addr=$USER_CACHE_NT_DIAL_ADDR --user_db_nt.dial_addr=$USER_DB_NT_DIAL_ADDR --usermention_service_nt.grpc.bind_addr=$USERMENTION_SERVICE_NT_GRPC_BIND_ADDR &
        USERMENTION_SERVICE_NT_PROC=$!
        return $?

	}

	if run_usermention_service_nt_proc; then
		if [ -z "${USERMENTION_SERVICE_NT_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting usermention_service_nt_proc: function usermention_service_nt_proc did not set USERMENTION_SERVICE_NT_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started usermention_service_nt_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting usermention_service_nt_proc due to exitcode ${exitcode} from usermention_service_nt_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running usermention_service_nt_ctr"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${USER_CACHE_NT_DIAL_ADDR+x}" ]; then
		echo "  USER_CACHE_NT_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USER_CACHE_NT_DIAL_ADDR=$USER_CACHE_NT_DIAL_ADDR"
	fi
	
	if [ -z "${USER_DB_NT_DIAL_ADDR+x}" ]; then
		echo "  USER_DB_NT_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USER_DB_NT_DIAL_ADDR=$USER_DB_NT_DIAL_ADDR"
	fi
	
	if [ -z "${USERMENTION_SERVICE_NT_GRPC_BIND_ADDR+x}" ]; then
		echo "  USERMENTION_SERVICE_NT_GRPC_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USERMENTION_SERVICE_NT_GRPC_BIND_ADDR=$USERMENTION_SERVICE_NT_GRPC_BIND_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	usermention_service_nt_proc
	
	wait
}

run_all
