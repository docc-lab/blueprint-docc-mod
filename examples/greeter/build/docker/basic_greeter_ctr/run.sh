#!/bin/bash

WORKSPACE_NAME="basic_greeter_ctr"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${BASIC_GREETER_GRPC_BIND_ADDR+x}" ]; then
		echo "    BASIC_GREETER_GRPC_BIND_ADDR (missing)"
	else
		echo "    BASIC_GREETER_GRPC_BIND_ADDR=$BASIC_GREETER_GRPC_BIND_ADDR"
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


basic_greeter_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${ZIPKIN_DIAL_ADDR+x}" ]; then
		if ! zipkin_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${BASIC_GREETER_GRPC_BIND_ADDR+x}" ]; then
		if ! basic_greeter_grpc_bind_addr; then
			return $?
		fi
	fi

	run_basic_greeter_proc() {
		
        cd basic_greeter_proc
        ./basic_greeter_proc --zipkin.dial_addr=$ZIPKIN_DIAL_ADDR --basic_greeter.grpc.bind_addr=$BASIC_GREETER_GRPC_BIND_ADDR &
        BASIC_GREETER_PROC=$!
        return $?

	}

	if run_basic_greeter_proc; then
		if [ -z "${BASIC_GREETER_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting basic_greeter_proc: function basic_greeter_proc did not set BASIC_GREETER_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started basic_greeter_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting basic_greeter_proc due to exitcode ${exitcode} from basic_greeter_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running basic_greeter_ctr"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${BASIC_GREETER_GRPC_BIND_ADDR+x}" ]; then
		echo "  BASIC_GREETER_GRPC_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  BASIC_GREETER_GRPC_BIND_ADDR=$BASIC_GREETER_GRPC_BIND_ADDR"
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

	basic_greeter_proc
	
	wait
}

run_all
