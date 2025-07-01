set -e
set -x

./1-pp-setup.sh
./2-op-prepare.sh
# TODO, need to fix genisis block hash mismatch
./3-op-start-service.sh
# TODO
# ./4-pp-bridge-start.sh