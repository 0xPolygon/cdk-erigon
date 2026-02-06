# Unwind Test Flow

The `zk/tests/unwinds` suite was rebuilt to make the unwind checks deterministic and to capture enough metadata to debug regressions without rerunning the entire pipeline. The new flow is orchestrated by `zk/tests/unwinds/unwind.sh` and works roughly as follows:

1. **Prime the datastream with the live sequencer**
   - Start the sequencer with the target config, wait until the RPC endpoint begins returning blocks, and push a burst of transactions so the datastream contains real activity.
   - Stop the sequencer and restart it with a `--zkevm.sequencer-halt-on-batch-number` guard so we can snapshot a halted state on demand (no more scraping the sequencer logs for “halted” messages).

2. **Capture first/second stop metadata**
   - Query the halted sequencer via RPC to pick a midpoint batch (`first_stop_batch`) and locate its final block.
   - Back up the datastream artifacts from the halted sequencer (binary + DB) and record the block numbers we will use for the RPC node runs.

3. **Replay the datastream with an RPC node**
   - Launch the standalone datastream host pointing at the halted snapshot.
   - Run `cdk-erigon` twice with `--debug.limit` (first stop and second stop) dumping state via `cmd/hack --action=dumpAll`.
   - Perform the unwind using `state_stages_zkevm --unwind-batch-no=<midpoint>` and compare the dumps to ensure only the expected tables diverge.
   - Re‑sync to the second stop and compare again to make sure forward progress matches the reference snapshot.

4. **Why this design**
   - *No more repo-bloating dumps*: Previously we hand-crafted datastream archives and checked them into git; any change that affected the state root (e.g. base fee tweaks) meant regenerating/touching huge binaries. Now the suite produces the datastream snapshot on the fly during CI, so it always matches the current code and doesn’t live in the repo.
   - *Deterministic halts*: We wait for RPC readiness and explicit block production before stopping the sequencer, so the script behaves consistently on CI without scraping log strings.
   - *Automated validation*: Every critical step (sync to first stop, unwind, re‑sync) still dumps/compares tables, but those dumps are generated deterministically as part of the run, eliminating manual refreshes.

The script handles all cleanup, port checks, retries, and logging (see `script.log`). To run locally, set `PRIVATE_KEY`, build `cdk-erigon`, and execute `zk/tests/unwinds/unwind.sh`. The CI workflows (`test-unwinds.yml`, `test-unwinds_type-1-pmt.yml`) call the same script, so the documentation here applies to both environments.
