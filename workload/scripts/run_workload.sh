#!/bin/bash

# Blueprint Workload Testing Suite Runner Script
# This script runs the complete workload testing suite

set -e

# Default configuration
FRONTEND_URL="${FRONTEND_URL:-http://192.168.64.11:32170}"
CATALOGUE_SIZE="${CATALOGUE_SIZE:-100}"
USER_COUNT="${USER_COUNT:-50}"
WORKLOAD_USERS="${WORKLOAD_USERS:-10}"
WORKLOAD_DURATION="${WORKLOAD_DURATION:-5m}"
WORKLOAD_MIX="${WORKLOAD_MIX:-realistic}"
OUTPUT_DIR="${OUTPUT_DIR:-./results}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Function to check if frontend is accessible
check_frontend() {
    print_status "Checking frontend accessibility..."
    if curl -s --connect-timeout 5 "$FRONTEND_URL/ListItems?tags=&order=&pageNum=1&pageSize=1" > /dev/null; then
        print_success "Frontend is accessible at $FRONTEND_URL"
    else
        print_error "Frontend is not accessible at $FRONTEND_URL"
        print_error "Please ensure SockShop is deployed and running"
        exit 1
    fi
}

# Function to create output directory
setup_output() {
    print_status "Setting up output directory: $OUTPUT_DIR"
    mkdir -p "$OUTPUT_DIR"
    timestamp=$(date +"%Y%m%d_%H%M%S")
    OUTPUT_DIR="$OUTPUT_DIR/workload_$timestamp"
    mkdir -p "$OUTPUT_DIR"
    print_success "Output directory created: $OUTPUT_DIR"
}

# Function to run initialization
run_initialization() {
    print_status "Running data initialization..."
    print_status "Catalogue size: $CATALOGUE_SIZE items"
    print_status "User count: $USER_COUNT users"
    
    cd "$(dirname "$0")/.."
    go run cmd/init-data/main.go \
        -frontend-url="$FRONTEND_URL" \
        -catalogue-size="$CATALOGUE_SIZE" \
        -user-count="$USER_COUNT" \
        -seed=42 \
        -verbose=true \
        2>&1 | tee "$OUTPUT_DIR/init.log"
    
    if [ ${PIPESTATUS[0]} -eq 0 ]; then
        print_success "Data initialization completed successfully"
    else
        print_error "Data initialization failed"
        exit 1
    fi
}

# Function to run workload
run_workload() {
    print_status "Running e-commerce workload..."
    print_status "Concurrent users: $WORKLOAD_USERS"
    print_status "Duration: $WORKLOAD_DURATION"
    print_status "Workload mix: $WORKLOAD_MIX"
    
    cd "$(dirname "$0")/.."
    go run cmd/ecommerce-workload/main.go \
        -frontend-url="$FRONTEND_URL" \
        -users="$WORKLOAD_USERS" \
        -duration="$WORKLOAD_DURATION" \
        -think-time=2s \
        -mix="$WORKLOAD_MIX" \
        -output="$OUTPUT_DIR/workload_stats.json" \
        -verbose=true \
        2>&1 | tee "$OUTPUT_DIR/workload.log"
    
    if [ ${PIPESTATUS[0]} -eq 0 ]; then
        print_success "Workload completed successfully"
    else
        print_error "Workload failed"
        exit 1
    fi
}

# Function to generate summary report
generate_report() {
    print_status "Generating summary report..."
    
    cat > "$OUTPUT_DIR/summary.md" << EOF
# Blueprint Workload Test Results

**Test Date:** $(date)
**Frontend URL:** $FRONTEND_URL
**Catalogue Size:** $CATALOGUE_SIZE items
**Pre-created Users:** $USER_COUNT users
**Workload Users:** $WORKLOAD_USERS concurrent users
**Workload Duration:** $WORKLOAD_DURATION
**Workload Mix:** $WORKLOAD_MIX

## Files Generated

- \`init.log\` - Data initialization log
- \`workload.log\` - Workload execution log
- \`workload_stats.json\` - Detailed statistics in JSON format
- \`summary.md\` - This summary report

## Key Metrics

Check the \`workload_stats.json\` file for detailed metrics including:
- Total requests and success rate
- Average, min, and max latency
- Operation statistics
- Service hop distribution
- Individual request details

## Deep Workflow Analysis

The workload tests deep workflows that traverse multiple service hops:

1. **Browse Catalogue** (2 hops): Frontend → Catalogue
2. **Add to Cart** (3 hops): Frontend → Catalogue → Cart
3. **Register User** (2 hops): Frontend → User
4. **Add Address/Payment** (2 hops): Frontend → User
5. **Place Order** (6 hops): Frontend → Order → User → Cart → Payment → Shipping
6. **Check Orders** (2 hops): Frontend → Order

## Service Hop Distribution

The workload generates requests with different service hop counts:
- 2 hops: Simple operations (browse, register, check orders)
- 3 hops: Cart operations (add to cart)
- 6 hops: Complex order processing (place order)

This provides comprehensive testing of distributed tracing across multiple service boundaries.

## Research Value

This workload suite is specifically designed for:
- **Distributed Tracing Research**: Testing span reconstruction across service boundaries
- **Observability Analysis**: Understanding service interaction patterns
- **Performance Testing**: Identifying bottlenecks in multi-service workflows
- **Error Propagation**: Testing how errors propagate through service chains
EOF

    print_success "Summary report generated: $OUTPUT_DIR/summary.md"
}

# Function to show usage
show_usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  -h, --help              Show this help message"
    echo "  --frontend-url URL      Frontend service URL (default: http://192.168.64.11:32170)"
    echo "  --catalogue-size N      Number of catalogue items (default: 100)"
    echo "  --user-count N          Number of users to pre-create (default: 50)"
    echo "  --workload-users N      Number of concurrent workload users (default: 10)"
    echo "  --duration DURATION     Workload duration (default: 5m)"
    echo "  --mix MIX               Workload mix: realistic, browsing, purchasing, stress (default: realistic)"
    echo "  --output-dir DIR        Output directory (default: ./results)"
    echo "  --skip-init             Skip data initialization"
    echo "  --init-only             Only run data initialization"
    echo ""
    echo "Environment Variables:"
    echo "  FRONTEND_URL            Frontend service URL"
    echo "  CATALOGUE_SIZE          Number of catalogue items"
    echo "  USER_COUNT              Number of users to pre-create"
    echo "  WORKLOAD_USERS          Number of concurrent workload users"
    echo "  WORKLOAD_DURATION       Workload duration"
    echo "  WORKLOAD_MIX            Workload mix"
    echo "  OUTPUT_DIR              Output directory"
    echo ""
    echo "Examples:"
    echo "  $0 --workload-users 20 --duration 10m --mix stress"
    echo "  $0 --catalogue-size 200 --user-count 100"
    echo "  $0 --skip-init --workload-users 5 --duration 2m"
}

# Parse command line arguments
SKIP_INIT=false
INIT_ONLY=false

while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_usage
            exit 0
            ;;
        --frontend-url)
            FRONTEND_URL="$2"
            shift 2
            ;;
        --catalogue-size)
            CATALOGUE_SIZE="$2"
            shift 2
            ;;
        --user-count)
            USER_COUNT="$2"
            shift 2
            ;;
        --workload-users)
            WORKLOAD_USERS="$2"
            shift 2
            ;;
        --duration)
            WORKLOAD_DURATION="$2"
            shift 2
            ;;
        --mix)
            WORKLOAD_MIX="$2"
            shift 2
            ;;
        --output-dir)
            OUTPUT_DIR="$2"
            shift 2
            ;;
        --skip-init)
            SKIP_INIT=true
            shift
            ;;
        --init-only)
            INIT_ONLY=true
            shift
            ;;
        *)
            print_error "Unknown option: $1"
            show_usage
            exit 1
            ;;
    esac
done

# Main execution
main() {
    echo "Blueprint Workload Testing Suite"
    echo "================================"
    echo ""
    
    # Check frontend accessibility
    check_frontend
    
    # Setup output directory
    setup_output
    
    # Run initialization (unless skipped)
    if [ "$SKIP_INIT" = false ]; then
        run_initialization
    else
        print_warning "Skipping data initialization"
    fi
    
    # Exit if init-only mode
    if [ "$INIT_ONLY" = true ]; then
        print_success "Data initialization completed. Exiting."
        exit 0
    fi
    
    # Run workload
    run_workload
    
    # Generate report
    generate_report
    
    print_success "All tests completed successfully!"
    print_status "Results available in: $OUTPUT_DIR"
}

# Run main function
main "$@"
