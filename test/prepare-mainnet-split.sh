#!/bin/bash

if [ $# -lt 1 ]; then
	echo "$0 <mainnet dir>"
	exit 1
fi

# make sure you we have dbtools
DBCPY=../build/bin/mdbx_copy
if [ ! -e $DBCPY ]; then
	cd ..
	make db-tools
	cd test
	if [ ! -e $DBCPY ]; then
		echo "dbtools (mdbx_copy) not found"
		exit 1
	fi
fi

# compile smt-db-split
DBSPLIT=../cmd/smt-db-split/smt-db-split
if [ ! -e $DBSPLIT ]; then
	cd ../cmd/smt-db-split
	go build
	cd ../../test
	if [ ! -e $DBSPLIT ]; then
		echo "smt-db-split binary not found"
		exit 1
	fi
fi

SRC=$1
DST="mainnet-split"
echo "Copy from folder: $SRC"
echo "Copy to folder: $DST"

mkdir -p $DST/seq/chaindata/
mkdir -p $DST/seq/smt/
cp mdbx_opts/opts_chaindb.json $DST/seq/chaindata/
cp mdbx_opts/opts_smt.json $DST/seq/smt/
$DBCPY -c $SRC/seq/chaindata/mdbx.dat $DST/seq/chaindata/mdbx.dat
cp $DST/seq/chaindata/mdbx.dat $DST/seq/smt/mdbx.dat
$DBSPLIT $DST/seq