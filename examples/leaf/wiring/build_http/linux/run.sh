#!/bin/bash

WORKSPACE_NAME="linux"
WORKSPACE_DIR=$(pwd)

usage() { 
	echo "Usage: $0 [-h]" 1>&2
	echo "  Required environment variables:"
	
	if [ -z "${LEAF_DB_DIAL_ADDR+x}" ]; then
		echo "    LEAF_DB_DIAL_ADDR (missing)"
	else
		echo "    LEAF_DB_DIAL_ADDR=$LEAF_DB_DIAL_ADDR"
	fi
	if [ -z "${LEAF_SERVICE_HTTP_BIND_ADDR+x}" ]; then
		echo "    LEAF_SERVICE_HTTP_BIND_ADDR (missing)"
	else
		echo "    LEAF_SERVICE_HTTP_BIND_ADDR=$LEAF_SERVICE_HTTP_BIND_ADDR"
	fi
	if [ -z "${LEAF_SERVICE_HTTP_DIAL_ADDR+x}" ]; then
		echo "    LEAF_SERVICE_HTTP_DIAL_ADDR (missing)"
	else
		echo "    LEAF_SERVICE_HTTP_DIAL_ADDR=$LEAF_SERVICE_HTTP_DIAL_ADDR"
	fi
	if [ -z "${NONLEAF_SERVICE_HTTP_BIND_ADDR+x}" ]; then
		echo "    NONLEAF_SERVICE_HTTP_BIND_ADDR (missing)"
	else
		echo "    NONLEAF_SERVICE_HTTP_BIND_ADDR=$NONLEAF_SERVICE_HTTP_BIND_ADDR"
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


leaf_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${LEAF_DB_DIAL_ADDR+x}" ]; then
		if ! leaf_db_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${ZIPKIN_DIAL_ADDR+x}" ]; then
		if ! zipkin_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${LEAF_SERVICE_HTTP_BIND_ADDR+x}" ]; then
		if ! leaf_service_http_bind_addr; then
			return $?
		fi
	fi

	run_leaf_proc() {
		
        export CGO_ENABLED=1
        cd leaf_proc/leaf_proc
        go run . --leaf_db.dial_addr=$LEAF_DB_DIAL_ADDR --zipkin.dial_addr=$ZIPKIN_DIAL_ADDR --leaf_service.http.bind_addr=$LEAF_SERVICE_HTTP_BIND_ADDR &
        LEAF_PROC=$!
        return $?

	}

	if run_leaf_proc; then
		if [ -z "${LEAF_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting leaf_proc: function leaf_proc did not set LEAF_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started leaf_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting leaf_proc due to exitcode ${exitcode} from leaf_proc"
		return $exitcode
	fi
}

nonleaf_proc() {
	cd $WORKSPACE_DIR
	
	if [ -z "${LEAF_SERVICE_HTTP_DIAL_ADDR+x}" ]; then
		if ! leaf_service_http_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${ZIPKIN_DIAL_ADDR+x}" ]; then
		if ! zipkin_dial_addr; then
			return $?
		fi
	fi

	if [ -z "${NONLEAF_SERVICE_HTTP_BIND_ADDR+x}" ]; then
		if ! nonleaf_service_http_bind_addr; then
			return $?
		fi
	fi

	run_nonleaf_proc() {
		
        export CGO_ENABLED=1
        cd nonleaf_proc/nonleaf_proc
        go run . --leaf_service.http.dial_addr=$LEAF_SERVICE_HTTP_DIAL_ADDR --zipkin.dial_addr=$ZIPKIN_DIAL_ADDR --nonleaf_service.http.bind_addr=$NONLEAF_SERVICE_HTTP_BIND_ADDR &
        NONLEAF_PROC=$!
        return $?

	}

	if run_nonleaf_proc; then
		if [ -z "${NONLEAF_PROC+x}" ]; then
			echo "${WORKSPACE_NAME} error starting nonleaf_proc: function nonleaf_proc did not set NONLEAF_PROC"
			return 1
		else
			echo "${WORKSPACE_NAME} started nonleaf_proc"
			return 0
		fi
	else
		exitcode=$?
		echo "${WORKSPACE_NAME} aborting nonleaf_proc due to exitcode ${exitcode} from nonleaf_proc"
		return $exitcode
	fi
}


run_all() {
	echo "Running linux"

	# Check that all necessary environment variables are set
	echo "Required environment variables:"
	missing_vars=0
	if [ -z "${LEAF_DB_DIAL_ADDR+x}" ]; then
		echo "  LEAF_DB_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  LEAF_DB_DIAL_ADDR=$LEAF_DB_DIAL_ADDR"
	fi
	
	if [ -z "${LEAF_SERVICE_HTTP_BIND_ADDR+x}" ]; then
		echo "  LEAF_SERVICE_HTTP_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  LEAF_SERVICE_HTTP_BIND_ADDR=$LEAF_SERVICE_HTTP_BIND_ADDR"
	fi
	
	if [ -z "${LEAF_SERVICE_HTTP_DIAL_ADDR+x}" ]; then
		echo "  LEAF_SERVICE_HTTP_DIAL_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  LEAF_SERVICE_HTTP_DIAL_ADDR=$LEAF_SERVICE_HTTP_DIAL_ADDR"
	fi
	
	if [ -z "${NONLEAF_SERVICE_HTTP_BIND_ADDR+x}" ]; then
		echo "  NONLEAF_SERVICE_HTTP_BIND_ADDR (missing)"
		missing_vars=$((missing_vars+1))
	else
		echo "  NONLEAF_SERVICE_HTTP_BIND_ADDR=$NONLEAF_SERVICE_HTTP_BIND_ADDR"
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

	leaf_proc
	nonleaf_proc
	
	wait
}

run_all
