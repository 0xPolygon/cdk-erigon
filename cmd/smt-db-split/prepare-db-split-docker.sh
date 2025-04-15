#!/bin/bash

if [ $# -lt 2 ]; then
	echo "$0 <original_db_path> <split_db_path>"
	exit 1
fi

# make sure we have the latest cdk-erigon image
cd ../..
make build-docker

# check paths are absolute
SRC=$1
DST=$2
if ! [[ $SRC =~ ^/ ]]; then
	echo "ERROR: SRC path is not absolute path!"
	exit 1
fi
if ! [[ $DST =~ ^/ ]]; then
	echo "ERROR: DST path is not absolute path!"
	exit 1
fi

TSTAMP=`date +%Y%m%d%H%M%S`
TMP=temp-$TSTAMP
mkdir -p $TMP

echo "WARNING: we are deleting destination folder: $DST"
rm -rf $DST
mkdir -p $DST
echo "Copy from folder: $SRC"
echo "Copy to folder: $DST"

mkdir -p $TMP/seq/chaindata/
mkdir -p $TMP/seq/smt/

mkdir -p $DST/seq/chaindata/
mkdir -p $DST/seq/smt/

if [ $# -gt 2 ]; then
	if [ $3 == "-d" ]; then
		echo "Dry-run done."
		exit 0
	fi
fi

docker run -v $SRC:/home/erigon/src -v ./$TMP:/home/erigon/tmp cdk-erigon:latest mdbx_copy -c /home/erigon/src/seq/chaindata/mdbx.dat /home/erigon/tmp/seq/chaindata/mdbx.dat
cp $TMP/seq/chaindata/mdbx.dat $TMP/seq/smt/mdbx.dat
docker run -v ./$TMP:/home/erigon/tmp cdk-erigon:latest smt-db-split /home/erigon/tmp/seq
docker run -v ./$TMP:/home/erigon/tmp -v $DST:/home/erigon/dst cdk-erigon:latest mdbx_copy -c /home/erigon/tmp/seq/chaindata/mdbx.dat /home/erigon/dst/seq/chaindata/mdbx.dat
docker run -v ./$TMP:/home/erigon/tmp -v $DST:/home/erigon/dst cdk-erigon:latest mdbx_copy -c /home/erigon/tmp/seq/smt/mdbx.dat /home/erigon/dst/seq/smt/mdbx.dat

rm -rf $TMP
echo "Done."