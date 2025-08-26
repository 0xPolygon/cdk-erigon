#!/usr/bin/env python3
import json
import asyncio
import aiohttp
import sys
from typing import List, Dict, Any, Tuple
from dataclasses import dataclass
from web3 import Web3
from eth_utils import to_hex, to_checksum_address
import argparse
from tqdm import tqdm
import logging
from datetime import datetime

class GenesisChecker:
    def __init__(self, genesis_file: str, rpc_url: str, batch_size: int = 100, skip: int = 0, specific_address: str = None):
        self.genesis_file = genesis_file
        self.rpc_url = rpc_url
        self.batch_size = batch_size
        self.skip = skip
        self.specific_address = specific_address.lower().replace('0x', '') if specific_address else None
        self.w3 = Web3(Web3.HTTPProvider(rpc_url))
        self.differences = []
        self.total_accounts = 0
        self.mismatch_count = 0
        self.max_retries = 5  # 最大重试次数
        self.retry_delay = 2  # 基础重试间隔（秒）
        self.timeout = 60  # RPC请求超时时间（秒）

        # 设置日志
        timestamp = datetime.now().strftime('%Y%m%d_%H%M%S')
        self.log_file = f'genesis_check_{timestamp}.log'
        logging.basicConfig(
            level=logging.INFO,
            format='%(asctime)s - %(message)s',
            handlers=[
                logging.FileHandler(self.log_file),
                logging.StreamHandler()
            ]
        )
        self.logger = logging.getLogger(__name__)

    async def make_rpc_request(self, session: aiohttp.ClientSession, request_data: Any) -> Any:
        """发送RPC请求，带重试逻辑"""
        for retry in range(self.max_retries):
            try:
                timeout = aiohttp.ClientTimeout(total=self.timeout)
                async with session.post(self.rpc_url, json=request_data, timeout=timeout) as response:
                    if response.status == 200:
                        result = await response.json()
                        # 检查是否有超时错误
                        if isinstance(result, list):
                            for r in result:
                                if 'error' in r and r['error'].get('code') == -32002:  # 超时错误
                                    raise TimeoutError(f"RPC timeout for {r.get('id', 'unknown')}")
                        return result
                    else:
                        self.logger.warning(f"RPC request failed with status {response.status}, attempt {retry + 1}/{self.max_retries}")
            except TimeoutError as e:
                self.logger.warning(f"RPC timeout error: {e}, attempt {retry + 1}/{self.max_retries}")
                # 超时错误固定等待3秒
                if retry < self.max_retries - 1:
                    await asyncio.sleep(3)  # 固定等待3秒
                continue
            except Exception as e:
                self.logger.warning(f"RPC request failed with error: {e}, attempt {retry + 1}/{self.max_retries}")

            if retry < self.max_retries - 1:  # 如果不是最后一次重试
                await asyncio.sleep(self.retry_delay * (retry + 1))  # 指数退避

        raise Exception(f"RPC request failed after {self.max_retries} attempts")

    def ensure_hex_prefix(self, value: Any) -> str:
        """确保十六进制字符串有0x前缀"""
        if value is None:
            return '0x0'
        value_str = str(value)
        return value_str if value_str.startswith('0x') else '0x' + value_str

    def normalize_hex(self, value: Any) -> str:
        """标准化十六进制值"""
        if isinstance(value, str) and value.startswith('0x'):
            return value.lower()
        elif isinstance(value, (int, str)):
            try:
                # 如果是十六进制字符串，先确保有0x前缀
                if isinstance(value, str):
                    value = self.ensure_hex_prefix(value)
                return hex(int(str(value), 16) if isinstance(value, str) else int(value)).lower()
            except ValueError:
                self.logger.warning(f"Failed to convert value to hex: {value}")
                return '0x0'
        return '0x0'

    def log_difference(self, msg: str) -> None:
        """记录差异"""
        self.differences.append(msg)
        self.logger.warning(msg)
        self.mismatch_count += 1

    async def compare_basic_state(self, session: aiohttp.ClientSession,
                                address: str, genesis_state: Dict,
                                batch_results: Dict[str, str], index: int) -> bool:
        """比较基本状态（balance, nonce, code）"""
        has_code = False

        # 比较余额
        chain_balance = batch_results[f"balance_{index}"]
        genesis_balance = self.normalize_hex(genesis_state.get('balance', '0x0'))
        if chain_balance.lower() != genesis_balance:
            self.log_difference(
                f"Balance mismatch for {address}: genesis={genesis_balance}, chain={chain_balance}"
            )

        # 比较nonce
        chain_nonce = batch_results[f"nonce_{index}"]
        genesis_nonce = self.normalize_hex(genesis_state.get('nonce', '0x0'))
        if chain_nonce.lower() != genesis_nonce:
            self.log_difference(
                f"Nonce mismatch for {address}: genesis={genesis_nonce}, chain={chain_nonce}"
            )

        # 比较代码
        chain_code = batch_results[f"code_{index}"]
        genesis_code = self.normalize_hex(genesis_state.get('code', '0x'))
        if chain_code.lower() != genesis_code:
            self.log_difference(
                f"Code mismatch for {address}: genesis={genesis_code}, chain={chain_code}"
            )

        has_code = chain_code not in ['0x', '0x0']
        return has_code

    async def compare_storage(self, session: aiohttp.ClientSession,
                            address: str, genesis_storage: Dict[str, str]) -> None:
        """比较存储数据"""
        if not genesis_storage:
            return

        # 批量获取和比较存储数据
        # 注意：如果storage数量小于batch_size，Python的切片操作会自动处理，
        # storage_slots[i:i + batch_size] 会返回剩余的所有元素
        batch_size = self.batch_size
        storage_slots = list(genesis_storage.keys())

        for i in range(0, len(storage_slots), batch_size):
            end_idx = min(i + batch_size, len(storage_slots))
            batch_slots = storage_slots[i:end_idx]
            batch_request = [
                {
                    "jsonrpc": "2.0",
                    "method": "eth_getStorageAt",
                    "params": [
                        self.ensure_hex_prefix(address),
                        self.ensure_hex_prefix(slot),
                        'latest'
                    ],
                    "id": slot
                }
                for slot in batch_slots
            ]

            try:
                results = await self.make_rpc_request(session, batch_request)

                # 立即比较每个存储槽位
                for result in results:
                    slot = result['id']
                    chain_value = result['result'].lower()
                    genesis_value = self.normalize_hex(genesis_storage[slot])

                    if chain_value != genesis_value:
                        self.log_difference(
                            f"Storage mismatch for {address} at slot {slot}: "
                            f"genesis={genesis_value}, chain={chain_value}"
                        )
            except Exception as e:
                self.logger.error(f"Failed to get storage for address {address}: {e}")
                continue

    async def check_and_compare_batch(self, session: aiohttp.ClientSession,
                                    address_states: List[Tuple[str, Dict]]) -> None:
        """检查并比较一批账户"""
        # 准备批量请求
        batch_request = []
        for i, (addr, _) in enumerate(address_states):
                            # 确保地址有0x前缀
                addr = self.ensure_hex_prefix(addr)
                batch_request.extend([
                {
                    "jsonrpc": "2.0",
                    "method": "eth_getBalance",
                    "params": [addr, 'latest'],
                    "id": f"balance_{i}"
                },
                {
                    "jsonrpc": "2.0",
                    "method": "eth_getTransactionCount",
                    "params": [addr, 'latest'],
                    "id": f"nonce_{i}"
                },
                {
                    "jsonrpc": "2.0",
                    "method": "eth_getCode",
                    "params": [addr, 'latest'],
                    "id": f"code_{i}"
                }
            ])

        # 获取基本状态
        try:
            results = await self.make_rpc_request(session, batch_request)

            # 转换结果为字典格式，方便查找
            result_dict = {}
            for r in results:
                if 'error' in r:
                    self.logger.error(f"RPC error for address {r['id']}: {r['error']}")
                    continue
                if 'result' in r:
                    result_dict[r['id']] = r['result']
                else:
                    self.logger.error(f"Unexpected RPC response for address {r['id']}: {r}")

            # 对每个账户进行比较
            for i, (address, genesis_state) in enumerate(address_states):
                # 比较基本状态
                has_code = await self.compare_basic_state(
                    session, address, genesis_state, result_dict, i
                )

                # 如果有代码，比较存储
                if has_code:
                    await self.compare_storage(
                        session, address, genesis_state.get('storage', {})
                    )
        except Exception as e:
            self.logger.error(f"Failed to get basic state for batch: {e}")
            return  # 跳过这一批账户

    async def check_accounts(self) -> None:
        """检查所有账户的状态"""
        async with aiohttp.ClientSession() as session:
            # 读取创世文件并按批次处理
            with open(self.genesis_file, 'r') as f:
                genesis = json.load(f)
                alloc = genesis.get('alloc', {})

                # 如果指定了地址，只检查该地址
                if self.specific_address:
                    if self.specific_address not in alloc:
                        self.logger.error(f"Address {self.specific_address} not found in genesis file")
                        return
                    self.total_accounts = 1
                    self.logger.info(f"Checking specific address: 0x{self.specific_address}")
                    await self.check_and_compare_batch(session, [(self.specific_address, alloc[self.specific_address])])
                    return

                # 检查所有地址
                self.total_accounts = len(alloc)

                if self.skip >= self.total_accounts:
                    self.logger.warning(f"Skip value ({self.skip}) is larger than or equal to total accounts ({self.total_accounts})")
                    return

                self.logger.info(f"Total {self.total_accounts} accounts (skipping first {self.skip} accounts)...")

                # 使用tqdm显示进度
                with tqdm(total=self.total_accounts) as pbar:
                    # 直接更新跳过的数量
                    if self.skip > 0:
                        pbar.update(self.skip)
                    batch = []
                    for i, (address, state) in enumerate(alloc.items()):
                        if i < self.skip:
                            continue
                        batch.append((address, state))

                        if len(batch) >= self.batch_size:
                            await self.check_and_compare_batch(session, batch)
                            pbar.update(len(batch))
                            batch = []

                    # 处理剩余的账户
                    if batch:
                        await self.check_and_compare_batch(session, batch)
                        pbar.update(len(batch))

    def print_report(self) -> None:
        """打印比较报告"""
        self.logger.info("\nComparison Report:")
        self.logger.info(f"Total accounts checked: {self.total_accounts}")
        self.logger.info(f"Total differences found: {self.mismatch_count}")

        if self.differences:
            self.logger.info("\nDifferences:")
            for diff in self.differences:
                self.logger.info(diff)
        else:
            self.logger.info("\nAll states match!")

        self.logger.info(f"\nDetailed log has been saved to: {self.log_file}")

async def main():
    parser = argparse.ArgumentParser(description='Compare genesis state with chain state')
    parser.add_argument('--genesis', required=True, help='Path to genesis file')
    parser.add_argument('--rpc', default='http://localhost:8545', help='RPC endpoint')
    parser.add_argument('--batch-size', type=int, default=100, help='Batch size for RPC requests')
    parser.add_argument('--skip', type=int, default=0, help='Skip first N addresses')
    parser.add_argument('--address', help='Check specific address (with or without 0x prefix)')

    args = parser.parse_args()

    checker = GenesisChecker(args.genesis, args.rpc, args.batch_size, args.skip, args.address)
    await checker.check_accounts()
    checker.print_report()

if __name__ == "__main__":
    asyncio.run(main())