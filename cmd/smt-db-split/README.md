## How to use `prepare-db-split` scripts

These scripts are used to split an existing single Erigon-based sequencer database into two databases: one for chain data and one for SMT. Originally, chain data and SMT are stored in a single MDBX database file (`mdbx.dat`) under `data/seq/chaindata/mdbx.dat`.

Suppose you have an existing single Erigon database under `/home/ubuntu/data`. For the sequencer, you should have a `seq` sub-folder: `/home/ubuntu/data/seq`. Suppose you want to split this database and create a new Erigon database folder called `/home/ubuntu/split-data`. You can run the scripts as follows:

```bash
./prepare-db-split-docker.sh /home/ubuntu/data /home/ubuntu/split-data
# or
./prepare-db-split.sh /home/ubuntu/data /home/ubuntu/split-data
```

The difference between these two scripts is:
- [`prepare-db-split.sh`](prepare-db-split.sh) uses native `mdbx_copy` and `smt-db-split` binaries (compiled from Go code) to perform the split operations.
- [`prepare-db-split-docker.sh`](prepare-db-split-docker.sh) uses Docker to run `mdbx_copy` and `smt-db-split` commands.

**Note**: When using the split database, make sure your sequencer configuration includes the following setting:
```
zkevm.standalone-smt-db: true
```

When using a single database for both chain data and SMT, make sure your sequencer configuration includes the following setting:
```
zkevm.standalone-smt-db: false
```

---