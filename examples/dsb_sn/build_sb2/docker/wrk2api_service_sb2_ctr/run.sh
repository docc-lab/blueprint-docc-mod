#!/bin/bash

WORKSPACE_NAME="wrk2api_service_sb2_ctr"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${COMPOSEPOST_SERVICE_SB2_GRPC_DIAL_ADDR+x}" ]; then
		echo "    COMPOSEPOST_SERVICE_SB2_GRPC_DIAL_ADDR (missing)"
	else
		echo "    COMPOSEPOST_SERVICE_SB2_GRPC_DIAL_ADDR=$COMPOSEPOST_SERVICE_SB2_GRPC_DIAL_ADDR"
	fi
	if [ -z "${HOMETIMELINE_SERVICE_SB2_GRPC_DIAL_ADDR+x}" ]; then
		echo "    HOMETIMELINE_SERVICE_SB2_GRPC_DIAL_ADDR (missing)"
	else
		echo "    HOMETIMELINE_SERVICE_SB2_GRPC_DIAL_ADDR=$HOMETIMELINE_SERVICE_SB2_GRPC_DIAL_ADDR"
	fi
	if [ -z "${OTELCOL_SB2_DIAL_ADDR+x}" ]; then
		echo "    OTELCOL_SB2_DIAL_ADDR (missing)"
	else
		echo "    OTELCOL_SB2_DIAL_ADDR=$OTELCOL_SB2_DIAL_ADDR"
	fi
	if [ -z "${SOCIALGRAPH_SERVICE_SB2_GRPC_DIAL_ADDR+x}" ]; then
		echo "    SOCIALGRAPH_SERVICE_SB2_GRPC_DIAL_ADDR (missing)"
	else
		echo "    SOCIALGRAPH_SERVICE_SB2_GRPC_DIAL_ADDR=$SOCIALGRAPH_SERVICE_SB2_GRPC_DIAL_ADDR"
	fi
	if [ -z "${USER_SERVICE_SB2_GRPC_DIAL_ADDR+x}" ]; then
		echo "    USER_SERVICE_SB2_GRPC_DIAL_ADDR (missing)"
	else
		echo "    USER_SERVICE_SB2_GRPC_DIAL_ADDR=$USER_SERVICE_SB2_GRPC_DIAL_ADDR"
	fi
	if [ -z "${USERTIMELINE_SERVICE_SB2_GRPC_DIAL_ADDR+x}" ]; then
		echo "    USERTIMELINE_SERVICE_SB2_GRPC_DIAL_ADDR (missing)"
	else
		echo "    USERTIMELINE_SERVICE_SB2_GRPC_DIAL_ADDR=$USERTIMELINE_SERVICE_SB2_GRPC_DIAL_ADDR"
	fi
	if [ -z "${WRK2API_SERVICE_SB2_HTTP_BIND_ADDR+x}" ]; then
		echo "    WRK2API_SERVICE_SB2_HTTP_BIND_ADDR (missing)"
	else
		echo "    WRK2API_SERVICE_SB2_HTTP_BIND_ADDR=$WRK2API_SERVICE_SB2_HTTP_BIND_ADDR"
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


wrk2api_service_sb2_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${USER_SERVICE_SB2_GRPC_DIAL_ADDR+x}" ]; then
		if ! user_service_sb2_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${OTELCOL_SB2_DIAL_ADDR+x}" ]; then
		if ! otelcol_sb2_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${COMPOSEPOST_SERVICE_SB2_GRPC_DIAL_ADDR+x}" ]; then
		if ! composepost_service_sb2_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${USERTIMELINE_SERVICE_SB2_GRPC_DIAL_ADDR+x}" ]; then
		if ! usertimeline_service_sb2_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${HOMETIMELINE_SERVICE_SB2_GRPC_DIAL_ADDR+x}" ]; then
		if ! hometimeline_service_sb2_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${SOCIALGRAPH_SERVICE_SB2_GRPC_DIAL_ADDR+x}" ]; then
		if ! socialgraph_service_sb2_grpc_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${WRK2API_SERVICE_SB2_HTTP_BIND_ADDR+x}" ]; then
		if ! wrk2api_service_sb2_http_bind_addr; then
			return $?
		fi
	fi

	run_wrk2api_service_sb2_proc() {
		
        cd wrk2api_service_sb2_proc
        ./wrk2api_service_sb2_proc --user_service_sb2.grpc.dial_addr=$USER_SERVICE_SB2_GRPC_DIAL_ADDR --otelcol_sb2.dial_addr=$OTELCOL_SB2_DIAL_ADDR --composepost_service_sb2.grpc.dial_addr=$COMPOSEPOST_SERVICE_SB2_GRPC_DIAL_ADDR --usertimeline_service_sb2.grpc.dial_addr=$USERTIMELINE_SERVICE_SB2_GRPC_DIAL_ADDR --hometimeline_service_sb2.grpc.dial_addr=$HOMETIMELINE_SERVICE_SB2_GRPC_DIAL_ADDR --socialgraph_service_sb2.grpc.dial_addr=$SOCIALGRAPH_SERVICE_SB2_GRPC_DIAL_ADDR --wrk2api_service_sb2.http.bind_addr=$WRK2API_SERVICE_SB2_HTTP_BIND_ADDR &
        WRK2API_SERVICE_SB2_PROC=$!
        return $?

	}

	if run_wrk2api_service_sb2_proc; then
		if [ -z "${WRK2API_SERVICE_SB2_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting wrk2api_service_sb2_proc: function wrk2api_service_sb2_proc did not set WRK2API_SERVICE_SB2_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started wrk2api_service_sb2_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting wrk2api_service_sb2_proc due to exitcode ${exitcode} from wrk2api_service_sb2_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running wrk2api_service_sb2_ctr"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${COMPOSEPOST_SERVICE_SB2_GRPC_DIAL_ADDR+x}" ]; then
		echo "  COMPOSEPOST_SERVICE_SB2_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  COMPOSEPOST_SERVICE_SB2_GRPC_DIAL_ADDR=$COMPOSEPOST_SERVICE_SB2_GRPC_DIAL_ADDR"
	fi
	
	if [ -z "${HOMETIMELINE_SERVICE_SB2_GRPC_DIAL_ADDR+x}" ]; then
		echo "  HOMETIMELINE_SERVICE_SB2_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  HOMETIMELINE_SERVICE_SB2_GRPC_DIAL_ADDR=$HOMETIMELINE_SERVICE_SB2_GRPC_DIAL_ADDR"
	fi
	
	if [ -z "${OTELCOL_SB2_DIAL_ADDR+x}" ]; then
		echo "  OTELCOL_SB2_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  OTELCOL_SB2_DIAL_ADDR=$OTELCOL_SB2_DIAL_ADDR"
	fi
	
	if [ -z "${SOCIALGRAPH_SERVICE_SB2_GRPC_DIAL_ADDR+x}" ]; then
		echo "  SOCIALGRAPH_SERVICE_SB2_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  SOCIALGRAPH_SERVICE_SB2_GRPC_DIAL_ADDR=$SOCIALGRAPH_SERVICE_SB2_GRPC_DIAL_ADDR"
	fi
	
	if [ -z "${USER_SERVICE_SB2_GRPC_DIAL_ADDR+x}" ]; then
		echo "  USER_SERVICE_SB2_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USER_SERVICE_SB2_GRPC_DIAL_ADDR=$USER_SERVICE_SB2_GRPC_DIAL_ADDR"
	fi
	
	if [ -z "${USERTIMELINE_SERVICE_SB2_GRPC_DIAL_ADDR+x}" ]; then
		echo "  USERTIMELINE_SERVICE_SB2_GRPC_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  USERTIMELINE_SERVICE_SB2_GRPC_DIAL_ADDR=$USERTIMELINE_SERVICE_SB2_GRPC_DIAL_ADDR"
	fi
	
	if [ -z "${WRK2API_SERVICE_SB2_HTTP_BIND_ADDR+x}" ]; then
		echo "  WRK2API_SERVICE_SB2_HTTP_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  WRK2API_SERVICE_SB2_HTTP_BIND_ADDR=$WRK2API_SERVICE_SB2_HTTP_BIND_ADDR"
	fi
		

	if [ "$missing_vars" -gt 0 ]; then
		echo "Aborting due to missing environment variables"
		return 1
	fi

	wrk2api_service_sb2_proc
	
	wait
}

run_all
