#!/bin/bash

if [ $# -lt 2 ]; then
	echo "$0 <original_db_path> <split_db_path>"
	exit 1
fi

if [ $(dirname "$0") != "." ]; then
	echo "Please run this script from the cmd/smt-db-split folder!"
	exit 1
fi

SCRIPDIR=$(pwd)
cd ../..
BASEDIR=$(pwd)
cd $SCRIPDIR

# make sure you we have dbtools
if [ -x /usr/local/bin/mdbx_copy ]; then
	# this is inside Docker
	DBCPY=/usr/local/bin/mdbx_copy
else
	DBCPY=$BASEDIR/build/bin/mdbx_copy
fi
if [ ! -x $DBCPY ]; then
	cd ../..
	make db-tools
	cd $SCRIPDIR
fi

# compile smt-db-split
if [ -x /usr/local/bin/smt-db-split ]; then
	# this is inside Docker
	DBSPLIT=/usr/local/bin/smt-db-split
else
	DBSPLIT=$BASEDIR/build/bin/smt-db-split
fi
if [ ! -x $DBSPLIT ]; then
	cd ../..
	make smt-db-split
	cd $SCRIPDIR
fi

# prepare folders
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
		rm -rf $TMP
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