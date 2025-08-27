#!/bin/bash

# 获取最新区块高度
latest_block=$(cast block-number --rpc-url http://localhost:8123)
if [ $? -ne 0 ]; then
    echo "Failed to get latest block number"
    exit 1
fi

# 转换为十进制
latest_block_dec=$((latest_block))
start_block=8602731

echo "Checking blocks from $start_block to $latest_block_dec..."

# 遍历区块
for ((block=start_block; block<=latest_block_dec; block++)); do
    # 获取区块信息
    block_info=$(cast block $block --rpc-url http://localhost:8123 --json)
    if [ $? -ne 0 ]; then
        echo "Failed to get block info for block $block"
        continue
    fi
    
    # 获取交易数量
    tx_count=$(echo $block_info | jq '.transactions | length')
    
    # 如果交易数量大于1，打印区块高度
    if [ "$tx_count" -gt 1 ]; then
        echo "Block $block has $tx_count transactions"
    fi
done

