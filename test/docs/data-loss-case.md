## Background
- X Layer needs to run e2e tests to simulate extreme scenarios, such as sudden hardware failures, to identify under what circumstances the sequencer may lose transactions and when it will not.
- Develop recovery strategies for situations where transaction loss may occur.
## Issue
- No existing e2e testing script handles different data loss cases

## Result
In the current sequencer version, we don't need to worry about transaction loss situation. Whenever the sequencer fails, it will not lose transaction data.

## Solution
Design e2e automated testing process:
1. Insert code snippets that simulate crash exits at a certain batch number into the source code
2. Continue sending txs until that batch number minus 1 (stopBatch-1) and record the latest tx nonce and block hash to local file. Wait for the sequencer to stop at that batch number.
3. Recover the source code and restart sequencer.
4. Send one more tx and compare the new nonce and block hash with the nonce and block hash that was stored before. The new one should be the old nonce plus 1 and the new block hash should be the same as the old one.
If the above testings all pass, then it means there's no discarded transaction.

## Data loss cases

| Case | Method | Observation |
|------|--------|-------------|
| Case 1 | Interrupt before `sequencingBatchStep` `doFinishBlockAndUpdateState` (before `RemoveMinedTransactions`) <br><br>In this case, txs are still in txpool, data is lost in chaindata and not written to datastream yet. | Since aborted txs are still in the txpool, after relaunch, the sequencer rebuilds the block with the unremoved txs in txpool. Transaction loss won't occur. |
| Case 2 | Interrupt before `sequencingBatchStep` 1st `CommitAndStart` (before `RemoveMinedTransactions`) <br><br>In this case, txs are still in txpool, data is lost in chaindata and not written to datastream yet. | Since aborted txs are still in the txpool, after relaunch, the sequencer rebuilds the block with the unremoved txs in txpool. Transaction loss won't occur. |
| Case 3 | Interrupt before `sequencingBatchStep` 2nd `CommitAndStart` (after `RemoveMinedTransactions` and `updateStreamAndCheckRollback`) <br><br>In this case, txs are removed from txpool, data is lost in chaindata (assuming current block having been written to ds) | Since aborted txs are already written into the datastream, after relaunch, the sequencer reads the latest block from the datastream and re-executes it to update the chaindata. Transaction loss won't occur. |
| Case 4 | Interrupt before `sequencingBatchStep` the last `sdb.tx.Commit` (after `RemoveMinedTransactions` and `updateStreamAndCheckRollback`) <br><br>In this case, txs are removed from txpool, data is lost in chaindata (assuming current block having been written to ds) | Since aborted txs are already written into the datastream, after relaunch, the sequencer reads the latest block from the datastream and re-executes it to update the chaindata. Transaction loss won't occur. |
| Case 5 | Interrupt between `sequencingBatchStep` `RemoveMinedTransactions` and `updateStreamAndCheckRollback` <br><br>In this case, txs are removed from txpool, and data is not written to datastream yet. | Txs have already been stored into the chaindata after `doFinishBlockAndUpdateState`, and they are removed from txpool and haven't been written to the datastream. <br><br>In this case, the `alignExecutionToDatastream` method will find that the `lastExecutedBlock` is higher than `lastDatastreamBlock` by 1. Then it will unwind the chaindata to `lastDatastreamBlock` to align with the datastream. During the unwinding, there's an `accumulator.StarChange` method collecting the unwound txs and the `Accumulator` has a `SendAndReset` method to send the txs to the channel's consumer, which will be subscribed by `func (f *Fetch) handleStateChanges`. <br><br>So, the `lastExecutedBlock`'s txs are added back to the txpool during relaunch, and transaction loss won't occur either. |
| Case 6 | comment `handleShutdown`, disable the graceful shutdown | 1. txs are removed from txpool, data is in chaindata <br>2. Call `unwindExecutionToSMT` to unwind <br>3. Call `resequenceFromSMTAlignment` <br>4. Recover block form chaindata |
| Data recovery from rpc | This is the ultimate solution for potential transaction loss. After each of the above cases, check if seq works fine after replacing seq data with rpc data. | The sequencer works fine, no transaction loss occurs. |


