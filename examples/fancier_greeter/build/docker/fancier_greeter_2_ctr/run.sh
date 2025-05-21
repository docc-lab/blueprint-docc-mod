#!/bin/bash

WORKSPACE_NAME="fancier_greeter_2_ctr"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${FANCIER_GREETER_2_HTTP_BIND_ADDR+x}" ]; then
		echo "    FANCIER_GREETER_2_HTTP_BIND_ADDR (missing)"
	else
		echo "    FANCIER_GREETER_2_HTTP_BIND_ADDR=$FANCIER_GREETER_2_HTTP_BIND_ADDR"
	fi
	if [ -z "${FANCIER_GREETER_2_BASIC_GREETER_GRPC_DIAL_ADDR+x}" ]; then
		echo "    FANCIER_GREETER_2_BASIC_GREETER_GRPC_DIAL_ADDR (missing)"
	else
		echo "    FANCIER_GREETER_2_BASIC_GREETER_GRPC_DIAL_ADDR=$FANCIER_GREETER_2_BASIC_GREETER_GRPC_DIAL_ADDR"
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


fancier_greeter_2_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${FANCIER_GREETER_2_BASIC_GREETER_GRPC_DIAL_ADDR+x}" ]; then
		if ! fancier_greeter_2_basic_greeter_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${ZIPKIN_DIAL_ADDR+x}" ]; then
		if ! zipkin_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${FANCIER_GREETER_2_HTTP_BIND_ADDR+x}" ]; then
		if ! fancier_greeter_2_http_bind_addr; then
			return $?
		fi
	fi

	run_fancier_greeter_2_proc() {
		
        cd fancier_greeter_2_proc
        ./fancier_greeter_2_proc --fancier_greeter_2__basic_greeter.grpc.dial_addr=$FANCIER_GREETER_2_BASIC_GREETER_GRPC_DIAL_ADDR --zipkin.dial_addr=$ZIPKIN_DIAL_ADDR --fancier_greeter_2.http.bind_addr=$FANCIER_GREETER_2_HTTP_BIND_ADDR &
        FANCIER_GREETER_2_PROC=$!
        return $?

	}

	if run_fancier_greeter_2_proc; then
		if [ -z "${FANCIER_GREETER_2_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting fancier_greeter_2_proc: function fancier_greeter_2_proc did not set FANCIER_GREETER_2_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started fancier_greeter_2_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting fancier_greeter_2_proc due to exitcode ${exitcode} from fancier_greeter_2_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running fancier_greeter_2_ctr"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${FANCIER_GREETER_2_HTTP_BIND_ADDR+x}" ]; then
		echo "  FANCIER_GREETER_2_HTTP_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  FANCIER_GREETER_2_HTTP_BIND_ADDR=$FANCIER_GREETER_2_HTTP_BIND_ADDR"
	fi
	
	if [ -z "${FANCIER_GREETER_2_BASIC_GREETER_GRPC_DIAL_ADDR+x}" ]; then
		echo "  FANCIER_GREETER_2_BASIC_GREETER_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  FANCIER_GREETER_2_BASIC_GREETER_GRPC_DIAL_ADDR=$FANCIER_GREETER_2_BASIC_GREETER_GRPC_DIAL_ADDR"
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

	fancier_greeter_2_proc
	
	wait
}

run_all
