#!/bin/bash

# Celestia-App Upgrade Test Runner
# This script provides an easy way to run the upgrade test with different configurations

set -e

# Default configuration
BASE_VERSION="${BASE_VERSION:-v4.0.0-rc6}"
TARGET_VERSION="${TARGET_VERSION:-v4.1.0-dev}"
VALIDATOR_COUNT="${VALIDATOR_COUNT:-3}"
UPGRADE_TIMEOUT="${UPGRADE_TIMEOUT:-15m}"
UPGRADE_METHOD="${UPGRADE_METHOD:-signal}"
PRE_UPGRADE_BLOCKS="${PRE_UPGRADE_BLOCKS:-20}"
POST_UPGRADE_BLOCKS="${POST_UPGRADE_BLOCKS:-20}"

# Test configuration
TEST_TIMEOUT="${TEST_TIMEOUT:-30m}"
VERBOSE="${VERBOSE:-true}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_header() {
    echo -e "${BLUE}=================================${NC}"
    echo -e "${BLUE}  Celestia-App Upgrade Test${NC}"
    echo -e "${BLUE}=================================${NC}"
}

print_config() {
    echo -e "${YELLOW}Test Configuration:${NC}"
    echo -e "  Base Version:        ${BASE_VERSION}"
    echo -e "  Target Version:      ${TARGET_VERSION}"
    echo -e "  Validator Count:     ${VALIDATOR_COUNT}"
    echo -e "  Upgrade Timeout:     ${UPGRADE_TIMEOUT}"
    echo -e "  Upgrade Method:      ${UPGRADE_METHOD}"
    echo -e "  Pre-upgrade Blocks:  ${PRE_UPGRADE_BLOCKS}"
    echo -e "  Post-upgrade Blocks: ${POST_UPGRADE_BLOCKS}"
    echo -e "  Test Timeout:        ${TEST_TIMEOUT}"
    echo ""
}

print_usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  -b, --base-version VERSION     Base version to upgrade from (default: ${BASE_VERSION})"
    echo "  -t, --target-version VERSION   Target version to upgrade to (default: ${TARGET_VERSION})"
    echo "  -v, --validator-count COUNT    Number of validators (default: ${VALIDATOR_COUNT})"
    echo "  -m, --method METHOD            Upgrade method: signal|governance (default: ${UPGRADE_METHOD})"
    echo "  -u, --upgrade-timeout TIMEOUT  Upgrade timeout (default: ${UPGRADE_TIMEOUT})"
    echo "  -T, --test-timeout TIMEOUT     Test timeout (default: ${TEST_TIMEOUT})"
    echo "  -q, --quiet                    Disable verbose output"
    echo "  -h, --help                     Show this help message"
    echo ""
    echo "Environment Variables:"
    echo "  CELESTIA_IMAGE                 Override Celestia Docker image"
    echo "  CELESTIA_TAG                   Override Celestia Docker tag"
    echo ""
    echo "Examples:"
    echo "  $0                                           # Run with defaults"
    echo "  $0 -b v4.0.0-rc6 -t v4.1.0                 # Test specific version upgrade"
    echo "  $0 -v 5 -u 20m                             # 5 validators, 20m timeout"
    echo "  $0 -m governance                            # Use governance upgrade method"
    echo "  CELESTIA_TAG=latest $0 -t main              # Test with latest image"
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -b|--base-version)
            BASE_VERSION="$2"
            shift 2
            ;;
        -t|--target-version)
            TARGET_VERSION="$2"
            shift 2
            ;;
        -v|--validator-count)
            VALIDATOR_COUNT="$2"
            shift 2
            ;;
        -m|--method)
            UPGRADE_METHOD="$2"
            shift 2
            ;;
        -u|--upgrade-timeout)
            UPGRADE_TIMEOUT="$2"
            shift 2
            ;;
        -T|--test-timeout)
            TEST_TIMEOUT="$2"
            shift 2
            ;;
        -q|--quiet)
            VERBOSE="false"
            shift
            ;;
        -h|--help)
            print_usage
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            print_usage
            exit 1
            ;;
    esac
done

# Validate upgrade method
if [[ "$UPGRADE_METHOD" != "signal" && "$UPGRADE_METHOD" != "governance" ]]; then
    echo -e "${RED}Error: Invalid upgrade method '$UPGRADE_METHOD'. Must be 'signal' or 'governance'${NC}"
    exit 1
fi

# Check if Docker is running
if ! docker info >/dev/null 2>&1; then
    echo -e "${RED}Error: Docker is not running or not accessible${NC}"
    echo "Please ensure Docker is installed and running"
    exit 1
fi

# Check if we're in the right directory
if [[ ! -f "e2e_upgrade_test.go" ]]; then
    echo -e "${RED}Error: e2e_upgrade_test.go not found${NC}"
    echo "Please run this script from the test/docker-e2e directory"
    exit 1
fi

print_header
print_config

echo -e "${YELLOW}Starting upgrade test...${NC}"
echo ""

# Set up environment variables
export BASE_VERSION
export TARGET_VERSION
export VALIDATOR_COUNT
export UPGRADE_TIMEOUT
export UPGRADE_METHOD
export PRE_UPGRADE_BLOCKS
export POST_UPGRADE_BLOCKS

# Build test flags
TEST_FLAGS="-run CelestiaTestSuite/TestCelestiaAppUpgrade -timeout ${TEST_TIMEOUT} -count 1"
if [[ "$VERBOSE" == "true" ]]; then
    TEST_FLAGS="$TEST_FLAGS -v"
fi

# Run the test
echo -e "${BLUE}Executing: go test ${TEST_FLAGS}${NC}"
echo ""

if go test $TEST_FLAGS; then
    echo ""
    echo -e "${GREEN}✅ Upgrade test completed successfully!${NC}"
    echo -e "${GREEN}   Base version: ${BASE_VERSION}${NC}"
    echo -e "${GREEN}   Target version: ${TARGET_VERSION}${NC}"
    echo -e "${GREEN}   All data availability features validated${NC}"
else
    echo ""
    echo -e "${RED}❌ Upgrade test failed${NC}"
    echo -e "${RED}   Check the logs above for details${NC}"
    exit 1
fi