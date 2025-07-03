set -e
set -x

./1-pp-setup.sh
./2-op-prepare.sh
./3-op-start-service.sh
./4-pp-bridge-start.sh