#!/bin/bash

if [ $# -lt 1 ]; then
	echo "$0 <mainnet dir>"
	exit 1
fi

DST="mainnet-split"
echo "Copy from folder: $1"
echo "Copy to folder: $DST"

cp -r $1 $DST
mkdir -p $DST/seq/smt/
cp $1/seq/chaindata/mdbx.* $DST/seq/smt/
cp mdbx_opts/opts_chaindb.json $DST/seq/chaindata/
cp mdbx_opts/opts_smt.json $DST/seq/smt/
