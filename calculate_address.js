const crypto = require('crypto');

function rlpEncode(input) {
    if (Array.isArray(input)) {
        let output = '';
        for (let item of input) {
            output += rlpEncode(item);
        }
        const length = output.length / 2;
        if (length < 56) {
            return (0xc0 + length).toString(16).padStart(2, '0') + output;
        } else {
            const lengthHex = length.toString(16);
            return (0xf7 + lengthHex.length / 2).toString(16).padStart(2, '0') + lengthHex + output;
        }
    } else if (typeof input === 'string') {
        if (input.startsWith('0x')) {
            input = input.slice(2);
        }
        const length = input.length / 2;
        if (length === 1 && parseInt(input, 16) < 0x80) {
            return input;
        } else if (length < 56) {
            return (0x80 + length).toString(16).padStart(2, '0') + input;
        } else {
            const lengthHex = length.toString(16);
            return (0xb7 + lengthHex.length / 2).toString(16).padStart(2, '0') + lengthHex + input;
        }
    } else if (typeof input === 'number') {
        if (input === 0) {
            return '80';
        } else if (input < 0x80) {
            return input.toString(16).padStart(2, '0');
        } else {
            const hex = input.toString(16);
            const length = Math.ceil(hex.length / 2);
            return (0x80 + length).toString(16).padStart(2, '0') + hex.padStart(length * 2, '0');
        }
    }
}

function calculateContractAddress(deployerAddress, nonce) {
    // 移除0x前缀
    const addr = deployerAddress.slice(2).toLowerCase();
    
    // RLP编码 [address, nonce]
    const rlpEncoded = rlpEncode([addr, nonce]);
    
    // 计算keccak256哈希
    const hash = crypto.createHash('sha3-256').update(Buffer.from(rlpEncoded, 'hex')).digest('hex');
    
    // 取后20字节作为地址
    return '0x' + hash.slice(-40);
}

const deployerAddress = '0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266';
const nonce = 0;

const contractAddress = calculateContractAddress(deployerAddress, nonce);
console.log('部署者地址:', deployerAddress);
console.log('Nonce:', nonce);
console.log('计算的合约地址:', contractAddress);

// 验证一下编码是否正确
console.log('\n验证RLP编码:');
const addr = deployerAddress.slice(2).toLowerCase();
const rlpEncoded = rlpEncode([addr, nonce]);
console.log('RLP编码结果:', rlpEncoded);

module.exports = { calculateContractAddress };


