#!/bin/bash

# 从文件中提取所有的tx hash
txhashes=$(grep -o '0x[a-fA-F0-9]\{64\}' $1)

total_l2_fee=0
total_l1_fee=0

# 十六进制转十进制的函数
hex_to_dec() {
    echo $(($1))
}

# 遍历每个tx hash
for hash in $txhashes; do
    echo "Processing tx: $hash"

    # 使用cast获取receipt并解析
    receipt=$(cast receipt $hash --rpc-url http://127.0.0.1:8123 --json)

    # 提取gasUsed和effectiveGasPrice（去掉0x前缀）
    gas_used_hex=$(echo $receipt | jq -r '.gasUsed')
    gas_price_hex=$(echo $receipt | jq -r '.effectiveGasPrice')

    # 转换为十进制
    gas_used=$(hex_to_dec $gas_used_hex)
    gas_price=$(hex_to_dec $gas_price_hex)

    # 计算L2 fee
    l2_fee=$((gas_used * gas_price))
    total_l2_fee=$((total_l2_fee + l2_fee))

    # 提取并计算L1 fee
    l1_fee_hex=$(echo $receipt | jq -r '.l1Fee')
    l1_fee=$(hex_to_dec $l1_fee_hex)
    total_l1_fee=$((total_l1_fee + l1_fee))

    echo "Gas Used: $gas_used ($gas_used_hex)"
    echo "Gas Price: $gas_price ($gas_price_hex)"
    echo "L2 Fee: $l2_fee"
    echo "L1 Fee: $l1_fee ($l1_fee_hex)"
    echo "-------------------"
done

echo "Total L2 Fee: $total_l2_fee"
echo "Total L1 Fee: $total_l1_fee"
echo "Total Fee: $((total_l2_fee + total_l1_fee))"
