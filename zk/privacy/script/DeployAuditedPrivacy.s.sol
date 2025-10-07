// SPDX-License-Identifier: UNLICENSED
pragma solidity 0.8.27;

interface Vm {
    function startBroadcast(uint256 privateKey) external;
    function stopBroadcast() external;
    function envUint(string calldata key) external returns (uint256);
    function envString(string calldata key) external returns (string memory);
}

Vm constant vm = Vm(address(uint160(uint256(keccak256("hevm cheat code")))));

import {Iden3State} from "../contracts/Iden3StateWrapper.sol";
import {MockStateTransitionVerifier} from "../contracts/mocks/MockStateTransitionVerifier.sol";
import {MockCrossChainProofValidator} from "../contracts/mocks/MockCrossChainProofValidator.sol";
import {MockGroth16Verifier} from "../contracts/mocks/MockGroth16Verifier.sol";
import {CredentialAtomicQueryMTPV2Validator} from "../contracts/validators/CredentialAtomicQueryMTPV2ValidatorWrapper.sol";
import {ERC20Verifier} from "../contracts/examples/ERC20Verifier.sol";
import {IState} from "@iden3/contracts/interfaces/IState.sol";
import {ERC1967Proxy} from "@openzeppelin/contracts/proxy/ERC1967/ERC1967Proxy.sol";

contract DeployAuditedPrivacy {
    function run() external {
        uint256 pk = vm.envUint("PK");
        vm.startBroadcast(pk);

        // Deploy mocks for state initialization
        MockStateTransitionVerifier stVerifier = new MockStateTransitionVerifier();
        MockCrossChainProofValidator ccValidator = new MockCrossChainProofValidator();

        // default id type for Ethereum-based IDs (0x0e00)
        bytes2 defaultIdType = bytes2(uint16(0x0e00));

        // Deploy Iden3 State implementation (upgradeable) and initialize via ERC1967 proxy
        Iden3State stateImpl = new Iden3State();
        // Build initializer calldata directly by signature to avoid selector lookup issues
        bytes memory initCalldata = abi.encodeWithSignature(
            "initialize(address,bytes2,address,address)",
            address(stVerifier),
            defaultIdType,
            msg.sender,
            address(ccValidator)
        );
        ERC1967Proxy proxy = new ERC1967Proxy(address(stateImpl), initCalldata);
        IState state = IState(address(proxy));

        // Deploy a mock groth16 verifier for MTP circuit
        MockGroth16Verifier groth16 = new MockGroth16Verifier();

        // Deploy MTP v2 validator and initialize
        CredentialAtomicQueryMTPV2Validator mtp = new CredentialAtomicQueryMTPV2Validator();
        mtp.initialize(address(state), address(groth16), msg.sender);

        // Deploy ERC20 verifier example and initialize
        ERC20Verifier erc20v = new ERC20Verifier();
        erc20v.initialize("DevZK", "DVZ", state);

        vm.stopBroadcast();
    }
}
