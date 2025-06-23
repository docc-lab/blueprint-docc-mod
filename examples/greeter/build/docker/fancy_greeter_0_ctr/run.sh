#!/bin/bash

WORKSPACE_NAME="fancy_greeter_0_ctr"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${BASIC_GREETER_GRPC_DIAL_ADDR+x}" ]; then
		echo "    BASIC_GREETER_GRPC_DIAL_ADDR (missing)"
	else
		echo "    BASIC_GREETER_GRPC_DIAL_ADDR=$BASIC_GREETER_GRPC_DIAL_ADDR"
	fi
	if [ -z "${FANCY_GREETER_0_HTTP_BIND_ADDR+x}" ]; then
		echo "    FANCY_GREETER_0_HTTP_BIND_ADDR (missing)"
	else
		echo "    FANCY_GREETER_0_HTTP_BIND_ADDR=$FANCY_GREETER_0_HTTP_BIND_ADDR"
	fi
	if [ -z "${ZIPKIN_DIAL_ADDR+x}" ]; then
		echo "    ZIPKIN_DIAL_ADDR (missing)"
	else
		echo "    ZIPKIN_DIAL_ADDR=$ZIPKIN_DIAL_ADDR"
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


fancy_greeter_0_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${BASIC_GREETER_GRPC_DIAL_ADDR+x}" ]; then
		if ! basic_greeter_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${ZIPKIN_DIAL_ADDR+x}" ]; then
		if ! zipkin_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${FANCY_GREETER_0_HTTP_BIND_ADDR+x}" ]; then
		if ! fancy_greeter_0_http_bind_addr; then
			return $?
		fi
	fi

	run_fancy_greeter_0_proc() {
		
        cd fancy_greeter_0_proc
        ./fancy_greeter_0_proc --basic_greeter.grpc.dial_addr=$BASIC_GREETER_GRPC_DIAL_ADDR --zipkin.dial_addr=$ZIPKIN_DIAL_ADDR --fancy_greeter_0.http.bind_addr=$FANCY_GREETER_0_HTTP_BIND_ADDR &
        FANCY_GREETER_0_PROC=$!
        return $?

	}

	if run_fancy_greeter_0_proc; then
		if [ -z "${FANCY_GREETER_0_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting fancy_greeter_0_proc: function fancy_greeter_0_proc did not set FANCY_GREETER_0_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started fancy_greeter_0_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting fancy_greeter_0_proc due to exitcode ${exitcode} from fancy_greeter_0_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running fancy_greeter_0_ctr"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${BASIC_GREETER_GRPC_DIAL_ADDR+x}" ]; then
		echo "  BASIC_GREETER_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  BASIC_GREETER_GRPC_DIAL_ADDR=$BASIC_GREETER_GRPC_DIAL_ADDR"
	fi
	
	if [ -z "${FANCY_GREETER_0_HTTP_BIND_ADDR+x}" ]; then
		echo "  FANCY_GREETER_0_HTTP_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  FANCY_GREETER_0_HTTP_BIND_ADDR=$FANCY_GREETER_0_HTTP_BIND_ADDR"
	fi
	
	if [ -z "${ZIPKIN_DIAL_ADDR+x}" ]; then
		echo "  ZIPKIN_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  ZIPKIN_DIAL_ADDR=$ZIPKIN_DIAL_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	fancy_greeter_0_proc
	
	wait
}

run_all
