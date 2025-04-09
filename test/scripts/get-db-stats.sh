#!/bin/bash

sudo ../build/bin/mdbx_stat -a data/seq/chaindata/mdbx.dat > stats1.txt
sudo ../build/bin/mdbx_stat -a data/seq/smt/mdbx.dat > stats2.txt
