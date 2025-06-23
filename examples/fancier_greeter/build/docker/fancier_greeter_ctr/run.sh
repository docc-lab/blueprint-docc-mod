#!/bin/bash

WORKSPACE_NAME="fancier_greeter_ctr"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${FANCIER_GREETER_HTTP_BIND_ADDR+x}" ]; then
		echo "    FANCIER_GREETER_HTTP_BIND_ADDR (missing)"
	else
		echo "    FANCIER_GREETER_HTTP_BIND_ADDR=$FANCIER_GREETER_HTTP_BIND_ADDR"
	fi
	if [ -z "${FANCIER_GREETER_BASIC_GREETER_GRPC_DIAL_ADDR+x}" ]; then
		echo "    FANCIER_GREETER_BASIC_GREETER_GRPC_DIAL_ADDR (missing)"
	else
		echo "    FANCIER_GREETER_BASIC_GREETER_GRPC_DIAL_ADDR=$FANCIER_GREETER_BASIC_GREETER_GRPC_DIAL_ADDR"
	fi
	if [ -z "${TRACING_AGENT_DIAL_ADDR+x}" ]; then
		echo "    TRACING_AGENT_DIAL_ADDR (missing)"
	else
		echo "    TRACING_AGENT_DIAL_ADDR=$TRACING_AGENT_DIAL_ADDR"
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


fancier_greeter_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${FANCIER_GREETER_BASIC_GREETER_GRPC_DIAL_ADDR+x}" ]; then
		if ! fancier_greeter_basic_greeter_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${TRACING_AGENT_DIAL_ADDR+x}" ]; then
		if ! tracing_agent_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${FANCIER_GREETER_HTTP_BIND_ADDR+x}" ]; then
		if ! fancier_greeter_http_bind_addr; then
			return $?
		fi
	fi

	run_fancier_greeter_proc() {
		
        cd fancier_greeter_proc
        ./fancier_greeter_proc --fancier_greeter__basic_greeter.grpc.dial_addr=$FANCIER_GREETER_BASIC_GREETER_GRPC_DIAL_ADDR --tracing_agent.dial_addr=$TRACING_AGENT_DIAL_ADDR --fancier_greeter.http.bind_addr=$FANCIER_GREETER_HTTP_BIND_ADDR &
        FANCIER_GREETER_PROC=$!
        return $?

	}

	if run_fancier_greeter_proc; then
		if [ -z "${FANCIER_GREETER_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting fancier_greeter_proc: function fancier_greeter_proc did not set FANCIER_GREETER_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started fancier_greeter_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting fancier_greeter_proc due to exitcode ${exitcode} from fancier_greeter_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running fancier_greeter_ctr"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${FANCIER_GREETER_HTTP_BIND_ADDR+x}" ]; then
		echo "  FANCIER_GREETER_HTTP_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  FANCIER_GREETER_HTTP_BIND_ADDR=$FANCIER_GREETER_HTTP_BIND_ADDR"
	fi
	
	if [ -z "${FANCIER_GREETER_BASIC_GREETER_GRPC_DIAL_ADDR+x}" ]; then
		echo "  FANCIER_GREETER_BASIC_GREETER_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  FANCIER_GREETER_BASIC_GREETER_GRPC_DIAL_ADDR=$FANCIER_GREETER_BASIC_GREETER_GRPC_DIAL_ADDR"
	fi
	
	if [ -z "${TRACING_AGENT_DIAL_ADDR+x}" ]; then
		echo "  TRACING_AGENT_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  TRACING_AGENT_DIAL_ADDR=$TRACING_AGENT_DIAL_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	fancier_greeter_proc
	
	wait
}

run_all
