//go:build !skip_smoke
// +build !skip_smoke

package e2e

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/holiman/uint256"
	ethereum "github.com/ledgerwatch/erigon"
	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/accounts/abi"
	"github.com/ledgerwatch/erigon/accounts/abi/bind"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/crypto"
	"github.com/ledgerwatch/erigon/ethclient"
	"github.com/ledgerwatch/erigon/rpc"
	"github.com/ledgerwatch/erigon/turbo/jsonrpc/constants"
	"gopkg.in/yaml.v2"

	"github.com/ledgerwatch/erigon/test/operations"
	"github.com/ledgerwatch/erigon/zkevm/encoding"
	"github.com/ledgerwatch/erigon/zkevm/etherman/smartcontracts/polygonzkevmbridge"
	"github.com/ledgerwatch/erigon/zkevm/log"

	"github.com/stretchr/testify/require"
)

const (
	blockAddress    = "0xdD2FD4581271e230360230F9337D5c0430Bf44C0"
	blockPrivateKey = "0xde9be858da4a475276426320d5e9262ecfc3ba460bfac56360bfa6c4c28b4ee0"

	testVerified                       = true
	tmpSenderPrivateKey                = "363ea277eec54278af051fb574931aec751258450a286edce9e1f64401f3b9c8"
	specificProjectSenderPrivateKey    = "100f4e42de757bdfa31122dfc5bc00f1afe508b7b3c214a96fa00cbf05d979cf"
	nonSpecificProjectSenderPrivateKay = "fc1c22e30c1d9e4f6449b3c44c2991dbc6a202d8895d18fefa224296c01949cd"
	erc20ABIJson                       = "[\n\t{\n\t\t\"inputs\": [],\n\t\t\"stateMutability\": \"nonpayable\",\n\t\t\"type\": \"constructor\"\n\t},\n\t{\n\t\t\"anonymous\": false,\n\t\t\"inputs\": [\n\t\t\t{\n\t\t\t\t\"indexed\": true,\n\t\t\t\t\"internalType\": \"address\",\n\t\t\t\t\"name\": \"owner\",\n\t\t\t\t\"type\": \"address\"\n\t\t\t},\n\t\t\t{\n\t\t\t\t\"indexed\": true,\n\t\t\t\t\"internalType\": \"address\",\n\t\t\t\t\"name\": \"spender\",\n\t\t\t\t\"type\": \"address\"\n\t\t\t},\n\t\t\t{\n\t\t\t\t\"indexed\": false,\n\t\t\t\t\"internalType\": \"uint256\",\n\t\t\t\t\"name\": \"value\",\n\t\t\t\t\"type\": \"uint256\"\n\t\t\t}\n\t\t],\n\t\t\"name\": \"Approval\",\n\t\t\"type\": \"event\"\n\t},\n\t{\n\t\t\"anonymous\": false,\n\t\t\"inputs\": [\n\t\t\t{\n\t\t\t\t\"indexed\": true,\n\t\t\t\t\"internalType\": \"address\",\n\t\t\t\t\"name\": \"from\",\n\t\t\t\t\"type\": \"address\"\n\t\t\t},\n\t\t\t{\n\t\t\t\t\"indexed\": true,\n\t\t\t\t\"internalType\": \"address\",\n\t\t\t\t\"name\": \"to\",\n\t\t\t\t\"type\": \"address\"\n\t\t\t},\n\t\t\t{\n\t\t\t\t\"indexed\": false,\n\t\t\t\t\"internalType\": \"uint256\",\n\t\t\t\t\"name\": \"value\",\n\t\t\t\t\"type\": \"uint256\"\n\t\t\t}\n\t\t],\n\t\t\"name\": \"Transfer\",\n\t\t\"type\": \"event\"\n\t},\n\t{\n\t\t\"inputs\": [\n\t\t\t{\n\t\t\t\t\"internalType\": \"address\",\n\t\t\t\t\"name\": \"owner\",\n\t\t\t\t\"type\": \"address\"\n\t\t\t},\n\t\t\t{\n\t\t\t\t\"internalType\": \"address\",\n\t\t\t\t\"name\": \"spender\",\n\t\t\t\t\"type\": \"address\"\n\t\t\t}\n\t\t],\n\t\t\"name\": \"allowance\",\n\t\t\"outputs\": [\n\t\t\t{\n\t\t\t\t\"internalType\": \"uint256\",\n\t\t\t\t\"name\": \"\",\n\t\t\t\t\"type\": \"uint256\"\n\t\t\t}\n\t\t],\n\t\t\"stateMutability\": \"view\",\n\t\t\"type\": \"function\"\n\t},\n\t{\n\t\t\"inputs\": [\n\t\t\t{\n\t\t\t\t\"internalType\": \"address\",\n\t\t\t\t\"name\": \"spender\",\n\t\t\t\t\"type\": \"address\"\n\t\t\t},\n\t\t\t{\n\t\t\t\t\"internalType\": \"uint256\",\n\t\t\t\t\"name\": \"amount\",\n\t\t\t\t\"type\": \"uint256\"\n\t\t\t}\n\t\t],\n\t\t\"name\": \"approve\",\n\t\t\"outputs\": [\n\t\t\t{\n\t\t\t\t\"internalType\": \"bool\",\n\t\t\t\t\"name\": \"\",\n\t\t\t\t\"type\": \"bool\"\n\t\t\t}\n\t\t],\n\t\t\"stateMutability\": \"nonpayable\",\n\t\t\"type\": \"function\"\n\t},\n\t{\n\t\t\"inputs\": [\n\t\t\t{\n\t\t\t\t\"internalType\": \"address\",\n\t\t\t\t\"name\": \"account\",\n\t\t\t\t\"type\": \"address\"\n\t\t\t}\n\t\t],\n\t\t\"name\": \"balanceOf\",\n\t\t\"outputs\": [\n\t\t\t{\n\t\t\t\t\"internalType\": \"uint256\",\n\t\t\t\t\"name\": \"\",\n\t\t\t\t\"type\": \"uint256\"\n\t\t\t}\n\t\t],\n\t\t\"stateMutability\": \"view\",\n\t\t\"type\": \"function\"\n\t},\n\t{\n\t\t\"inputs\": [],\n\t\t\"name\": \"decimals\",\n\t\t\"outputs\": [\n\t\t\t{\n\t\t\t\t\"internalType\": \"uint8\",\n\t\t\t\t\"name\": \"\",\n\t\t\t\t\"type\": \"uint8\"\n\t\t\t}\n\t\t],\n\t\t\"stateMutability\": \"view\",\n\t\t\"type\": \"function\"\n\t},\n\t{\n\t\t\"inputs\": [\n\t\t\t{\n\t\t\t\t\"internalType\": \"address\",\n\t\t\t\t\"name\": \"spender\",\n\t\t\t\t\"type\": \"address\"\n\t\t\t},\n\t\t\t{\n\t\t\t\t\"internalType\": \"uint256\",\n\t\t\t\t\"name\": \"subtractedValue\",\n\t\t\t\t\"type\": \"uint256\"\n\t\t\t}\n\t\t],\n\t\t\"name\": \"decreaseAllowance\",\n\t\t\"outputs\": [\n\t\t\t{\n\t\t\t\t\"internalType\": \"bool\",\n\t\t\t\t\"name\": \"\",\n\t\t\t\t\"type\": \"bool\"\n\t\t\t}\n\t\t],\n\t\t\"stateMutability\": \"nonpayable\",\n\t\t\"type\": \"function\"\n\t},\n\t{\n\t\t\"inputs\": [\n\t\t\t{\n\t\t\t\t\"internalType\": \"address\",\n\t\t\t\t\"name\": \"spender\",\n\t\t\t\t\"type\": \"address\"\n\t\t\t},\n\t\t\t{\n\t\t\t\t\"internalType\": \"uint256\",\n\t\t\t\t\"name\": \"addedValue\",\n\t\t\t\t\"type\": \"uint256\"\n\t\t\t}\n\t\t],\n\t\t\"name\": \"increaseAllowance\",\n\t\t\"outputs\": [\n\t\t\t{\n\t\t\t\t\"internalType\": \"bool\",\n\t\t\t\t\"name\": \"\",\n\t\t\t\t\"type\": \"bool\"\n\t\t\t}\n\t\t],\n\t\t\"stateMutability\": \"nonpayable\",\n\t\t\"type\": \"function\"\n\t},\n\t{\n\t\t\"inputs\": [],\n\t\t\"name\": \"name\",\n\t\t\"outputs\": [\n\t\t\t{\n\t\t\t\t\"internalType\": \"string\",\n\t\t\t\t\"name\": \"\",\n\t\t\t\t\"type\": \"string\"\n\t\t\t}\n\t\t],\n\t\t\"stateMutability\": \"view\",\n\t\t\"type\": \"function\"\n\t},\n\t{\n\t\t\"inputs\": [],\n\t\t\"name\": \"symbol\",\n\t\t\"outputs\": [\n\t\t\t{\n\t\t\t\t\"internalType\": \"string\",\n\t\t\t\t\"name\": \"\",\n\t\t\t\t\"type\": \"string\"\n\t\t\t}\n\t\t],\n\t\t\"stateMutability\": \"view\",\n\t\t\"type\": \"function\"\n\t},\n\t{\n\t\t\"inputs\": [],\n\t\t\"name\": \"totalSupply\",\n\t\t\"outputs\": [\n\t\t\t{\n\t\t\t\t\"internalType\": \"uint256\",\n\t\t\t\t\"name\": \"\",\n\t\t\t\t\"type\": \"uint256\"\n\t\t\t}\n\t\t],\n\t\t\"stateMutability\": \"view\",\n\t\t\"type\": \"function\"\n\t},\n\t{\n\t\t\"inputs\": [\n\t\t\t{\n\t\t\t\t\"internalType\": \"address\",\n\t\t\t\t\"name\": \"to\",\n\t\t\t\t\"type\": \"address\"\n\t\t\t},\n\t\t\t{\n\t\t\t\t\"internalType\": \"uint256\",\n\t\t\t\t\"name\": \"amount\",\n\t\t\t\t\"type\": \"uint256\"\n\t\t\t}\n\t\t],\n\t\t\"name\": \"transfer\",\n\t\t\"outputs\": [\n\t\t\t{\n\t\t\t\t\"internalType\": \"bool\",\n\t\t\t\t\"name\": \"\",\n\t\t\t\t\"type\": \"bool\"\n\t\t\t}\n\t\t],\n\t\t\"stateMutability\": \"nonpayable\",\n\t\t\"type\": \"function\"\n\t},\n\t{\n\t\t\"inputs\": [\n\t\t\t{\n\t\t\t\t\"internalType\": \"address\",\n\t\t\t\t\"name\": \"from\",\n\t\t\t\t\"type\": \"address\"\n\t\t\t},\n\t\t\t{\n\t\t\t\t\"internalType\": \"address\",\n\t\t\t\t\"name\": \"to\",\n\t\t\t\t\"type\": \"address\"\n\t\t\t},\n\t\t\t{\n\t\t\t\t\"internalType\": \"uint256\",\n\t\t\t\t\"name\": \"amount\",\n\t\t\t\t\"type\": \"uint256\"\n\t\t\t}\n\t\t],\n\t\t\"name\": \"transferFrom\",\n\t\t\"outputs\": [\n\t\t\t{\n\t\t\t\t\"internalType\": \"bool\",\n\t\t\t\t\"name\": \"\",\n\t\t\t\t\"type\": \"bool\"\n\t\t\t}\n\t\t],\n\t\t\"stateMutability\": \"nonpayable\",\n\t\t\"type\": \"function\"\n\t}\n]"
	erc20BytecodeStr                   = "60806040523480156200001157600080fd5b506040518060400160405280600781526020017f4d79546f6b656e000000000000000000000000000000000000000000000000008152506040518060400160405280600381526020017f4d544b000000000000000000000000000000000000000000000000000000000081525081600390816200008f9190620004e4565b508060049081620000a19190620004e4565b505050620000e433620000b9620000ea60201b60201c565b600a620000c791906200075b565b6305f5e100620000d89190620007ac565b620000f360201b60201c565b620008e3565b60006012905090565b600073ffffffffffffffffffffffffffffffffffffffff168273ffffffffffffffffffffffffffffffffffffffff160362000165576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016200015c9062000858565b60405180910390fd5b62000179600083836200026060201b60201c565b80600260008282546200018d91906200087a565b92505081905550806000808473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825401925050819055508173ffffffffffffffffffffffffffffffffffffffff16600073ffffffffffffffffffffffffffffffffffffffff167fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef83604051620002409190620008c6565b60405180910390a36200025c600083836200026560201b60201c565b5050565b505050565b505050565b600081519050919050565b7f4e487b7100000000000000000000000000000000000000000000000000000000600052604160045260246000fd5b7f4e487b7100000000000000000000000000000000000000000000000000000000600052602260045260246000fd5b60006002820490506001821680620002ec57607f821691505b602082108103620003025762000301620002a4565b5b50919050565b60008190508160005260206000209050919050565b60006020601f8301049050919050565b600082821b905092915050565b6000600883026200036c7fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff826200032d565b6200037886836200032d565b95508019841693508086168417925050509392505050565b6000819050919050565b6000819050919050565b6000620003c5620003bf620003b98462000390565b6200039a565b62000390565b9050919050565b6000819050919050565b620003e183620003a4565b620003f9620003f082620003cc565b8484546200033a565b825550505050565b600090565b6200041062000401565b6200041d818484620003d6565b505050565b5b8181101562000445576200043960008262000406565b60018101905062000423565b5050565b601f82111562000494576200045e8162000308565b62000469846200031d565b8101602085101562000479578190505b6200049162000488856200031d565b83018262000422565b50505b505050565b600082821c905092915050565b6000620004b96000198460080262000499565b1980831691505092915050565b6000620004d48383620004a6565b9150826002028217905092915050565b620004ef826200026a565b67ffffffffffffffff8111156200050b576200050a62000275565b5b620005178254620002d3565b6200052482828562000449565b600060209050601f8311600181146200055c576000841562000547578287015190505b620005538582620004c6565b865550620005c3565b601f1984166200056c8662000308565b60005b8281101562000596578489015182556001820191506020850194506020810190506200056f565b86831015620005b65784890151620005b2601f891682620004a6565b8355505b6001600288020188555050505b505050505050565b7f4e487b7100000000000000000000000000000000000000000000000000000000600052601160045260246000fd5b60008160011c9050919050565b6000808291508390505b60018511156200065957808604811115620006315762000630620005cb565b5b6001851615620006415780820291505b80810290506200065185620005fa565b945062000611565b94509492505050565b60008262000674576001905062000747565b8162000684576000905062000747565b81600181146200069d5760028114620006a857620006de565b600191505062000747565b60ff841115620006bd57620006bc620005cb565b5b8360020a915084821115620006d757620006d6620005cb565b5b5062000747565b5060208310610133831016604e8410600b8410161715620007185782820a905083811115620007125762000711620005cb565b5b62000747565b62000727848484600162000607565b92509050818404811115620007415762000740620005cb565b5b81810290505b9392505050565b600060ff82169050919050565b6000620007688262000390565b915062000775836200074e565b9250620007a47fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff848462000662565b905092915050565b6000620007b98262000390565b9150620007c68362000390565b9250828202620007d68162000390565b91508282048414831517620007f057620007ef620005cb565b5b5092915050565b600082825260208201905092915050565b7f45524332303a206d696e7420746f20746865207a65726f206164647265737300600082015250565b600062000840601f83620007f7565b91506200084d8262000808565b602082019050919050565b60006020820190508181036000830152620008738162000831565b9050919050565b6000620008878262000390565b9150620008948362000390565b9250828201905080821115620008af57620008ae620005cb565b5b92915050565b620008c08162000390565b82525050565b6000602082019050620008dd6000830184620008b5565b92915050565b61122f80620008f36000396000f3fe608060405234801561001057600080fd5b50600436106100a95760003560e01c80633950935111610071578063395093511461016857806370a082311461019857806395d89b41146101c8578063a457c2d7146101e6578063a9059cbb14610216578063dd62ed3e14610246576100a9565b806306fdde03146100ae578063095ea7b3146100cc57806318160ddd146100fc57806323b872dd1461011a578063313ce5671461014a575b600080fd5b6100b6610276565b6040516100c39190610b0c565b60405180910390f35b6100e660048036038101906100e19190610bc7565b610308565b6040516100f39190610c22565b60405180910390f35b61010461032b565b6040516101119190610c4c565b60405180910390f35b610134600480360381019061012f9190610c67565b610335565b6040516101419190610c22565b60405180910390f35b610152610364565b60405161015f9190610cd6565b60405180910390f35b610182600480360381019061017d9190610bc7565b61036d565b60405161018f9190610c22565b60405180910390f35b6101b260048036038101906101ad9190610cf1565b6103a4565b6040516101bf9190610c4c565b60405180910390f35b6101d06103ec565b6040516101dd9190610b0c565b60405180910390f35b61020060048036038101906101fb9190610bc7565b61047e565b60405161020d9190610c22565b60405180910390f35b610230600480360381019061022b9190610bc7565b6104f5565b60405161023d9190610c22565b60405180910390f35b610260600480360381019061025b9190610d1e565b610518565b60405161026d9190610c4c565b60405180910390f35b60606003805461028590610d8d565b80601f01602080910402602001604051908101604052809291908181526020018280546102b190610d8d565b80156102fe5780601f106102d3576101008083540402835291602001916102fe565b820191906000526020600020905b8154815290600101906020018083116102e157829003601f168201915b5050505050905090565b60008061031361059f565b90506103208185856105a7565b600191505092915050565b6000600254905090565b60008061034061059f565b905061034d858285610770565b6103588585856107fc565b60019150509392505050565b60006012905090565b60008061037861059f565b905061039981858561038a8589610518565b6103949190610ded565b6105a7565b600191505092915050565b60008060008373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020549050919050565b6060600480546103fb90610d8d565b80601f016020809104026020016040519081016040528092919081815260200182805461042790610d8d565b80156104745780601f1061044957610100808354040283529160200191610474565b820191906000526020600020905b81548152906001019060200180831161045757829003601f168201915b5050505050905090565b60008061048961059f565b905060006104978286610518565b9050838110156104dc576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016104d390610e93565b60405180910390fd5b6104e982868684036105a7565b60019250505092915050565b60008061050061059f565b905061050d8185856107fc565b600191505092915050565b6000600160008473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060008373ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002054905092915050565b600033905090565b600073ffffffffffffffffffffffffffffffffffffffff168373ffffffffffffffffffffffffffffffffffffffff1603610616576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161060d90610f25565b60405180910390fd5b600073ffffffffffffffffffffffffffffffffffffffff168273ffffffffffffffffffffffffffffffffffffffff1603610685576040517f08c379a000000000000000000000000000000000000000000000000000000000815260040161067c90610fb7565b60405180910390fd5b80600160008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002060008473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020819055508173ffffffffffffffffffffffffffffffffffffffff168373ffffffffffffffffffffffffffffffffffffffff167f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925836040516107639190610c4c565b60405180910390a3505050565b600061077c8484610518565b90507fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff81146107f657818110156107e8576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016107df90611023565b60405180910390fd5b6107f584848484036105a7565b5b50505050565b600073ffffffffffffffffffffffffffffffffffffffff168373ffffffffffffffffffffffffffffffffffffffff160361086b576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610862906110b5565b60405180910390fd5b600073ffffffffffffffffffffffffffffffffffffffff168273ffffffffffffffffffffffffffffffffffffffff16036108da576040517f08c379a00000000000000000000000000000000000000000000000000000000081526004016108d190611147565b60405180910390fd5b6108e5838383610a72565b60008060008573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff1681526020019081526020016000205490508181101561096b576040517f08c379a0000000000000000000000000000000000000000000000000000000008152600401610962906111d9565b60405180910390fd5b8181036000808673ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16815260200190815260200160002081905550816000808573ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001908152602001600020600082825401925050819055508273ffffffffffffffffffffffffffffffffffffffff168473ffffffffffffffffffffffffffffffffffffffff167fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef84604051610a599190610c4c565b60405180910390a3610a6c848484610a77565b50505050565b505050565b505050565b600081519050919050565b600082825260208201905092915050565b60005b83811015610ab6578082015181840152602081019050610a9b565b60008484015250505050565b6000601f19601f8301169050919050565b6000610ade82610a7c565b610ae88185610a87565b9350610af8818560208601610a98565b610b0181610ac2565b840191505092915050565b60006020820190508181036000830152610b268184610ad3565b905092915050565b600080fd5b600073ffffffffffffffffffffffffffffffffffffffff82169050919050565b6000610b5e82610b33565b9050919050565b610b6e81610b53565b8114610b7957600080fd5b50565b600081359050610b8b81610b65565b92915050565b6000819050919050565b610ba481610b91565b8114610baf57600080fd5b50565b600081359050610bc181610b9b565b92915050565b60008060408385031215610bde57610bdd610b2e565b5b6000610bec85828601610b7c565b9250506020610bfd85828601610bb2565b9150509250929050565b60008115159050919050565b610c1c81610c07565b82525050565b6000602082019050610c376000830184610c13565b92915050565b610c4681610b91565b82525050565b6000602082019050610c616000830184610c3d565b92915050565b600080600060608486031215610c8057610c7f610b2e565b5b6000610c8e86828701610b7c565b9350506020610c9f86828701610b7c565b9250506040610cb086828701610bb2565b9150509250925092565b600060ff82169050919050565b610cd081610cba565b82525050565b6000602082019050610ceb6000830184610cc7565b92915050565b600060208284031215610d0757610d06610b2e565b5b6000610d1584828501610b7c565b91505092915050565b60008060408385031215610d3557610d34610b2e565b5b6000610d4385828601610b7c565b9250506020610d5485828601610b7c565b9150509250929050565b7f4e487b7100000000000000000000000000000000000000000000000000000000600052602260045260246000fd5b60006002820490506001821680610da557607f821691505b602082108103610db857610db7610d5e565b5b50919050565b7f4e487b7100000000000000000000000000000000000000000000000000000000600052601160045260246000fd5b6000610df882610b91565b9150610e0383610b91565b9250828201905080821115610e1b57610e1a610dbe565b5b92915050565b7f45524332303a2064656372656173656420616c6c6f77616e63652062656c6f7760008201527f207a65726f000000000000000000000000000000000000000000000000000000602082015250565b6000610e7d602583610a87565b9150610e8882610e21565b604082019050919050565b60006020820190508181036000830152610eac81610e70565b9050919050565b7f45524332303a20617070726f76652066726f6d20746865207a65726f2061646460008201527f7265737300000000000000000000000000000000000000000000000000000000602082015250565b6000610f0f602483610a87565b9150610f1a82610eb3565b604082019050919050565b60006020820190508181036000830152610f3e81610f02565b9050919050565b7f45524332303a20617070726f766520746f20746865207a65726f20616464726560008201527f7373000000000000000000000000000000000000000000000000000000000000602082015250565b6000610fa1602283610a87565b9150610fac82610f45565b604082019050919050565b60006020820190508181036000830152610fd081610f94565b9050919050565b7f45524332303a20696e73756666696369656e7420616c6c6f77616e6365000000600082015250565b600061100d601d83610a87565b915061101882610fd7565b602082019050919050565b6000602082019050818103600083015261103c81611000565b9050919050565b7f45524332303a207472616e736665722066726f6d20746865207a65726f20616460008201527f6472657373000000000000000000000000000000000000000000000000000000602082015250565b600061109f602583610a87565b91506110aa82611043565b604082019050919050565b600060208201905081810360008301526110ce81611092565b9050919050565b7f45524332303a207472616e7366657220746f20746865207a65726f206164647260008201527f6573730000000000000000000000000000000000000000000000000000000000602082015250565b6000611131602383610a87565b915061113c826110d5565b604082019050919050565b6000602082019050818103600083015261116081611124565b9050919050565b7f45524332303a207472616e7366657220616d6f756e742065786365656473206260008201527f616c616e63650000000000000000000000000000000000000000000000000000602082015250565b60006111c3602683610a87565b91506111ce82611167565b604082019050919050565b600060208201905081810360008301526111f2816111b6565b905091905056fea2646970667358221220a1e42afa780fa0b792c1b1544459f1223cd5f165dbd77fb15760adb1e937625e64736f6c63430008130033"
	erc20FreeGasAddressStr             = "0xAD1D01007a56EE0A4FFD0488fb58fC6500Cb1fbE"
)

func TestGetBatchSealTime(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	// latest batch seal time
	var batchNum uint64
	var batchSealTime uint64
	var err error
	for i := 0; i < 50; i++ {
		batchNum, err = operations.GetBatchNumber()
		require.NoError(t, err)
		batchSealTime, err = operations.GetBatchSealTime(new(big.Int).SetUint64(batchNum))
		require.Equal(t, batchSealTime, uint64(0))
		log.Infof("Batch number: %d, times:%v", batchNum, i)
		if batchNum > 1 {
			break
		}
		time.Sleep(1 * time.Second)
	}

	// old batch seal time
	batchNum = batchNum - 1
	batch, err := operations.GetBatchByNumber(new(big.Int).SetUint64(batchNum))
	var maxTime uint64
	for _, block := range batch.Blocks {
		blockInfo, err := operations.GetBlockByHash(common.HexToHash(block.(string)))
		require.NoError(t, err)
		log.Infof("Block Timestamp: %+v", blockInfo.Timestamp)
		blockTime := uint64(blockInfo.Timestamp)
		if blockTime > maxTime {
			maxTime = blockTime
		}
	}
	batchSealTime, err = operations.GetBatchSealTime(new(big.Int).SetUint64(batchNum))
	require.NoError(t, err)
	log.Infof("Max block time: %d, batchSealTime: %d", maxTime, batchSealTime)
	require.Equal(t, maxTime, batchSealTime)
}

func TestBridgeTx(t *testing.T) {
	ctx := context.Background()
	l1Client, err := ethclient.Dial(operations.DefaultL1NetworkURL)
	require.NoError(t, err)
	l2Client, err := ethclient.Dial(operations.DefaultL2NetworkURL)
	require.NoError(t, err)
	transToken(t, ctx, l2Client, uint256.NewInt(encoding.Gwei), operations.DefaultL2AdminAddress)

	amount := new(big.Int).SetUint64(100)
	//layer2 network id
	var destNetwork uint32 = 1
	destAddr := common.HexToAddress(operations.DefaultL2AdminAddress)
	auth, err := operations.GetAuth(operations.DefaultL1AdminPrivateKey, operations.DefaultL1ChainID)
	require.NoError(t, err)

	wethAddress := common.HexToAddress("0x95076baf95000f2e67b2f88998a26d82140308ca")
	wethToken, err := operations.NewToken(wethAddress, l2Client)
	require.NoError(t, err)
	balanceBefore, err := wethToken.BalanceOf(&bind.CallOpts{}, destAddr)
	require.NoError(t, err)
	log.Infof("balanceBefore:%d", balanceBefore)

	err = sendBridgeAsset(ctx, common.Address{}, amount, destNetwork, &destAddr, []byte{}, auth, common.HexToAddress(operations.BridgeAddr), l1Client)
	require.NoError(t, err)

	const maxAttempts = 120

	var balanceAfter *big.Int
	for i := 0; i < maxAttempts; i++ {
		time.Sleep(1 * time.Second)

		balanceAfter, err = wethToken.BalanceOf(&bind.CallOpts{}, destAddr)
		require.NoError(t, err)
		log.Infof("balanceAfter:%d", balanceAfter)

		if balanceAfter.Cmp(balanceBefore) > 0 {
			return
		}
	}

	t.Errorf("bridge transaction failed after %d seconds: balance did not increase (before: %s, after: %s)",
		maxAttempts,
		balanceBefore.String(),
		balanceAfter.String(),
	)
}

func TestClaimTx(t *testing.T) {
	ctx := context.Background()
	client, err := ethclient.Dial(operations.DefaultL2NetworkURL)
	transToken(t, ctx, client, uint256.NewInt(encoding.Gwei), operations.DefaultL2AdminAddress)

	from := common.HexToAddress(operations.DefaultL2AdminAddress)
	to := common.HexToAddress(operations.DefaultL2AdminAddress)
	nonce, err := client.PendingNonceAt(ctx, from)
	gas, err := client.EstimateGas(ctx, ethereum.CallMsg{
		From:  from,
		To:    &to,
		Value: uint256.NewInt(10),
	})
	require.NoError(t, err)
	var tx types.Transaction = &types.LegacyTx{
		CommonTx: types.CommonTx{
			Nonce: nonce,
			To:    &to,
			Gas:   gas,
			Value: uint256.NewInt(10),
		},
		GasPrice: uint256.MustFromBig(big.NewInt(0)),
	}

	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(operations.DefaultL2AdminPrivateKey, "0x"))
	require.NoError(t, err)

	signer := types.MakeSigner(operations.GetTestChainConfig(operations.DefaultL2ChainID), 1, 0)
	signedTx, err := types.SignTx(tx, *signer, privateKey)
	require.NoError(t, err)

	err = client.SendTransaction(ctx, signedTx)
	require.NoError(t, err)

	err = operations.WaitTxToBeMined(ctx, client, signedTx, operations.DefaultTimeoutTxToBeMined)
	require.NoError(t, err)
}

func TestNewAccFreeGas(t *testing.T) {
	ctx := context.Background()
	client, _ := ethclient.Dial(operations.DefaultL2NetworkURL)
	transToken(t, ctx, client, uint256.NewInt(encoding.Gwei), operations.DefaultL2AdminAddress)
	var gas uint64 = 21000

	//newAcc transfer failed
	from := common.HexToAddress(operations.DefaultL2NewAcc2Address)
	to := common.HexToAddress(operations.DefaultL2AdminAddress)
	nonce, err := client.PendingNonceAt(ctx, from)
	require.NoError(t, err)
	var tx types.Transaction = &types.LegacyTx{
		CommonTx: types.CommonTx{
			Nonce: nonce,
			To:    &to,
			Gas:   gas,
			Value: uint256.NewInt(0),
		},
		GasPrice: uint256.MustFromBig(big.NewInt(0)),
	}
	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(operations.DefaultL2NewAcc2PrivateKey, "0x"))
	require.NoError(t, err)
	signer := types.MakeSigner(operations.GetTestChainConfig(operations.DefaultL2ChainID), 1, 0)
	signedTx, err := types.SignTx(tx, *signer, privateKey)
	require.NoError(t, err)
	err = client.SendTransaction(ctx, signedTx)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "RPC error response: FEE_TOO_LOW: underpriced"), "Expected error message not found")
	err = operations.WaitTxToBeMined(ctx, client, signedTx, 5)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "context deadline exceeded"), "Expected error message not found")

	// seq -> newAcc
	from = common.HexToAddress(operations.DefaultL2AdminAddress)
	to = common.HexToAddress(operations.DefaultL2NewAcc2Address)
	nonce, err = client.PendingNonceAt(ctx, from)
	require.NoError(t, err)
	tx = &types.LegacyTx{
		CommonTx: types.CommonTx{
			Nonce: nonce,
			To:    &to,
			Gas:   gas,
			Value: uint256.NewInt(10),
		},
		GasPrice: uint256.MustFromBig(big.NewInt(0)),
	}
	privateKey, err = crypto.HexToECDSA(strings.TrimPrefix(operations.DefaultL1AdminPrivateKey, "0x"))
	require.NoError(t, err)
	signedTx, err = types.SignTx(tx, *signer, privateKey)
	require.NoError(t, err)
	err = client.SendTransaction(ctx, signedTx)
	require.NoError(t, err)
	err = operations.WaitTxToBeMined(ctx, client, signedTx, operations.DefaultTimeoutTxToBeMined)
	require.NoError(t, err)

	// newAcc transfer success
	from = common.HexToAddress(operations.DefaultL2NewAcc2Address)
	to = common.HexToAddress(operations.DefaultL2AdminAddress)
	nonce, err = client.PendingNonceAt(ctx, from)
	require.NoError(t, err)
	tx = &types.LegacyTx{
		CommonTx: types.CommonTx{
			Nonce: nonce,
			To:    &to,
			Gas:   gas,
			Value: uint256.NewInt(0),
		},
		GasPrice: uint256.MustFromBig(big.NewInt(0)),
	}
	privateKey, err = crypto.HexToECDSA(strings.TrimPrefix(operations.DefaultL2NewAcc2PrivateKey, "0x"))
	require.NoError(t, err)
	signedTx, err = types.SignTx(tx, *signer, privateKey)
	require.NoError(t, err)
	err = client.SendTransaction(ctx, signedTx)
	require.NoError(t, err)
	err = operations.WaitTxToBeMined(ctx, client, signedTx, operations.DefaultTimeoutTxToBeMined)
	require.NoError(t, err)
}

// Note: this function is used to test removeTransaction function for the sequencer
func TestSequencerRemoveInvalidTransferTokenFrom(t *testing.T) {
	ctx := context.Background()
	seqClient, err := ethclient.Dial(operations.DefaultL2SeqURL)
	log.Infof("=== connect to : %v", operations.DefaultL2SeqURL)
	if err != nil {
		log.Fatalf("Failed to connect to L2 client: %v", err)
	}
	log.Infof("======= Start TestInvalidTransaferTokenFrom and remove transaction ======")
	// sealing case
	var (
		wg sync.WaitGroup
	)
	sealingTxsCount := 1000
	txsList1 := []types.Transaction{}
	// transfer some token to the rich account
	log.Infof("## init tokens to the rich account, from: %v, to: %v", operations.DefaultL2AdminAddress, operations.DefaultRichAddress)
	adminNonce := getNonce(seqClient, ctx, operations.DefaultL2AdminPrivateKey)
	for i := 0; i < sealingTxsCount; i++ {
		tx := generateSignedTokenTransferTx(t, ctx, seqClient, operations.DefaultL2AdminPrivateKey,
			new(uint256.Int).Mul(uint256.NewInt(1), uint256.NewInt(1e18)), operations.DefaultRichAddress, adminNonce+(uint64(i)))
		txsList1 = append(txsList1, tx)
		err = seqClient.SendTransaction(ctx, tx)
		if err != nil {
			log.Infof("* === !!! TestInvalidTransaferTokenFrom: SendTransaction err: %v", err)
		}
		require.NoError(t, err)
	}
	// wait for mined
	for _, tx1 := range txsList1 {
		err := operations.WaitTxToBeMined(ctx, seqClient, tx1, operations.DefaultTimeoutTxToBeMined)
		if err != nil {
			log.Infof("* === !!! TestInvalidTransaferTokenFrom: WaitTxToBeMined err when transfer token to the rich account, error: %v", err)
		}
		require.NoError(t, err)
	}

	status, err := operations.TxPoolStatus()
	require.NoError(t, err)
	log.Infof("## Transaction status after init the rich account: %v", status)
	log.Infof("### init tokens to the rich account success, from: %v, to: %v", operations.DefaultL2AdminAddress, operations.DefaultRichAddress)

	// the rich account transactions(async)
	richAccountNonce := getNonce(seqClient, ctx, operations.DefaultRichPrivateKey)
	wg.Add(1)
	txsList2 := []types.Transaction{}
	go func(ctx context.Context) {
		defer wg.Done()
		//log.Infof("* The rich account transfer tokens, richAccount: %v", operations.DefaultRichAddress)
		for i := 0; i < sealingTxsCount-1; i++ {
			tx := generateSignedTokenTransferTx(t, ctx, seqClient, operations.DefaultRichPrivateKey,
				new(uint256.Int).Mul(uint256.NewInt(1), uint256.NewInt(1e18)), operations.DefaultL2AdminAddress, richAccountNonce+uint64(i))
			txsList2 = append(txsList2, tx)
			sendErr := seqClient.SendTransaction(ctx, tx)
			if sendErr != nil {
				log.Infof("* === !!! TestInvalidTransaferTokenFrom: SendTransaction err: %v", sendErr)
			}
			require.NoError(t, sendErr)
		}
		for _, tx2 := range txsList2 {
			errTransferToken := operations.WaitTxToBeMined(ctx, seqClient, tx2, operations.DefaultTimeoutTxToBeMined)
			if errTransferToken != nil {
				log.Infof("* === !!! TestInvalidTransaferTokenFrom: WaitTxToBeMined err when transfer token between the rich account, error: %v", errTransferToken)
			}
			require.NoError(t, errTransferToken)
		}
		txpoolStatus, err1 := operations.TxPoolStatus()
		require.NoError(t, err1)
		log.Infof("* Transaction status after transfer token between the rich account: %v", txpoolStatus)
		log.Infof("* The rich account transfer tokens success, richAccount: %v", operations.DefaultRichAddress)
	}(ctx)

	txsCount := 1000
	// all pending case
	startNonce := getNonce(seqClient, ctx, operations.DefaultL2AdminPrivateKey) + 1
	txsToRemove := common.Hash{}
	log.Infof("---The pending case test ---")
	txsList3 := []types.Transaction{}
	for i := 0; i < txsCount; i++ {
		nonce := startNonce + (uint64(i))
		//log.Infof("* Submitting Pending transaction with discontinuous nonce, nonce: %v", nonce)
		signedTx := generateSignedTokenTransferTx(t, ctx, seqClient, operations.DefaultL2AdminPrivateKey,
			new(uint256.Int).Mul(uint256.NewInt(1), uint256.NewInt(1e18)), operations.DefaultL2AdminAddress, nonce)
		if i == 0 {
			txsToRemove = signedTx.Hash()
		} else {
			txsList3 = append(txsList3, signedTx)
		}
		err = seqClient.SendTransaction(ctx, signedTx)
		if err != nil {
			log.Infof("* === !!!  TestInvalidTransaferTokenFrom: SendTransaction err: %v", err)
		}
		require.NoError(t, err)
	}
	log.Infof("---Finish the pending case test ---")

	status, err = operations.TxPoolStatus()
	require.NoError(t, err)
	log.Infof("* Transaction status before remove transaction: %v", status)
	// Assert txpool pending count meets expectation before removal
	queuedHex, _ := status["queued"].(string)
	// Convert hex string (e.g., 0x2e) to integer and compare
	queuedCount, err := strconv.ParseInt(queuedHex, 0, 32)
	require.NoError(t, err)
	require.Equal(t, txsCount, int(queuedCount))

	// Helper: check if a tx hash exists inside txpool_content recursively
	txInPool := func(hash common.Hash) bool {
		content, err := operations.TxPoolContent()
		if err != nil || content == nil {
			return false
		}
		found := false
		var walk func(v any)
		walk = func(v any) {
			switch vv := v.(type) {
			case map[string]any:
				for _, inner := range vv {
					if found {
						return
					}
					walk(inner)
				}
			case []any:
				for _, inner := range vv {
					if found {
						return
					}
					walk(inner)
				}
			case string:
				if strings.EqualFold(vv, hash.Hex()) {
					found = true
				}
			}
		}
		walk(content)
		return found
	}

	// Ensure the transaction to remove is currently discoverable (RPC or txpool)
	txInfoBefore, err := operations.EthGetTransactionByHash(txsToRemove)
	require.NoError(t, err)
	require.NotNil(t, txInfoBefore)
	// require.True(t, txInPool(txsToRemove), "transaction must be found in txpool before removal")
	// remove the transaction
	log.Infof("* Remove transaction: %v, nonce: %v", txsToRemove.Hex(), startNonce)
	err = operations.RemoveTransaction(operations.DefaultL2SeqURL, txsToRemove)
	require.True(t, err == nil)
	// check the transaction status
	status, err = operations.TxPoolStatus()
	require.NoError(t, err)
	log.Infof("* Transaction status after remove transaction: %v, nonce: %v", status, startNonce)

	// Verify the transaction is no longer discoverable after removal (allow brief propagation time)
	for i := 0; i < 20; i++ {
		txInfoAfter, err := operations.EthGetTransactionByHash(txsToRemove)
		require.NoError(t, err)
		require.Nil(t, txInfoAfter)
		// also confirm it does not exist in txpool
		require.False(t, txInPool(txsToRemove))
		time.Sleep(200 * time.Millisecond)
	}

	// complement the transaction
	log.Infof("---Test complement case ---")
	startNonce -= 1
	for i := 0; i < 2; i++ {
		nonce := startNonce + uint64(i)
		log.Infof("* Complement the transaction, nonce: %v ", nonce)
		signedTx := generateSignedTokenTransferTx(t, ctx, seqClient, operations.DefaultL2AdminPrivateKey,
			new(uint256.Int).Mul(uint256.NewInt(1), uint256.NewInt(1e18)), operations.DefaultL2AdminAddress, nonce)
		err = seqClient.SendTransaction(ctx, signedTx)
		if err != nil {
			log.Infof("* === !!!  TestInvalidTransaferTokenFrom: complement transaction and send err: %v, txHash: %v", err, signedTx.Hash())
		}
		require.NoError(t, err)
		txsList3 = append(txsList3, signedTx)
	}
	for _, tx3 := range txsList3 {
		errSendComplementTx := operations.WaitTxToBeMined(ctx, seqClient, tx3, operations.DefaultTimeoutTxToBeMined)
		if errSendComplementTx != nil {
			log.Infof("* === !!!  TestInvalidTransaferTokenFrom: WaitTxToBeMined err when complement the transaction, error: %v", errSendComplementTx)
		}
		require.NoError(t, errSendComplementTx)
	}

	log.Infof("---Test complement case finish ---")
	wg.Wait()
	// check the transaction status
	status, err = operations.TxPoolStatus()
	require.NoError(t, err)
	log.Infof("* Transaction status after complement the transaction: %v", status)
	/// check the status
	log.Infof("### check txpool status")
	require.Equal(t, status["baseFee"].(string), "0x0")
	require.Equal(t, status["pending"].(string), "0x0")
	require.Equal(t, status["queued"].(string), "0x0")
	log.Infof("### check txpool status success")
	// check the on-chain information
	log.Infof("### check chain status")
	for _, complementTx := range txsList3 {
		resultTx, pending, err := seqClient.TransactionByHash(ctx, complementTx.Hash())
		require.NoError(t, err)
		require.Equal(t, complementTx.Hash(), resultTx.Hash())
		require.Equal(t, pending, false)
	}
	log.Infof("### check chain status success")
	log.Infof("==== TestInvalidTransaferTokenFrom and remove transaction successfully ===")
}

func TestWhiteAndBlockList(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	ctx := context.Background()
	client, err := ethclient.Dial(operations.DefaultL2NetworkURL)
	require.NoError(t, err)

	from := common.HexToAddress(operations.DefaultL2AdminAddress)
	blockAddressConverted := common.HexToAddress(blockAddress)
	nonBlockAddress := common.HexToAddress(operations.DefaultL2NewAcc1Address)

	nonce, err := client.PendingNonceAt(ctx, from)
	require.NoError(t, err)

	gasPrice, err := client.SuggestGasPrice(ctx)
	require.NoError(t, err)

	gas, err := client.EstimateGas(ctx, ethereum.CallMsg{
		From:  from,
		To:    &blockAddressConverted,
		Value: uint256.NewInt(10),
	})
	require.NoError(t, err)

	var txToBlockAddress types.Transaction = &types.LegacyTx{
		CommonTx: types.CommonTx{
			Nonce: nonce,
			To:    &blockAddressConverted,
			Gas:   gas,
			Value: uint256.NewInt(10),
		},
		GasPrice: uint256.MustFromBig(gasPrice),
	}

	var txToNonBlockAddress types.Transaction = &types.LegacyTx{
		CommonTx: types.CommonTx{
			Nonce: nonce,
			To:    &nonBlockAddress,
			Gas:   gas,
			Value: uint256.NewInt(10),
		},
		GasPrice: uint256.MustFromBig(gasPrice),
	}

	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(operations.DefaultL2AdminPrivateKey, "0x"))
	require.NoError(t, err)

	signer := types.MakeSigner(operations.GetTestChainConfig(operations.DefaultL2ChainID), 1, 0)

	signedTxToBlockAddress, err := types.SignTx(txToBlockAddress, *signer, privateKey)
	require.NoError(t, err)

	err = client.SendTransaction(ctx, signedTxToBlockAddress)
	log.Infof("err:%v", err)
	require.True(t, strings.Contains(err.Error(), "INTERNAL_ERROR: blocked receiver"))

	signedTxToNonBlockAddress, err := types.SignTx(txToNonBlockAddress, *signer, privateKey)
	require.NoError(t, err)

	err = client.SendTransaction(ctx, signedTxToNonBlockAddress)
	require.NoError(t, err)

	//TODO: sender in blocklist should fail
	//now only admin account have balance. So we may add another account that has balance.
}

func TestRPCAPI(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	config, err := LoadConfig("../../test/config/test.erigon.rpc.config.yaml")
	require.NoError(t, err)

	if config.HTTPAPIKeys != "" {

		_, err := operations.GetEthSyncing(operations.DefaultL2NetworkURL)
		require.Error(t, err)
		require.True(t, strings.Contains(err.Error(), "no authentication"))

		_, err = operations.GetEthSyncing(operations.DefaultL2NetworkURL + "/45543e0adc5dd3e316044909d32501a5")
		require.NoError(t, err)
	} else {

		var rateErr error
		for i := 0; i < 1000; i++ {
			_, err1 := operations.GetEthSyncing(operations.DefaultL2NetworkURL)
			if err1 != nil {
				rateErr = err1
				break
			}
		}

		require.True(t, strings.Contains(rateErr.Error(), "rate limit exceeded"))
	}
}

func TestChainID(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	chainID, err := operations.GetNetVersion(operations.DefaultL2NetworkURL)
	require.NoError(t, err)
	require.Equal(t, chainID, operations.DefaultL2ChainID)
}

func TestInnerTx(t *testing.T) {
	ctx := context.Background()
	client, err := ethclient.Dial(operations.DefaultL2NetworkURL)
	require.NoError(t, err)
	txHash := transToken(t, ctx, client, uint256.NewInt(encoding.Gwei), operations.DefaultL2AdminAddress)
	log.Infof("txHash: %s", txHash)

	result, err := operations.GetInternalTransactions(common.HexToHash(txHash))
	require.NoError(t, err)
	require.Greater(t, len(result), 0)
	require.Equal(t, result[0].From, operations.DefaultL2AdminAddress)

	tx, err := operations.GetTransactionByHash(common.HexToHash(txHash))
	require.NoError(t, err)
	log.Infof("tx: %+v", tx.BlockNumber)
	result1, err := operations.GetBlockInternalTransactions(new(big.Int).SetUint64(uint64(*tx.BlockNumber)))
	require.NoError(t, err)
	require.Greater(t, len(result1), 0)
	require.Equal(t, result1[common.HexToHash(txHash)][0].From, operations.DefaultL2AdminAddress)
}

func TestEthTransfer(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	if !testVerified {
		return
	}

	ctx := context.Background()
	auth, err := operations.GetAuth(operations.DefaultL2AdminPrivateKey, operations.DefaultL2ChainID)
	require.NoError(t, err)
	client, err := ethclient.Dial(operations.DefaultL2NetworkURL)
	require.NoError(t, err)

	from := common.HexToAddress(operations.DefaultL2AdminAddress)
	to := common.HexToAddress(operations.DefaultL2NewAcc1Address)
	nonce, err := client.PendingNonceAt(ctx, from)
	require.NoError(t, err)
	var tx types.Transaction = &types.LegacyTx{
		CommonTx: types.CommonTx{
			Nonce: nonce,
			To:    &to,
			Gas:   21000,
			Value: uint256.NewInt(0),
		},
		GasPrice: uint256.NewInt(10 * encoding.Gwei),
	}
	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(operations.DefaultL2AdminPrivateKey, "0x"))
	require.NoError(t, err)
	signer := types.MakeSigner(operations.GetTestChainConfig(operations.DefaultL2ChainID), 1, 0)
	signedTx, err := types.SignTx(tx, *signer, privateKey)
	var txs []*types.Transaction
	txs = append(txs, &signedTx)
	_, err = operations.ApplyL2Txs(ctx, txs, auth, client, operations.VerifiedConfirmationLevel)
	require.NoError(t, err)
}

func TestGasPrice(t *testing.T) {
	ctx := context.Background()
	client, err := ethclient.Dial(operations.DefaultL2NetworkURL)
	log.Infof("Start TestGasPrice")
	gasPrice1, err := operations.GetGasPrice()
	gasPrice2 := gasPrice1
	require.NoError(t, err)
	for i := 1; i < 100; i++ {
		temp, err := operations.GetGasPrice()
		require.NoError(t, err)
		if temp > gasPrice2 {
			gasPrice2 = temp
		}
		require.NoError(t, err)

		from := common.HexToAddress(operations.DefaultL2AdminAddress)
		to := common.HexToAddress(operations.DefaultL2NewAcc1Address)
		nonce, err := client.PendingNonceAt(ctx, from)
		require.NoError(t, err)
		var tx types.Transaction = &types.LegacyTx{
			CommonTx: types.CommonTx{
				Nonce: nonce,
				To:    &to,
				Gas:   21000,
				Value: uint256.NewInt(0),
			},
			GasPrice: uint256.NewInt(uint64(i) * 10 * encoding.Gwei),
		}
		privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(operations.DefaultL2AdminPrivateKey, "0x"))
		require.NoError(t, err)
		signer := types.MakeSigner(operations.GetTestChainConfig(operations.DefaultL2ChainID), 1, 0)
		signedTx, err := types.SignTx(tx, *signer, privateKey)
		require.NoError(t, err)
		log.Infof("Get new GP:%v, TXGP:%v", temp, tx.GetPrice())
		err = client.SendTransaction(ctx, signedTx)
		time.Sleep(500 * time.Millisecond)
		//err = operations.WaitTxToBeMined(ctx, client, signedTx, operations.DefaultTimeoutTxToBeMined)
		//require.NoError(t, err)
		if gasPrice2 > gasPrice1 {
			log.Infof("GP compare ok: [%d,%d]", gasPrice1, gasPrice2)
			break
		}
	}
	require.NoError(t, err)
	log.Infof("gasPrice: [%d,%d]", gasPrice1, gasPrice2)
	require.Greater(t, gasPrice2, gasPrice1)
}

func TestMetrics(t *testing.T) {
	result, err := operations.GetMetricsPrometheus()
	require.NoError(t, err)
	require.Equal(t, strings.Contains(result, "xlayer_operation_timing_seconds"), true)
	//require.Equal(t, strings.Contains(result, "sequencer_pool_tx_count"), true)

	// TODO: enable this test after metrics are enabled
	//result, err = operations.GetMetrics()
	//require.NoError(t, err)
	//require.Equal(t, strings.Contains(result, "zkevm_getBatchWitness"), true)
	//require.Equal(t, strings.Contains(result, "eth_sendRawTransaction"), true)
	//require.Equal(t, strings.Contains(result, "eth_getTransactionCount"), true)
}

func transToken(t *testing.T, ctx context.Context, client *ethclient.Client, amount *uint256.Int, toAddress string) string {
	return transTokenWithFrom(t, ctx, client, operations.DefaultL2AdminPrivateKey, amount, toAddress)
}

func getNonce(client *ethclient.Client, ctx context.Context, fromPrivateKey string) uint64 {
	chainID, err := client.ChainID(ctx)
	if err != nil {
		log.Infof("Get nonce err for get chainID failed: %v", err)
	}
	auth, err := operations.GetAuth(fromPrivateKey, chainID.Uint64())
	if err != nil {
		log.Infof("Get nonce err for get auth failed: %v", err)
	}
	nonce, err := client.PendingNonceAt(ctx, auth.From)
	if err != nil {
		log.Infof("Get nonce err for PendingNonceAt failed: %v", err)
	}
	return nonce
}

func transTokenWithFrom(t *testing.T, ctx context.Context, client *ethclient.Client, fromPrivateKey string, amount *uint256.Int, toAddress string) string {
	return transTokenWithFromImpl(t, ctx, client, fromPrivateKey, amount, toAddress, getNonce(client, ctx, fromPrivateKey))
}

func generateSignedTokenTransferTx(t *testing.T, ctx context.Context, client *ethclient.Client, fromPrivateKey string, amount *uint256.Int, toAddress string, nonce uint64) types.Transaction {
	chainID, err := client.ChainID(ctx)
	require.NoError(t, err)
	auth, err := operations.GetAuth(fromPrivateKey, chainID.Uint64())
	gasPrice, err := client.SuggestGasPrice(ctx)
	require.NoError(t, err)

	to := common.HexToAddress(toAddress)
	gas, err := client.EstimateGas(ctx, ethereum.CallMsg{
		From:  auth.From,
		To:    &to,
		Value: amount,
	})
	require.NoError(t, err)

	var tx types.Transaction = &types.LegacyTx{
		CommonTx: types.CommonTx{
			Nonce: nonce,
			To:    &to,
			Gas:   gas,
			Value: amount,
		},
		GasPrice: uint256.MustFromBig(gasPrice),
	}

	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(fromPrivateKey, "0x"))
	require.NoError(t, err)

	signer := types.MakeSigner(operations.GetTestChainConfig(operations.DefaultL2ChainID), 1, 0)
	signedTx, err := types.SignTx(tx, *signer, privateKey)
	require.NoError(t, err)
	log.Infof("gas: %d, gasPrice: %d, nonce: %d, hash: %v", gas, gasPrice, nonce, signedTx.Hash().Hex())
	return signedTx
}

func transTokenWithFromImpl(t *testing.T, ctx context.Context, client *ethclient.Client, fromPrivateKey string, amount *uint256.Int, toAddress string, nonce uint64) string {
	signedTx := generateSignedTokenTransferTx(t, ctx, client, fromPrivateKey, amount, toAddress, nonce)
	err := client.SendTransaction(ctx, signedTx)
	require.NoError(t, err)

	err = operations.WaitTxToBeMined(ctx, client, signedTx, operations.DefaultTimeoutTxToBeMined)
	require.NoError(t, err)

	return signedTx.Hash().String()
}

func TestMinGasPrice(t *testing.T) {
	ctx := context.Background()
	client, err := ethclient.Dial(operations.DefaultL2NetworkURL)
	log.Infof("Start TestMinGasPrice")
	require.NoError(t, err)
	for i := 1; i < 3; i++ {
		temp, err := operations.GetMinGasPrice()
		log.Infof("minGP: [%d]", temp)
		if temp > 1 {
			temp = temp - 1
		}
		require.NoError(t, err)

		from := common.HexToAddress(operations.DefaultL2NewAcc2Address)
		to := common.HexToAddress(operations.DefaultL1AdminAddress)
		nonce, err := client.PendingNonceAt(ctx, from)
		require.NoError(t, err)
		var tx types.Transaction = &types.LegacyTx{
			CommonTx: types.CommonTx{
				Nonce: nonce,
				To:    &to,
				Gas:   21000,
				Value: uint256.NewInt(0),
			},
			GasPrice: uint256.NewInt(temp),
		}
		privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(operations.DefaultL2NewAcc2PrivateKey, "0x"))
		require.NoError(t, err)
		signer := types.MakeSigner(operations.GetTestChainConfig(operations.DefaultL2ChainID), 1, 0)
		signedTx, err := types.SignTx(tx, *signer, privateKey)
		require.NoError(t, err)
		log.Infof("GP:%v", tx.GetPrice())
		err = client.SendTransaction(ctx, signedTx)
		require.Error(t, err)
	}
	for i := 3; i < 5; i++ {
		temp, err := operations.GetMinGasPrice()
		log.Infof("minGP: [%d]", temp)
		require.NoError(t, err)

		from := common.HexToAddress(operations.DefaultL2AdminAddress)
		to := common.HexToAddress(operations.DefaultL1AdminAddress)
		nonce, err := client.PendingNonceAt(ctx, from)
		require.NoError(t, err)
		var tx types.Transaction = &types.LegacyTx{
			CommonTx: types.CommonTx{
				Nonce: nonce,
				To:    &to,
				Gas:   21000,
				Value: uint256.NewInt(0),
			},
			GasPrice: uint256.NewInt(temp),
		}
		privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(operations.DefaultL2AdminPrivateKey, "0x"))
		require.NoError(t, err)
		signer := types.MakeSigner(operations.GetTestChainConfig(operations.DefaultL2ChainID), 1, 0)
		signedTx, err := types.SignTx(tx, *signer, privateKey)
		require.NoError(t, err)
		log.Infof("GP:%v", tx.GetPrice())
		err = client.SendTransaction(ctx, signedTx)
		require.NoError(t, err)
	}
	require.NoError(t, err)
}

func sendBridgeAsset(
	ctx context.Context, tokenAddr common.Address, amount *big.Int, destNetwork uint32, destAddr *common.Address,
	metadata []byte, auth *bind.TransactOpts, bridgeSCAddr common.Address, c *ethclient.Client,
) error {
	emptyAddr := common.Address{}
	if tokenAddr == emptyAddr {
		auth.Value = amount
	}
	if destAddr == nil {
		destAddr = &auth.From
	}
	if len(bridgeSCAddr) == 0 {
		return fmt.Errorf("Bridge address error")
	}

	br, err := polygonzkevmbridge.NewPolygonzkevmbridge(bridgeSCAddr, c)
	if err != nil {
		return err
	}
	tx, err := br.BridgeAsset(auth, destNetwork, *destAddr, amount, tokenAddr, true, metadata)
	if err != nil {
		return err
	}
	// wait transfer to be included in a batch
	const txTimeout = 60 * time.Second
	return operations.WaitTxToBeMined(ctx, c, tx, txTimeout)
}

type Config struct {
	HTTPMethodRateLimit string `yaml:"http.methodratelimit"`
	HTTPAPIKeys         string `yaml:"http.apikeys"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

func TestSpecificProjectFreeGas(t *testing.T) {
	// transfer token to the new account
	tmpPrivateKey, err := crypto.HexToECDSA(tmpSenderPrivateKey)
	require.NoError(t, err)

	tmpPublicKey := tmpPrivateKey.Public()
	tmpPublicKeyECDSA, ok := tmpPublicKey.(*ecdsa.PublicKey)
	require.True(t, ok)
	tmpFromAddress := crypto.PubkeyToAddress(*tmpPublicKeyECDSA)
	ctx := context.Background()

	client, err := ethclient.Dial(operations.DefaultL2NetworkURL)
	transToken(t, ctx, client,
		new(uint256.Int).Mul(uint256.NewInt(1000), uint256.NewInt(1e18)),
		tmpFromAddress.String())

	// send to specific project sender
	privateKey, err := crypto.HexToECDSA(specificProjectSenderPrivateKey)
	require.NoError(t, err)

	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	require.True(t, ok)
	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)

	transTokenWithFrom(t, ctx, client, "0x"+tmpSenderPrivateKey,
		new(uint256.Int).Mul(uint256.NewInt(100), uint256.NewInt(1e18)),
		fromAddress.String())

	erc20FreeGasAddress := common.HexToAddress(erc20FreeGasAddressStr)

	code, err := client.CodeAt(ctx, erc20FreeGasAddress, nil)
	require.NoError(t, err)
	erc20ABI, err := abi.JSON(strings.NewReader(erc20ABIJson))
	require.NoError(t, err)
	if len(code) == 0 {
		// Fetch nonce
		nonce, err := client.PendingNonceAt(ctx, fromAddress)
		require.NoError(t, err)

		log.Infof("Nonce: %d", nonce)

		// Define gas parameters
		gasPrice, err := client.SuggestGasPrice(ctx)
		require.NoError(t, err)

		// Set up transaction options
		auth, err := bind.NewKeyedTransactorWithChainID(privateKey, big.NewInt(195))
		require.NoError(t, err)

		auth.Nonce = big.NewInt(int64(nonce))
		auth.Value = big.NewInt(0)
		auth.GasLimit = uint64(3000000)
		auth.GasPrice = gasPrice

		// Deploy the contract
		erc20Bytecode, err := hex.DecodeString(erc20BytecodeStr)
		require.NoError(t, err)
		erc20Address, tx, _, err := bind.DeployContract(auth, erc20ABI, erc20Bytecode, client)
		require.NoError(t, err)

		log.Infof("Contract deployed at: %s, transaction hash: %s", erc20Address.Hex(), tx.Hash().Hex())

		// Wait for contract deployment to be mined
		bind.WaitDeployed(ctx, client, tx)
	}

	amount := new(big.Int).Mul(big.NewInt(1), big.NewInt(1e18)) // Adjust for token decimals (18 in this case)
	// Prepare transfer data
	data, err := erc20ABI.Pack("transfer", common.HexToAddress(blockAddress), amount)
	require.NoError(t, err)
	// Get the sender's nonce
	freeGasNonce, err := client.PendingNonceAt(context.Background(), fromAddress)
	require.NoError(t, err)

	// Create the transaction with free gas
	freeGasTx := &types.LegacyTx{
		CommonTx: types.CommonTx{
			Nonce: freeGasNonce,
			To:    &erc20FreeGasAddress,
			Gas:   60000,
			Value: uint256.NewInt(0),
			Data:  data,
		},
		GasPrice: uint256.NewInt(0),
	}

	signer := types.MakeSigner(operations.GetTestChainConfig(operations.DefaultL2ChainID), 1, 0)
	signedTx, err := types.SignTx(freeGasTx, *signer, privateKey)
	require.NoError(t, err)
	err = client.SendTransaction(ctx, signedTx)
	require.NoError(t, err)
	err = operations.WaitTxToBeMined(ctx, client, signedTx, operations.DefaultTimeoutTxToBeMined)
	require.NoError(t, err)
	receipt, err := client.TransactionReceipt(ctx, signedTx.Hash())
	require.NoError(t, err)
	log.Infof("receipt: %+v", receipt)

	// Send with gas price
	freeGasNonceWithGp, err := client.PendingNonceAt(context.Background(), fromAddress)
	require.NoError(t, err)
	freeGasTxWithGp := &types.LegacyTx{
		CommonTx: types.CommonTx{
			Nonce: freeGasNonceWithGp,
			To:    &erc20FreeGasAddress,
			Gas:   60000,
			Value: uint256.NewInt(0),
			Data:  data,
		},
		GasPrice: uint256.NewInt(100),
	}

	signerWithGp := types.MakeSigner(operations.GetTestChainConfig(operations.DefaultL2ChainID), 1, 0)
	signedTxWithGp, err := types.SignTx(freeGasTxWithGp, *signerWithGp, privateKey)
	require.NoError(t, err)
	err = client.SendTransaction(ctx, signedTxWithGp)
	require.NoError(t, err)
	err = operations.WaitTxToBeMined(ctx, client, signedTxWithGp, operations.DefaultTimeoutTxToBeMined)
	require.NoError(t, err)
	receiptWithGp, err := client.TransactionReceipt(ctx, signedTx.Hash())
	require.NoError(t, err)
	log.Infof("receipt: %+v", receiptWithGp)

	// send to non specific project sender
	privateKeyNon, err := crypto.HexToECDSA(nonSpecificProjectSenderPrivateKay)
	require.NoError(t, err)

	publicKeyNon := privateKeyNon.Public()
	publicKeyECDSANon, ok := publicKeyNon.(*ecdsa.PublicKey)
	require.True(t, ok)
	fromAddressNon := crypto.PubkeyToAddress(*publicKeyECDSANon)

	transTokenWithFrom(t, ctx, client, "0x"+tmpSenderPrivateKey,
		new(uint256.Int).Mul(uint256.NewInt(100), uint256.NewInt(1e18)),
		fromAddressNon.String())

	// not allowed from address
	freeGasNonceNon, err := client.PendingNonceAt(context.Background(), fromAddressNon)
	require.NoError(t, err)
	freeGasTxNon := &types.LegacyTx{
		CommonTx: types.CommonTx{
			Nonce: freeGasNonceNon,
			To:    &erc20FreeGasAddress,
			Gas:   60000,
			Value: uint256.NewInt(0),
			Data:  data,
		},
		GasPrice: uint256.NewInt(100),
	}

	signerNon := types.MakeSigner(operations.GetTestChainConfig(operations.DefaultL2ChainID), 1, 0)
	signedTxNon, err := types.SignTx(freeGasTxNon, *signerNon, privateKeyNon)
	require.NoError(t, err)
	err = client.SendTransaction(ctx, signedTxNon)
	require.ErrorContains(t, err, "FEE_TOO_LOW")
}

func TestRPC(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	// latest batch seal time
	var batchNum uint64
	var batchSealTime uint64
	var err error
	for i := 0; i < 50; i++ {
		batchNum, err = operations.GetBatchNumber()
		require.NoError(t, err)
		batchSealTime, err = operations.GetBatchSealTime(new(big.Int).SetUint64(batchNum))
		require.Equal(t, batchSealTime, uint64(0))
		log.Infof("Batch number: %d, times:%v", batchNum, i)
		if batchNum > 1 {
			break
		}
		time.Sleep(1 * time.Second)
	}

	// old batch seal time
	batchNum = batchNum - 1
	batch, err := operations.GetBatchByNumber(new(big.Int).SetUint64(batchNum))
	var maxTime uint64
	for _, block := range batch.Blocks {
		blockInfo, err := operations.GetBlockByHash(common.HexToHash(block.(string)))
		require.NoError(t, err)
		log.Infof("Block Timestamp: %+v", blockInfo.Timestamp)
		blockTime := uint64(blockInfo.Timestamp)
		if blockTime > maxTime {
			maxTime = blockTime
		}
	}
	batchSealTime, err = operations.GetBatchSealTime(new(big.Int).SetUint64(batchNum))
	require.NoError(t, err)
	log.Infof("Max block time: %d, batchSealTime: %d", maxTime, batchSealTime)
	require.Equal(t, maxTime, batchSealTime)
}

func TestDebugTraceRPC(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	// Wait for at least one block to be available
	var blockNumber uint64
	var err error
	for i := 0; i < 30; i++ {
		blockNumber, err = operations.GetBlockNumber()
		require.NoError(t, err)
		log.Infof("Block number: %d, attempt: %v", blockNumber, i)
		if blockNumber > 3 {
			break
		}
		time.Sleep(1 * time.Second)
	}
	require.Greater(t, blockNumber, uint64(0), "Block number should be greater than 0")

	// Get a block to trace
	batchNum, err := operations.GetBatchNumber()
	require.NoError(t, err)

	batch, err := operations.GetBatchByNumber(new(big.Int).SetUint64(batchNum))
	require.NoError(t, err)
	require.NotEmpty(t, batch.Blocks, "Batch should contain at least one block")

	// Test debug_traceBlockByHash
	t.Run("DebugTraceBlockByHash", func(t *testing.T) {
		// Get the hash of the first block in the batch
		blockHash := common.HexToHash(batch.Blocks[0].(string))
		require.NotEqual(t, common.Hash{}, blockHash, "Block hash should not be empty")

		traceResult, err := operations.DebugTraceBlockByHash(blockHash)
		require.NoError(t, err)
		require.NotNil(t, traceResult, "Trace result should not be nil")

		log.Infof("DebugTraceBlockByHash result type: %T", traceResult)
	})

	// Test debug_traceBlockByNumber
	t.Run("DebugTraceBlockByNumber", func(t *testing.T) {
		traceResult, err := operations.DebugTraceBlockByNumber(1) // Trace block #1
		require.NoError(t, err)
		require.NotNil(t, traceResult, "Trace result should not be nil")

		log.Infof("DebugTraceBlockByNumber result type: %T", traceResult)
	})

	// Test debug_traceBatchByNumber
	t.Run("DebugTraceBatchByNumber", func(t *testing.T) {
		// Use batch number 1 to avoid issues with empty batches
		if batchNum > 1 {
			traceResult, err := operations.DebugTraceBatchByNumber(1)
			require.NoError(t, err)
			require.NotNil(t, traceResult, "Trace result should not be nil")

			log.Infof("DebugTraceBatchByNumber result type: %T", traceResult)
		} else {
			t.Skip("Batch number too low, skipping test")
		}
	})

	// Test debug_traceTransaction
	t.Run("DebugTraceTransaction", func(t *testing.T) {
		// Find a transaction to trace
		blockInfo, err := operations.GetBlockByHash(common.HexToHash(batch.Blocks[0].(string)))
		require.NoError(t, err)

		if len(blockInfo.Transactions) > 0 {
			// Check if we have a transaction hash directly
			if blockInfo.Transactions[0].Hash != nil {
				txHash := *blockInfo.Transactions[0].Hash
				require.NotEqual(t, common.Hash{}, txHash, "Transaction hash should not be empty")

				traceResult, err := operations.DebugTraceTransaction(txHash)
				require.NoError(t, err)
				require.NotNil(t, traceResult, "Trace result should not be nil")

				log.Infof("DebugTraceTransaction result type: %T", traceResult)
			} else {
				t.Skip("Transaction hash not available in the expected format")
			}
		} else {
			t.Skip("No transactions found in block, skipping test")
		}
	})

	// Test zkevm_getExitRootTable
	t.Run("ZKEVMGetExitRootTable", func(t *testing.T) {
		rootTable, err := operations.ZKEVMGetExitRootTable()
		require.NoError(t, err)
		require.NotNil(t, rootTable, "Exit root table should not be nil")

		log.Infof("ZKEVMGetExitRootTable result type: %T", rootTable)
	})
}

// setupTestEnvironment creates a test environment with necessary data for tests
func setupTestEnvironment(t *testing.T) (common.Hash, uint64) {
	// Wait for at least one block to be available
	var blockNumber uint64
	var err error
	for i := 0; i < 30; i++ {
		blockNumber, err = operations.GetBlockNumber()
		require.NoError(t, err)
		log.Infof("Block number: %d, attempt: %v", blockNumber, i)
		if blockNumber > 0 {
			break
		}
		time.Sleep(1 * time.Second)
	}
	require.Greater(t, blockNumber, uint64(0), "Block number should be greater than 0")

	// Get a block hash to use for tests
	batchNum, err := operations.GetBatchNumber()
	require.NoError(t, err)

	batch, err := operations.GetBatchByNumber(new(big.Int).SetUint64(batchNum))
	require.NoError(t, err)
	require.NotEmpty(t, batch.Blocks, "Batch should contain at least one block")

	blockHash := common.HexToHash(batch.Blocks[0].(string))
	require.NotEqual(t, common.Hash{}, blockHash, "Block hash should not be empty")

	return blockHash, blockNumber
}

// TestEthereumBasicRPC tests basic Ethereum RPC methods
func TestEthereumBasicRPC(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	_, _ = setupTestEnvironment(t)

	// Default test address for tests that require an address
	testAddress := common.HexToAddress("0x1234567890123456789012345678901234567890")

	// Test eth_chainId
	t.Run("EthChainID", func(t *testing.T) {
		chainID, err := operations.EthChainID()
		require.NoError(t, err)
		require.NotEqual(t, uint64(0), chainID, "Chain ID should not be zero")
		log.Infof("EthChainID result: %d", chainID)
	})

	// Test eth_syncing
	t.Run("EthSyncing", func(t *testing.T) {
		syncing, err := operations.EthSyncing()
		require.NoError(t, err)
		log.Infof("EthSyncing result: %t", syncing)
	})

	// Test eth_getBalance
	t.Run("EthGetBalance", func(t *testing.T) {
		balance, err := operations.EthGetBalance(testAddress, "latest")
		require.NoError(t, err)
		log.Infof("EthGetBalance result for test address: %s", balance.String())
	})

	// Test eth_getCode
	t.Run("EthGetCode", func(t *testing.T) {
		code, err := operations.EthGetCode(testAddress, "latest")
		require.NoError(t, err)
		log.Infof("EthGetCode result length: %d", len(code))
	})

	// Test eth_getTransactionCount
	t.Run("EthGetTransactionCount", func(t *testing.T) {
		txCount, err := operations.EthGetTransactionCount(testAddress, "latest")
		require.NoError(t, err)
		log.Infof("EthGetTransactionCount result: %d", txCount)
	})

	// Test eth_blockNumber
	t.Run("EthBlockNumber", func(t *testing.T) {
		blockNumber, err := operations.EthBlockNumber()
		require.NoError(t, err)
		require.Greater(t, blockNumber, uint64(0), "Block number should be greater than 0")
		log.Infof("EthBlockNumber result: %d", blockNumber)
	})

	// Test eth_gasPrice
	t.Run("EthGasPrice", func(t *testing.T) {
		gasPrice, err := operations.EthGasPrice()
		require.NoError(t, err)
		require.Greater(t, gasPrice.Cmp(big.NewInt(0)), 0, "Gas price should be greater than 0")
		log.Infof("EthGasPrice result: %s", gasPrice.String())
	})

	// Test eth_getStorageAt
	t.Run("EthGetStorageAt", func(t *testing.T) {
		storage, err := operations.EthGetStorageAt(testAddress, "0x0", "latest")
		require.NoError(t, err)
		log.Infof("EthGetStorageAt result: %s", storage)
	})
}

// TestEthereumBlockRPC tests Ethereum block-related RPC methods
func TestEthereumBlockRPC(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	blockHash, blockNumber := setupTestEnvironment(t)

	// Test eth_getBlockByHash
	t.Run("EthGetBlockByHash", func(t *testing.T) {
		block, err := operations.EthGetBlockByHash(blockHash, true)
		require.NoError(t, err)
		require.NotNil(t, block, "Block should not be nil")
		log.Infof("EthGetBlockByHash result type: %T", block)
	})

	// Test eth_getBlockByNumber
	t.Run("EthGetBlockByNumber", func(t *testing.T) {
		blockNumberHex := fmt.Sprintf("0x%x", blockNumber)
		block, err := operations.EthGetBlockByNumber(blockNumberHex, true)
		require.NoError(t, err)
		require.NotNil(t, block, "Block should not be nil")
		log.Infof("EthGetBlockByNumber result type: %T", block)
	})

	// Test eth_getBlockTransactionCountByHash
	t.Run("EthGetBlockTransactionCountByHash", func(t *testing.T) {
		txCount, err := operations.EthGetBlockTransactionCountByHash(blockHash)
		require.NoError(t, err)
		log.Infof("EthGetBlockTransactionCountByHash result: %d", txCount)
	})

	// Test eth_getBlockTransactionCountByNumber
	t.Run("EthGetBlockTransactionCountByNumber", func(t *testing.T) {
		txCount, err := operations.EthGetBlockTransactionCountByNumber("0x1") // Block #1
		require.NoError(t, err)
		log.Infof("EthGetBlockTransactionCountByNumber result: %d", txCount)
	})

	// Test eth_getTransactionByBlockHashAndIndex
	t.Run("EthGetTransactionByBlockHashAndIndex", func(t *testing.T) {
		tx, err := operations.EthGetTransactionByBlockHashAndIndex(blockHash, "0x0")
		require.NoError(t, err)
		log.Infof("EthGetTransactionByBlockHashAndIndex result type: %T", tx)
	})

	// Test eth_getTransactionByBlockNumberAndIndex
	t.Run("EthGetTransactionByBlockNumberAndIndex", func(t *testing.T) {
		tx, err := operations.EthGetTransactionByBlockNumberAndIndex("0x1", "0x0") // Block #1, first tx
		require.NoError(t, err)
		require.NotNil(t, tx, "Transaction should not be nil")
		log.Infof("EthGetTransactionByBlockNumberAndIndex result type: %T", tx)
	})

	// Test eth_getBlockInternalTransactions
	t.Run("EthGetBlockInternalTransactions", func(t *testing.T) {
		internalTxs, err := operations.EthGetBlockInternalTransactions("0x1") // Block #1
		require.NoError(t, err)
		require.NotNil(t, internalTxs, "Internal transactions should not be nil")
		log.Infof("EthGetBlockInternalTransactions result type: %T", internalTxs)
	})
}

// TestEthereumTransactionRPC tests Ethereum transaction-related RPC methods
func TestEthereumTransactionRPC(t *testing.T) {

	t.Run("EthEstimateGas", func(t *testing.T) {
		t.Skip("Skipping test due to insufficient funds")
	})

	t.Run("EthCall", func(t *testing.T) {
		t.Skip("Skipping test due to insufficient funds")
	})

	t.Run("EthGetTransactionByHash", func(t *testing.T) {
		t.Skip("Skipping test due to no available transactions")
	})

	t.Run("EthGetInternalTransactions", func(t *testing.T) {
		t.Skip("Skipping test due to no available transactions")
	})

	t.Run("EthGetTransactionReceipt", func(t *testing.T) {
		t.Skip("Skipping test due to no available transactions")
	})
}

// TestEthereumLogsRPC tests Ethereum logs-related RPC methods
func TestEthereumLogsRPC(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	// Test eth_getLogs
	t.Run("EthGetLogs", func(t *testing.T) {
		fromBlock := "0x1"
		toBlock := "0x1"
		address := common.HexToAddress("0x1234567890123456789012345678901234567890")

		logs, err := operations.EthGetLogs(fromBlock, toBlock, address)
		require.NoError(t, err)
		require.NotNil(t, logs, "Logs should not be nil")
		log.Infof("EthGetLogs result type: %T", logs)
	})
}

// TestTxPoolRPC tests transaction pool related RPC methods
func TestTxPoolRPC(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	_, _ = setupTestEnvironment(t)

	// Test txpool_content - This might return a large object, so only log type
	t.Run("TxPoolContent", func(t *testing.T) {
		content, err := operations.TxPoolContent()
		require.NoError(t, err)
		log.Infof("TxPoolContent result type: %T", content)
	})

	// Test txpool_status
	t.Run("TxPoolStatus", func(t *testing.T) {
		status, err := operations.TxPoolStatus()
		require.NoError(t, err)
		log.Infof("TxPoolStatus result type: %T", status)
	})

	// Test txpool_limbo
	t.Run("TxPoolLimbo", func(t *testing.T) {
		limbo, err := operations.TxPoolLimbo()
		require.NoError(t, err)
		require.NotNil(t, limbo, "Limbo transactions should not be nil")
		log.Infof("TxPoolLimbo result type: %T", limbo)
	})
}

// TestZKEVMRPC tests zkevm-specific RPC methods
func TestZKEVMRPC(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	blockHash, blockNumber := setupTestEnvironment(t)

	// Test zkevm_getExitRootTable - already covered in debug tests but including here for completeness
	t.Run("ZKEVMGetExitRootTable", func(t *testing.T) {
		rootTable, err := operations.ZKEVMGetExitRootTable()
		require.NoError(t, err)
		require.NotNil(t, rootTable, "Exit root table should not be nil")
		log.Infof("ZKEVMGetExitRootTable result type: %T", rootTable)
	})

	// Test zkevm_batchNumber
	t.Run("ZKEVMBatchNumber", func(t *testing.T) {
		batchNum, err := operations.ZKEVMBatchNumber()
		require.NoError(t, err)
		require.Greater(t, batchNum, uint64(0), "Batch number should be greater than 0")
		log.Infof("ZKEVMBatchNumber result: %d", batchNum)
	})

	// Test zkevm_getLatestDataStreamBlock
	t.Run("ZKEVMGetLatestDataStreamBlock", func(t *testing.T) {
		dataStreamBlock, err := operations.ZKEVMGetLatestDataStreamBlock()
		require.NoError(t, err)
		require.NotNil(t, dataStreamBlock, "Data stream block should not be nil")
		log.Infof("ZKEVMGetLatestDataStreamBlock result type: %T", dataStreamBlock)
	})

	// Test zkevm_estimateCounters
	t.Run("ZKEVMEstimateCounters", func(t *testing.T) {
		t.Skip("Skipping test due to method handler crash")
	})

	// Test sync_getOffChainData
	t.Run("SyncGetOffChainData", func(t *testing.T) {
		t.Skip("Skipping test due to method not available")
	})

	// Test zkevm_batchNumberByBlockNumber
	t.Run("ZKEVMBatchNumberByBlockNumber", func(t *testing.T) {
		batchNum, err := operations.ZKEVMBatchNumberByBlockNumber("0x1") // Block #1
		require.NoError(t, err)
		require.Greater(t, batchNum, uint64(0), "Batch number should be greater than 0")
		log.Infof("ZKEVMBatchNumberByBlockNumber result: %d", batchNum)
	})

	// Test zkevm_getBatchByNumber
	t.Run("ZKEVMGetBatchByNumber", func(t *testing.T) {
		batch, err := operations.ZKEVMGetBatchByNumber(1) // Batch #1
		require.NoError(t, err)
		require.NotNil(t, batch, "Batch should not be nil")
		log.Infof("ZKEVMGetBatchByNumber result type: %T", batch)
	})

	// Test zkevm_getFullBlockByHash
	t.Run("ZKEVMGetFullBlockByHash", func(t *testing.T) {
		block, err := operations.ZKEVMGetFullBlockByHash(blockHash, true)
		require.NoError(t, err)
		require.NotNil(t, block, "Full block should not be nil")
		log.Infof("ZKEVMGetFullBlockByHash result type: %T", block)
	})

	// Test zkevm_getFullBlockByNumber
	t.Run("ZKEVMGetFullBlockByNumber", func(t *testing.T) {
		block, err := operations.ZKEVMGetFullBlockByNumber(blockNumber, true)
		require.NoError(t, err)
		require.NotNil(t, block, "Full block should not be nil")
		log.Infof("ZKEVMGetFullBlockByNumber result type: %T", block)
	})
}

func TestFixedNonceTooLowTransactions(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	ctx := context.Background()
	client, err := ethclient.Dial(operations.DefaultL2NetworkURL)
	require.NoError(t, err)

	// Use a fixed sender address and private key
	sender := common.HexToAddress(operations.DefaultL2AdminAddress)
	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(operations.DefaultL2AdminPrivateKey, "0x"))
	require.NoError(t, err)
	signer := types.MakeSigner(operations.GetTestChainConfig(operations.DefaultL2ChainID), 1, 0)

	// Get the initial nonce
	baseNonce, err := client.PendingNonceAt(ctx, sender)
	require.NoError(t, err)
	log.Infof("Starting nonce: %d", baseNonce)

	// Fixed transaction parameters
	const (
		totalTxs       = 20 // Total number of transactions
		batchSize      = 5  // Number of transactions per batch
		nonceTooLowCnt = 5  // Fixed number of NonceTooLow transactions
	)

	// Generate transactions with a fixed pattern
	var txs []types.Transaction
	currentNonce := baseNonce
	lowNonceIndexes := map[int]bool{1: true, 4: true, 8: true, 12: true, 16: true} // Fixed low nonce transaction indices

	for i := 0; i < totalTxs; i++ {
		var nonce uint64
		if lowNonceIndexes[i] {
			// Choose a nonce that is explicitly lower than the current nonce for low nonce transactions
			nonce = baseNonce + uint64(i/4) // Ensure nonce is lower than the current valid value
		} else {
			// Normally increment nonce
			nonce = currentNonce
			currentNonce++
		}

		to := common.HexToAddress(operations.DefaultL2NewAcc1Address)
		value := uint256.NewInt(uint64(1000 + i)) // Fixed value for easy verification
		gas := uint64(21000)
		gasPrice := uint256.NewInt(1 * encoding.Gwei) // Fixed gas price

		tx := &types.LegacyTx{
			CommonTx: types.CommonTx{
				Nonce: nonce,
				To:    &to,
				Gas:   gas,
				Value: value,
				Data:  nil,
			},
			GasPrice: gasPrice,
		}

		signedTx, err := types.SignTx(tx, *signer, privateKey)
		require.NoError(t, err)
		txs = append(txs, signedTx)
		log.Infof("Generated tx %d: nonce=%d, value=%s", i, nonce, value.String())
	}

	// Send transactions and record results
	type txResult struct {
		tx     types.Transaction
		err    error
		sentAt time.Time
	}
	var results []txResult

	expectedNextNonce := baseNonce
	maxSuccessNonce := baseNonce - 1
	for i := 0; i < len(txs); i += batchSize {
		end := i + batchSize
		if end > len(txs) {
			end = len(txs)
		}
		batch := txs[i:end]

		// Requery nonce before sending
		currentNonce, err := client.PendingNonceAt(ctx, sender)
		if err == nil && currentNonce > expectedNextNonce {
			expectedNextNonce = currentNonce
		}
		log.Infof("Sending batch %d-%d, expected next nonce: %d", i, end-1, expectedNextNonce)

		for _, tx := range batch {
			err := client.SendTransaction(ctx, tx)
			results = append(results, txResult{
				tx:     tx,
				err:    err,
				sentAt: time.Now(),
			})
			if err != nil {
				log.Infof("Tx %s failed: %v", tx.Hash().Hex(), err)
			} else {
				log.Infof("Tx %s sent successfully", tx.Hash().Hex())
				if tx.GetNonce() > maxSuccessNonce {
					maxSuccessNonce = tx.GetNonce()
				}
				expectedNextNonce = tx.GetNonce() + 1
			}
			time.Sleep(500 * time.Millisecond)
		}
	}

	// Validate results
	nonceTooLowCount := 0
	successCount := 0
	for _, result := range results {
		if result.err != nil {
			errStr := result.err.Error()
			if strings.Contains(errStr, "nonce too low") || strings.Contains(errStr, "NONCE_TOO_LOW") {
				nonceTooLowCount++
				log.Infof("NonceTooLow detected: tx nonce=%d, expected next nonce=%d",
					result.tx.GetNonce(), expectedNextNonce)
				require.True(t, result.tx.GetNonce() < expectedNextNonce,
					"NonceTooLow error should occur when nonce %d < expected nonce %d",
					result.tx.GetNonce(), expectedNextNonce)
			} else if strings.Contains(errStr, "could not replace existing tx") {
				// Treat "could not replace existing tx" as a "nonce too low" scenario
				nonceTooLowCount++
				log.Infof("NonceTooLow (replacement) detected: tx nonce=%d, expected next nonce=%d",
					result.tx.GetNonce(), expectedNextNonce)
				require.True(t, result.tx.GetNonce() < expectedNextNonce,
					"NonceTooLow error should occur when nonce %d < expected nonce %d",
					result.tx.GetNonce(), expectedNextNonce)
			}
		} else {
			successCount++
			// Asynchronously verify transaction is mined
			go func(tx types.Transaction) {
				err := operations.WaitTxToBeMined(ctx, client, tx, operations.DefaultTimeoutTxToBeMined)
				if err == nil {
					log.Debugf("Transaction mined: %s", tx.Hash().Hex())
				} else {
					log.Warnf("Transaction %s failed to be mined: %v", tx.Hash().Hex(), err)
				}
			}(result.tx)
		}
	}

	// Assert fixed results
	log.Infof("Expected NonceTooLow: %d, Actual: %d, Successful: %d",
		nonceTooLowCnt, nonceTooLowCount, successCount)

	require.Equal(t, nonceTooLowCnt, nonceTooLowCount, "NonceTooLow count does not match expectation")
	require.Equal(t, totalTxs-nonceTooLowCnt, successCount, "Number of successful transactions does not match expectation")

	// Wait for all transactions to be confirmed (optional)
	time.Sleep(5 * time.Second)

	// Validate the final nonce
	finalNonce, err := client.PendingNonceAt(ctx, sender)
	require.NoError(t, err)
	expectedFinalNonce := baseNonce + uint64(totalTxs-nonceTooLowCnt)
	require.Equal(t, expectedFinalNonce, finalNonce,
		"Final nonce is incorrect, expected: %d, actual: %d", expectedFinalNonce, finalNonce)
}

// TestVerification tests the block verification functionality
func TestVerification(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	// Set default verification delay batch to 2
	const defaultVerificationDelayBatch = 2

	// Helper function to get highest block number in a batch
	getHighestBlockInBatch := func(batchNumber uint64) (uint64, error) {
		batch, err := operations.GetBatchByNumber(new(big.Int).SetUint64(batchNumber))
		if err != nil {
			return 0, err
		}

		// Get blocks from batch
		blocks := batch.Blocks
		if len(blocks) == 0 {
			return 0, fmt.Errorf("no blocks found in batch %d", batchNumber)
		}

		// Find the highest block number
		var highestBlock uint64
		for _, blockHash := range blocks {
			blockHashStr, ok := blockHash.(string)
			if !ok {
				continue
			}

			block, err := operations.GetBlockByHash(common.HexToHash(blockHashStr))
			if err != nil {
				continue
			}

			blockNumber := uint64(block.Number)
			if blockNumber > highestBlock {
				highestBlock = blockNumber
			}
		}

		return highestBlock, nil
	}

	// Helper function to get finalized and safe block numbers
	getFinalizedAndSafeBlocks := func() (uint64, uint64, error) {
		// Get finalized block
		finalizedBlock, err := operations.GetBlockByNumber(big.NewInt(int64(rpc.FinalizedBlockNumber)))
		if err != nil {
			return 0, 0, err
		}
		finalizedNumber := uint64(finalizedBlock.Number)

		// Get safe block
		safeBlock, err := operations.GetBlockByNumber(big.NewInt(int64(rpc.SafeBlockNumber)))
		if err != nil {
			return 0, 0, err
		}
		safeNumber := uint64(safeBlock.Number)

		return finalizedNumber, safeNumber, nil
	}

	// Run the test 10 times
	prevBatchNumber := uint64(0)
	for i := 0; i < 10; i++ {
		log.Infof("Test iteration %d/10", i+1)

		// 1. Get current latest batch number
		latestBatchNumber, err := operations.GetBatchNumber()
		require.NoError(t, err)
		log.Infof("Latest batch number: %d", latestBatchNumber)

		// 2. Calculate target batch number (latest - default verification delay batch)
		// and also make sure that the batch number is increasing every time we check the verification
		if latestBatchNumber < defaultVerificationDelayBatch || latestBatchNumber == prevBatchNumber {
			log.Infof("Latest batch number (%d) is less than verification delay batch (%d) or equal to previous batch number (%d), skipping",
				latestBatchNumber, defaultVerificationDelayBatch, prevBatchNumber)
			time.Sleep(2 * time.Second)
			i-- // ignore this iteration
			continue
		}

		prevBatchNumber = latestBatchNumber

		targetBatchNumber := latestBatchNumber - defaultVerificationDelayBatch
		log.Infof("Target batch number: %d", targetBatchNumber)

		// 3. Get the highest block number in the target batch
		expectedVerificationBlock, err := getHighestBlockInBatch(targetBatchNumber)
		require.NoError(t, err)
		log.Infof("Expected verification block: %d", expectedVerificationBlock)

		// 4. Get finalized and safe block numbers
		finalizedBlockNumber, safeBlockNumber, err := getFinalizedAndSafeBlocks()
		require.NoError(t, err)
		log.Infof("Finalized block number: %d, Safe block number: %d", finalizedBlockNumber, safeBlockNumber)

		// 5. Wait for the finalized and safe block numbers to be >= expected verification block
		for i := 0; i < 30 && finalizedBlockNumber < expectedVerificationBlock && safeBlockNumber < expectedVerificationBlock; i++ {
			time.Sleep(1 * time.Second)
			finalizedBlockNumber, safeBlockNumber, err = getFinalizedAndSafeBlocks()
			require.NoError(t, err)
			log.Infof("Finalized block number: %d, Safe block number: %d", finalizedBlockNumber, safeBlockNumber)
		}

		// 6. Check that finalized and safe block numbers are >= highest block in target batch
		require.GreaterOrEqual(t, finalizedBlockNumber, expectedVerificationBlock,
			"Finalized block number should be >= highest block in target batch")
		require.GreaterOrEqual(t, safeBlockNumber, expectedVerificationBlock,
			"Safe block number should be >= highest block in target batch")

		log.Infof("Verification check passed for iteration %d", i+1)

		// 7. Sleep 2 seconds before next iteration
		time.Sleep(2 * time.Second)
	}

	log.Info("Verification delay batch test completed successfully")
}

func TestGetBlockGasLimit(t *testing.T) {
	log.Infof("Start TestGetBlockGasLimit")
	gaslimit, err := operations.GetBlockGasLimit()
	require.NoError(t, err)
	require.Equal(t, uint64(30000000), gaslimit)
	require.NoError(t, err)
}

// TestHighGasEstimation tests gas estimation for high gas consumption transactions
func TestHighGasEstimation(t *testing.T) {
	log.Infof("Start TestHighGasEstimation")
	client, err := ethclient.Dial(operations.DefaultL2NetworkURL)
	require.NoError(t, err)
	defer client.Close()

	// Test 1: Contract deployment (high gas consumption)
	t.Run("ContractDeployment", func(t *testing.T) {
		from := common.HexToAddress(operations.DefaultL2AdminAddress)

		// Simple ERC20-like contract bytecode (this is a complex contract that consumes significant gas)
		bytecode := "0x608060405234801561001057600080fd5b506040516105643803806105648339810160408190526100309190610054565b600055610084565b6000602082840312156100655760ff5b5051919050565b6104d1806100936000396000f3fe608060405234801561001057600080fd5b50600436106100575760003560e01c806306fdde031461005c578063095ea7b31461007a57806318160ddd1461009d57806323b872dd146100af578063313ce567146100c2575b600080fd5b6100646100d7565b6040516100719190610250565b60405180910390f35b61008d610088366004610334565b610169565b604051901515815260200161005e565b6100a56100a7565b005b61008d6100bd366004610334565b6101d3565b6100ca6101d5565b60405160ff909116815260200161005e565b60606040518060400160405280600781526020017f54657374204552430000000000000000000000000000000000000000000000008152509050919050565b6000813f7c010000000000000000000000000000000000000000000000000000000000000081141561013e5760019150506101cd565b816001600160a01b03163f7c010000000000000000000000000000000000000000000000000000000000000081141561017a5760019150506101cd565b60405162461bcd60e51b815260206004820152601060248201527f496e76616c696420616464726573730000000000000000000000000000000000604482015260640160405180910390fd5b92915050565b505050565b6012919050565b600060208083528351808285015260005b8181101561020c578581018301518582016040015282016101f0565b8181111561021e576000604083870101525b50601f01601f1916929092016040019392505050565b80356001600160a01b038116811461024b57600080fd5b919050565b6000806040838503121561026357600080fd5b61026c83610234565b946020939093013593505050565b60006020828403121561028c57600080fd5b61029582610234565b9392505050565b6000806000606084860312156102b157600080fd5b6102ba84610234565b92506102c860208501610234565b9150604084013590509250925092565b600181811c908216806102ec57607f821691505b6020821081141561030d57634e487b7160e01b600052602260045260246000fd5b5091905056fea2646970667358221220f7e7b4c8c6d8d5a8a2b1c9e8f7a6b5c4d3e2f1a9b8c7d6e5f4a3b2c1d0e9f8a722"

		// Estimate gas for contract deployment
		estimatedGas, err := operations.EthEstimateGas(
			from,
			common.Address{}, // to address is empty for contract creation
			"0x0",            // gas (will be estimated)
			"0x3B9ACA00",     // gasPrice (1 Gwei)
			"0x0",            // value
			bytecode,         // data (contract bytecode)
		)
		require.NoError(t, err)
		log.Infof("Contract deployment estimated gas: %d", estimatedGas)

		// Expect high gas consumption for contract deployment (typically > 200,000)
		require.Greater(t, estimatedGas, uint64(21000), "Contract deployment should consume significant gas")
	})

	// Test 2: Transaction with large data payload
	t.Run("LargeDataTransaction", func(t *testing.T) {
		from := common.HexToAddress(operations.DefaultL2AdminAddress)
		to := common.HexToAddress(operations.DefaultL2NewAcc1Address)

		// Create a large data payload (4KB of data)
		largeData := "0x"
		for i := 0; i < 4096; i++ {
			largeData += "00"
		}

		// Estimate gas for transaction with large data
		estimatedGas, err := operations.EthEstimateGas(
			from,
			to,
			"0x0",        // gas (will be estimated)
			"0x3B9ACA00", // gasPrice (1 Gwei)
			"0x0",        // value
			largeData,    // large data payload
		)
		require.NoError(t, err)
		log.Infof("Large data transaction estimated gas: %d", estimatedGas)

		// Expect higher gas consumption due to data costs (21000 base + data costs)
		require.Greater(t, estimatedGas, uint64(21000), "Large data transaction should consume more than base gas")
		// Data cost is 4 gas per zero byte and 16 gas per non-zero byte
		// For 4KB of zero bytes: 21000 + (4096 * 4) = 37,384
		require.Greater(t, estimatedGas, uint64(35000), "Should account for data costs")
	})

	// Test 3: Multiple sequential operations to test gas limit constraints
	t.Run("GasLimitConstraints", func(t *testing.T) {
		// Get current block gas limit
		blockGasLimit, err := operations.GetBlockGasLimit()
		require.NoError(t, err)
		log.Infof("Current block gas limit: %d", blockGasLimit)

		from := common.HexToAddress(operations.DefaultL2AdminAddress)
		to := common.HexToAddress(operations.DefaultL2NewAcc1Address)

		// Try to estimate gas for a transaction that would exceed block limit
		// (This should either return an error or cap at a reasonable value)
		excessiveGasHex := fmt.Sprintf("0x%x", blockGasLimit+1000000) // Try to use more than block limit

		_, err = operations.EthEstimateGas(
			from,
			to,
			excessiveGasHex, // try to set gas higher than block limit
			"0x3B9ACA00",    // gasPrice (1 Gwei)
			"0x0",           // value
			"0x",            // empty data
		)

		// This might fail or succeed depending on implementation
		// The key is that we're testing the gas estimation behavior at boundaries
		if err != nil {
			log.Infof("Gas estimation correctly rejected excessive gas limit: %v", err)
		} else {
			log.Infof("Gas estimation handled excessive gas limit gracefully")
		}
	})

	// Test 4: Complex computation simulation (using precompile calls)
	t.Run("ComplexComputation", func(t *testing.T) {
		from := common.HexToAddress(operations.DefaultL2AdminAddress)
		// Use the SHA256 precompile address (0x02) to simulate complex computation
		sha256Precompile := common.HexToAddress("0x0000000000000000000000000000000000000002")

		// Create data for SHA256 computation (large input)
		computationData := "0x"
		for i := 0; i < 1000; i++ {
			computationData += fmt.Sprintf("%02x", i%256)
		}

		// Estimate gas for precompile call
		estimatedGas, err := operations.EthEstimateGas(
			from,
			sha256Precompile,
			"0x0",           // gas (will be estimated)
			"0x3B9ACA00",    // gasPrice (1 Gwei)
			"0x0",           // value
			computationData, // data for computation
		)
		require.NoError(t, err)
		log.Infof("Complex computation estimated gas: %d", estimatedGas)

		// SHA256 precompile has specific gas costs
		require.Greater(t, estimatedGas, uint64(21000), "Complex computation should consume more than base gas")
	})

	// Test 2: Transaction with large data payload
	t.Run("SuperLargeDataTransaction", func(t *testing.T) {
		from := common.HexToAddress(operations.DefaultL2AdminAddress)
		to := common.HexToAddress(operations.DefaultL2NewAcc1Address)

		const targetBytes = 7494751
		largeData := "0x" + strings.Repeat("00", targetBytes)

		// Estimate gas for transaction with large data
		_, err := operations.EthEstimateGas(
			from,
			to,
			"0x0",        // gas (will be estimated)
			"0x3B9ACA00", // gasPrice (1 Gwei)
			"0x0",        // value
			largeData,    // large data payload
		)
		require.Error(t, err)
		log.Infof("SuperLarge data transaction estimated gas exceed")
	})

	log.Infof("TestHighGasEstimation completed successfully")
}

func TestTransactionPreExecInnerTransaction(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	operations.EnsureContractsDeployed(t)

	contractAABI, err := abi.JSON(strings.NewReader(constants.ContractAABIJson))
	require.NoError(t, err)
	calldata, err := contractAABI.Pack("triggerCall")
	require.NoError(t, err)

	rpcClient, err := rpc.Dial(operations.DefaultL2NetworkURL, nil)
	require.NoError(t, err)
	defer rpcClient.Close()

	fromAddr := common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")
	txRequest := map[string]interface{}{
		"from": fromAddr.Hex(), "to": operations.ContractAAddr.Hex(), "gas": "0x30000",
		"gasPrice": "0x4a817c800", "value": "0x0", "nonce": "0x1",
		"data": fmt.Sprintf("0x%x", calldata),
	}
	stateOverride := map[string]interface{}{
		fromAddr.Hex(): map[string]interface{}{"balance": "0x1000000000000000000000"},
	}

	var result json.RawMessage
	err = rpcClient.Call(&result, "eth_transactionPreExec", []interface{}{txRequest}, "latest", stateOverride)
	require.NoError(t, err)

	var preExecResults []map[string]interface{}
	err = json.Unmarshal(result, &preExecResults)
	require.NoError(t, err)
	require.Len(t, preExecResults, 1)

	preExecResult := preExecResults[0]
	require.NotNil(t, preExecResult["logs"])
	require.NotNil(t, preExecResult["stateDiff"])
	require.NotNil(t, preExecResult["gasUsed"])
	require.NotNil(t, preExecResult["blockNumber"])

	innerTxList, ok := preExecResult["innerTxs"].([]interface{})
	require.True(t, ok)
	require.GreaterOrEqual(t, len(innerTxList), 1)

	firstInnerTx := innerTxList[0].(map[string]interface{})
	require.Equal(t, "call", firstInnerTx["call_type"])
	require.Equal(t, strings.ToLower(operations.ContractAAddr.Hex()), strings.ToLower(firstInnerTx["to"].(string)))
	require.Equal(t, "0xf18c388a", firstInnerTx["input"].(string))
	require.False(t, firstInnerTx["is_error"].(bool), "Expected is_error to be false for the first inner transaction")

	if len(innerTxList) >= 2 {
		secondInnerTx := innerTxList[1].(map[string]interface{})
		require.Equal(t, "call", secondInnerTx["call_type"])
		require.Equal(t, strings.ToLower(operations.ContractAAddr.Hex()), strings.ToLower(secondInnerTx["from"].(string)))
		require.Equal(t, strings.ToLower(operations.ContractBAddr.Hex()), strings.ToLower(secondInnerTx["to"].(string)))
		require.Equal(t, "0x32e43a11", secondInnerTx["input"].(string))

		name := secondInnerTx["name"].(string)
		require.True(t, name[len(name)-1] >= '0' && name[len(name)-1] <= '9')
		require.False(t, secondInnerTx["is_error"].(bool), "Expected is_error to be false for the second inner transaction")
	}

	transferTx := map[string]interface{}{
		"from": fromAddr.Hex(), "to": "0x742d35Cc4cF52f9234E96bC29d7F6a0c91d87b06",
		"value": "0x1000000000000000", "gas": "0x5208",
		"gasPrice": "0x4a817c800", "nonce": "0x2",
	}

	var transferResult json.RawMessage
	err = rpcClient.Call(&transferResult, "eth_transactionPreExec", []interface{}{transferTx}, "latest", nil)
	require.NoError(t, err)

	var transferResults []map[string]interface{}
	err = json.Unmarshal(transferResult, &transferResults)
	require.NoError(t, err)
	require.Len(t, transferResults, 1)

	transferInnerTxs, ok := transferResults[0]["innerTxs"].([]interface{})
	require.True(t, ok, "innerTxs should be an array for simple transfers")
	require.Empty(t, transferInnerTxs, "innerTxs should be empty array for simple transfers (dept == 0)")

	t.Logf("✅ Simple transfer validation: innerTxs count = %d (expected: 0)", len(transferInnerTxs))
}

// TestTransactionPreExecWithCreateOpcode tests the eth_transactionPreExec RPC method with CREATE opcode
// Uses pre-deployed factory contract to test calling a function that creates another contract using CREATE opcode
func TestTransactionPreExecWithCreateOpcode(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	operations.EnsureContractsDeployed(t)

	factoryABI, err := abi.JSON(strings.NewReader(constants.ContractFactoryABIJson))
	require.NoError(t, err)
	calldata, err := factoryABI.Pack("createSimpleStorage", big.NewInt(123))
	require.NoError(t, err)

	rpcClient, err := rpc.Dial(operations.DefaultL2NetworkURL, nil)
	require.NoError(t, err)
	defer rpcClient.Close()

	fromAddr := common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")
	txRequest := map[string]interface{}{
		"from": fromAddr.Hex(), "to": operations.FactoryAddr.Hex(), "gas": "0x100000",
		"gasPrice": "0x4a817c800", "value": "0x0", "nonce": "0x1",
		"data": fmt.Sprintf("0x%x", calldata),
	}
	stateOverride := map[string]interface{}{
		fromAddr.Hex(): map[string]interface{}{"balance": "0x1000000000000000000000"},
	}

	var result json.RawMessage
	err = rpcClient.Call(&result, "eth_transactionPreExec", []interface{}{txRequest}, "latest", stateOverride)
	require.NoError(t, err)

	var preExecResults []map[string]interface{}
	err = json.Unmarshal(result, &preExecResults)
	require.NoError(t, err)
	require.Len(t, preExecResults, 1)

	innerTxList, ok := preExecResults[0]["innerTxs"].([]interface{})
	require.True(t, ok)
	require.GreaterOrEqual(t, len(innerTxList), 1)

	foundCreate := false
	for _, innerTx := range innerTxList {
		innerTxMap := innerTx.(map[string]interface{})
		if callType := innerTxMap["call_type"]; callType == "create" || callType == "create2" {
			foundCreate = true
			require.Equal(t, strings.ToLower(operations.FactoryAddr.Hex()), strings.ToLower(innerTxMap["from"].(string)))
			if to := innerTxMap["to"]; to != nil {
				require.NotEmpty(t, to.(string))
				require.NotEqual(t, "0x0000000000000000000000000000000000000000", strings.ToLower(to.(string)))
			}
			if input := innerTxMap["input"]; input != nil {
				require.NotEmpty(t, input.(string))
				require.NotEqual(t, "0x", input.(string))
			}
			break
		}
	}
	require.True(t, foundCreate)
}

// TestTransactionPreExecNonSequentialNonces tests nonce validation with the updated strict nonce checking
func TestTransactionPreExecNonSequentialNonces(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	operations.EnsureContractsDeployed(t)

	contractBABI, err := abi.JSON(strings.NewReader(constants.ContractBABIJson))
	require.NoError(t, err)
	calldata, err := contractBABI.Pack("dummy")
	require.NoError(t, err)

	rpcClient, err := rpc.Dial(operations.DefaultL2NetworkURL, nil)
	require.NoError(t, err)
	defer rpcClient.Close()

	fromAddr := common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")

	txRequest1 := map[string]interface{}{
		"from": fromAddr.Hex(), "to": operations.ContractBAddr.Hex(), "gas": "0x30000",
		"gasPrice": "0x4a817c800", "value": "0x0", "nonce": "0x5",
		"data": fmt.Sprintf("0x%x", calldata),
	}
	txRequest2 := map[string]interface{}{
		"from": fromAddr.Hex(), "to": operations.ContractBAddr.Hex(), "gas": "0x30000",
		"gasPrice": "0x4a817c800", "value": "0x0", "nonce": "0x3",
		"data": fmt.Sprintf("0x%x", calldata),
	}
	stateOverride := map[string]interface{}{
		fromAddr.Hex(): map[string]interface{}{"balance": "0x1000000000000000000000"},
	}

	var result json.RawMessage
	err = rpcClient.Call(&result, "eth_transactionPreExec", []interface{}{txRequest1, txRequest2}, "latest", stateOverride)
	require.NoError(t, err)

	var preExecResults []map[string]interface{}
	err = json.Unmarshal(result, &preExecResults)
	require.NoError(t, err)
	require.Len(t, preExecResults, 2)

	// Second transaction should also fail due to wrong nonce
	secondResult := preExecResults[1]
	require.NotNil(t, secondResult["error"])
	errorMap2 := secondResult["error"].(map[string]interface{})
	errorCode2 := int(errorMap2["code"].(float64))
	require.Equal(t, 1003, errorCode2)
	require.Contains(t, errorMap2["msg"].(string), fromAddr.Hex())
}

// TestTransactionPreExecGasValidation compares gasUsed with eth_estimateGas
func TestTransactionPreExecGasValidation(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	operations.EnsureContractsDeployed(t)

	contractAABI, err := abi.JSON(strings.NewReader(constants.ContractAABIJson))
	require.NoError(t, err)
	calldata, err := contractAABI.Pack("triggerCall")
	require.NoError(t, err)

	// Create both RPC client and eth client for comparison
	rpcClient, err := rpc.Dial(operations.DefaultL2NetworkURL, nil)
	require.NoError(t, err)
	defer rpcClient.Close()

	ethClient, err := ethclient.Dial(operations.DefaultL2NetworkURL)
	require.NoError(t, err)
	defer ethClient.Close()

	fromAddr := common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")

	ctx := context.Background()
	fundingAmount := uint256.NewInt(5000000000000000000)
	fundingTxHash := operations.TransToken(t, ctx, ethClient, fundingAmount, fromAddr.String())
	t.Logf("✅ Funded test address %s with 5 ETH, tx: %s", fromAddr.Hex(), fundingTxHash)

	balance, err := ethClient.BalanceAt(ctx, fromAddr, nil)
	require.NoError(t, err)
	balanceETH := new(big.Float).Quo(new(big.Float).SetInt(balance.ToBig()), new(big.Float).SetFloat64(1e18))
	t.Logf("✅ Test address balance after funding: %s ETH", balanceETH.String())
	t.Logf("🎯 Both eth_transactionPreExec and eth_estimateGas will now use the same funded address")

	// Test Case 1: Simple Contract Call
	txRequest := map[string]interface{}{
		"from": fromAddr.Hex(), "to": operations.ContractAAddr.Hex(), "gas": "0x100000",
		"gasPrice": "0x4a817c800", "value": "0x0", "nonce": "0x1",
		"data": fmt.Sprintf("0x%x", calldata),
	}
	// Get gasUsed from eth_transactionPreExec
	var preExecResult json.RawMessage
	err = rpcClient.Call(&preExecResult, "eth_transactionPreExec", []interface{}{txRequest}, "latest", nil)
	require.NoError(t, err)

	var preExecResults []map[string]interface{}
	err = json.Unmarshal(preExecResult, &preExecResults)
	require.NoError(t, err)
	require.Len(t, preExecResults, 1)

	preExecGasUsed := preExecResults[0]["gasUsed"].(float64)

	// Get gas estimate from eth_estimateGas
	estimateGasRequest := map[string]interface{}{
		"from": fromAddr.Hex(), "to": operations.ContractAAddr.Hex(),
		"data": fmt.Sprintf("0x%x", calldata),
	}

	var estimateResult string
	err = rpcClient.Call(&estimateResult, "eth_estimateGas", estimateGasRequest, "latest")
	require.NoError(t, err)

	estimatedGas, err := strconv.ParseUint(strings.TrimPrefix(estimateResult, "0x"), 16, 64)
	require.NoError(t, err)

	// Validation: Both should be very close
	gasUsedUint64 := uint64(preExecGasUsed)
	tolerance := uint64(5000) // Allow 5K gas difference for binary search precision

	require.Greater(t, gasUsedUint64, uint64(21000), "Gas should be > 21000 for contract call")
	require.Greater(t, estimatedGas, uint64(21000), "Estimated gas should be > 21000 for contract call")

	diff := uint64(0)
	if estimatedGas > gasUsedUint64 {
		diff = estimatedGas - gasUsedUint64
	} else {
		diff = gasUsedUint64 - estimatedGas
	}

	require.LessOrEqual(t, diff, tolerance,
		"Gas difference too large: preExec=%d, estimate=%d, diff=%d",
		gasUsedUint64, estimatedGas, diff)

	t.Logf("✅ Contract Call Gas: preExec=%d, estimate=%d, diff=%d (within tolerance)",
		gasUsedUint64, estimatedGas, diff)

	// Test Case 2: Simple Transfer (should use exactly 21000 gas)
	transferTx := map[string]interface{}{
		"from": fromAddr.Hex(), "to": "0x742d35Cc4cF52f9234E96bC29d7F6a0c91d87b06",
		"value": "0x1000000000000000", "gas": "0x5208", // 21000 in hex
		"gasPrice": "0x4a817c800", "nonce": "0x2",
	}

	// PreExec gas usage
	err = rpcClient.Call(&preExecResult, "eth_transactionPreExec", []interface{}{transferTx}, "latest", nil)
	require.NoError(t, err)
	err = json.Unmarshal(preExecResult, &preExecResults)
	require.NoError(t, err)
	transferPreExecGas := uint64(preExecResults[0]["gasUsed"].(float64))

	// Estimate gas usage
	transferEstimate := map[string]interface{}{
		"from": fromAddr.Hex(), "to": "0x742d35Cc4cF52f9234E96bC29d7F6a0c91d87b06",
		"value": "0x1000000000000000",
	}
	err = rpcClient.Call(&estimateResult, "eth_estimateGas", transferEstimate, "latest")
	require.NoError(t, err)
	transferEstimatedGas, err := strconv.ParseUint(strings.TrimPrefix(estimateResult, "0x"), 16, 64)
	require.NoError(t, err)

	// Simple transfers should be exactly 21000 gas
	require.Equal(t, uint64(21000), transferPreExecGas, "Simple transfer should use exactly 21000 gas")
	require.Equal(t, uint64(21000), transferEstimatedGas, "Simple transfer estimate should be exactly 21000 gas")

	t.Logf("✅ Transfer Gas: preExec=%d, estimate=%d (both exactly 21000)",
		transferPreExecGas, transferEstimatedGas)

	// Test Case 3: CREATE operation
	factoryABI, err := abi.JSON(strings.NewReader(constants.ContractFactoryABIJson))
	require.NoError(t, err)
	createCalldata, err := factoryABI.Pack("createSimpleStorage", big.NewInt(999))
	require.NoError(t, err)

	createTx := map[string]interface{}{
		"from": fromAddr.Hex(), "to": operations.FactoryAddr.Hex(), "gas": "0x200000",
		"gasPrice": "0x4a817c800", "value": "0x0", "nonce": "0x3",
		"data": fmt.Sprintf("0x%x", createCalldata),
	}

	// PreExec gas usage for CREATE
	err = rpcClient.Call(&preExecResult, "eth_transactionPreExec", []interface{}{createTx}, "latest", nil)
	require.NoError(t, err)
	err = json.Unmarshal(preExecResult, &preExecResults)
	require.NoError(t, err)
	createPreExecGas := uint64(preExecResults[0]["gasUsed"].(float64))

	// Estimate gas for CREATE
	createEstimate := map[string]interface{}{
		"from": fromAddr.Hex(), "to": operations.FactoryAddr.Hex(),
		"data": fmt.Sprintf("0x%x", createCalldata),
	}
	err = rpcClient.Call(&estimateResult, "eth_estimateGas", createEstimate, "latest")
	require.NoError(t, err)
	createEstimatedGas, err := strconv.ParseUint(strings.TrimPrefix(estimateResult, "0x"), 16, 64)
	require.NoError(t, err)

	createDiff := uint64(0)
	if createEstimatedGas > createPreExecGas {
		createDiff = createEstimatedGas - createPreExecGas
	} else {
		createDiff = createPreExecGas - createEstimatedGas
	}

	require.LessOrEqual(t, createDiff, uint64(50000),
		"CREATE gas difference too large: preExec=%d, estimate=%d, diff=%d",
		createPreExecGas, createEstimatedGas, createDiff)

	t.Logf("✅ CREATE Gas: preExec=%d, estimate=%d, diff=%d",
		createPreExecGas, createEstimatedGas, createDiff)
}
