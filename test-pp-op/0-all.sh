set -e
set -x

./1-pp-setup.sh
# TODO some issue with bridge, need to fix which cause the pp bridge failed
# ./11-bridge.sh # option: pp bridge L1 <> L2 
./2-op-prepare.sh
./3-op-start-service.sh
./4-pp-bridge-start.sh
./11-bridge-pp.sh # option: pp bridge L1 <> L2
./14-bridge-op.sh # option: op bridge L1 <> L2