#!/bin/bash

if [ $# -lt 2 ]; then
	echo "$0 <original_db_path> <split_db_path>"
	exit 1
fi

PWD=`pwd`

# make sure you we have dbtools
if [ -e /usr/local/bin/mdbx_copy ]; then
	# this is inside Docker
	DBCPY=/usr/local/bin/mdbx_copy
else
	DBCPY=../../build/bin/mdbx_copy
fi
if [ ! -e $DBCPY ]; then
	cd ../..
	make db-tools
	cd $PWD
	if [ ! -e $DBCPY ]; then
		echo "dbtools (mdbx_copy) not found"
		exit 1
	fi
fi

# compile smt-db-split
if [ -e /usr/local/bin/smt-db-split ]; then
	# this is inside Docker
	DBSPLIT=/usr/local/bin/smt-db-split
else
	DBSPLIT=../../build/bin/smt-db-split
fi
if [ ! -e $DBSPLIT ]; then
	cd ../..
	make smt-db-split
	cd $PWD
	if [ ! -e $DBSPLIT ]; then
		echo "smt-db-split binary not found"
		exit 1
	fi
fi

SRC=$1
DST=$2
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
cp mdbx_opts/opts_chaindb.json $TMP/seq/chaindata/
cp mdbx_opts/opts_smt.json $TMP/seq/smt/

mkdir -p $DST/seq/chaindata/
mkdir -p $DST/seq/smt/

if [ $# -gt 2 ]; then
	if [ $3 == "-d" ]; then
		echo "Dry-run done."
		exit 0
	fi
fi

$DBCPY -c $SRC/seq/chaindata/mdbx.dat $TMP/seq/chaindata/mdbx.dat
cp $TMP/seq/chaindata/mdbx.dat $TMP/seq/smt/mdbx.dat
$DBSPLIT $TMP/seq
$DBCPY -c $TMP/seq/chaindata/mdbx.dat $DST/seq/chaindata/mdbx.dat
$DBCPY -c $TMP/seq/smt/mdbx.dat $DST/seq/smt/mdbx.dat

rm -rf $TMP
echo "Done."