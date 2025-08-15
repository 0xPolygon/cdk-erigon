// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts-upgradeable/access/OwnableUpgradeable.sol";
import "@openzeppelin/contracts-upgradeable/utils/PausableUpgradeable.sol";
import "@openzeppelin/contracts-upgradeable/utils/ReentrancyGuardUpgradeable.sol";
import "@openzeppelin/contracts-upgradeable/proxy/utils/Initializable.sol";

/**
 * @title TokenManagerV1
 * @dev Enhanced Token Manager with address-based access control
 * Features:
 * - Simple address-based permissions (Admin, Operator)
 * - Single address per role (strictly enforced)
 * - Reentrancy protection
 * - Gas optimized operations
 * - Anti front-running protection for logic contract
 */
contract TokenManagerV1 is 
    Initializable, 
    OwnableUpgradeable, 
    PausableUpgradeable, 
    ReentrancyGuardUpgradeable 
{
    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    // ==================== CONSTANTS ====================
    
    address constant PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001001;
    
    bytes1 constant TEST_OP = 0x01;
    bytes1 constant BRIDGE_OP = 0x02;
    bytes1 constant CLEAN_OP = 0x03;
    
    // ==================== STATE VARIABLES ====================
    
    uint256 public activationBlock;
    address public admin;     // Single admin address
    address public operator;  // Single operator address
    
    // ==================== EVENTS ====================
    
    // System Events
    event Initialized(address indexed owner, address indexed admin, uint256 activationBlock);
    event ActivationBlockSet(uint256 activationBlock);
    event AdminChanged(address indexed oldAdmin, address indexed newAdmin);
    event OperatorChanged(address indexed oldOperator, address indexed newOperator);
    
    // Token Operation Events
    event TokenBridged(address indexed operator, uint256 amount);
    event TargetAddressCleaned(address indexed operator);
    
    // ==================== MODIFIERS ====================
    
    /**
     * @dev Modifier to check if Token Manager is active
     */
    modifier onlyActive() {
        require(isActive(), "Token Manager is not active");
        _;
    }
    
    /**
     * @dev Modifier to check if precompile is available
     */
    modifier onlyWithPrecompile() {
        require(isPrecompileAvailable(), "Precompile is not available");
        _;
    }
    
    /**
     * @dev Modifier to restrict access to admin only
     */
    modifier onlyAdmin() {
        require(msg.sender == admin, "Only admin can call this function");
        _;
    }
    
    /**
     * @dev Modifier to restrict access to operator only
     */
    modifier onlyOperator() {
        require(msg.sender == operator, "Only operator can call this function");
        _;
    }
    
    // ==================== INITIALIZATION ====================

    /**
     * @dev Initialize the contract with Owner and Admin
     * @param _owner Initial owner (system-level permissions)
     * @param _admin Initial admin (business-level permissions)
     */
    function initialize(address _owner, address _admin) external initializer {
        require(_admin != address(0), "Admin cannot be zero address");
        
        __Ownable_init(_owner);
        __Pausable_init();
        __ReentrancyGuard_init();
        
        admin = _admin;
        activationBlock = type(uint256).max; // Not active by default
        
        emit Initialized(_owner, _admin, activationBlock);
    }

    // ==================== PRECOMPILE FUNCTIONS ====================
    
    /**
     * @dev Check if precompile is available (internal use only)
     * @return bool True if precompile is available, false otherwise
     */
    function isPrecompileAvailable() internal view returns (bool) {
        bytes memory testData = abi.encodePacked(TEST_OP);
        (bool success, bytes memory returnData) = PRECOMPILE_ADDRESS.staticcall(testData);
        return success && returnData.length == 2 && 
               returnData[0] == 0x4F && returnData[1] == 0x4B; // "OK" in hex
    }

    // ==================== SYSTEM CONTROL ====================
    
    /**
     * @dev Set activation block
     * @param _activationBlock Block number when Token Manager becomes active
     */
    function setActivationBlock(uint256 _activationBlock) external onlyOwner {
        activationBlock = _activationBlock;
        emit ActivationBlockSet(_activationBlock);
    }
    
    /**
     * @dev Check if Token Manager is active
     * @return bool True if active, false otherwise
     */
    function isActive() public view returns (bool) {
        return block.number >= activationBlock;
    }
    
    /**
     * @dev Pause all token operations
     */
    function pause() external onlyOwner {
        _pause();
    }
    
    /**
     * @dev Unpause all token operations
     */
    function unpause() external onlyOwner {
        _unpause();
    }

    // ==================== ADMIN MANAGEMENT ====================
    
    /**
     * @dev Set new admin address
     * @param newAdmin New admin address
     */
    function setAdmin(address newAdmin) external onlyAdmin {
        require(newAdmin != address(0), "Admin cannot be zero address");
        require(newAdmin != admin, "Address is already the current admin");
        
        address oldAdmin = admin;
        admin = newAdmin;
        
        emit AdminChanged(oldAdmin, newAdmin);
    }

    // ==================== OPERATOR MANAGEMENT ====================
    
    /**
     * @dev Set operator address (can be zero address to remove operator)
     * @param newOperator New operator address (use address(0) to remove)
     */
    function setOperator(address newOperator) external onlyAdmin {
        require(newOperator != operator, "Address is already the current operator");
        
        address oldOperator = operator;
        operator = newOperator;
        
        emit OperatorChanged(oldOperator, newOperator);
    }

    // ==================== TOKEN OPERATIONS ====================
    
    /**
     * @dev Bridge tokens from L1 to operator's address
     * @param amount Amount of tokens to bridge
     */
    function bridgeFrom(uint256 amount) 
        external 
        onlyOperator 
        onlyActive 
        whenNotPaused 
        onlyWithPrecompile 
        nonReentrant
    {
        require(amount > 0, "Amount must be greater than zero");
        
        address operatorAddress = msg.sender;
        
        // Prepare precompile call data: [operation:1][address:32][amount:32]
        bytes memory callData = abi.encodePacked(
            BRIDGE_OP,
            bytes32(uint256(uint160(operatorAddress))),
            bytes32(amount)
        );
        
        // Call precompile
        (bool success, ) = PRECOMPILE_ADDRESS.call(callData);
        require(success, "Precompile bridge call failed");
        
        emit TokenBridged(operatorAddress, amount);
    }
    
    /**
     * @dev Clean up tokens from precompile's target address
     */
    function cleanup() 
        external 
        onlyOperator 
        onlyActive 
        whenNotPaused 
        onlyWithPrecompile 
        nonReentrant
    {
        // Prepare precompile call data: [operation:1]
        bytes memory callData = abi.encodePacked(CLEAN_OP);
        
        // Call precompile
        (bool success, ) = PRECOMPILE_ADDRESS.call(callData);
        require(success, "Precompile cleanup call failed");
        
        emit TargetAddressCleaned(msg.sender);
    }

    // ==================== SECURITY OVERRIDES ====================
    
    /**
     * @dev Override renounceOwnership to prevent accidental loss of admin control
     */
    function renounceOwnership() public virtual override onlyOwner {
        revert("TokenManager: renounceOwnership is disabled for security");
    }

    /**
     * @dev Override transferOwnership with validation
     * @param newOwner Address of new owner
     */
    function transferOwnership(address newOwner) public virtual override onlyOwner {
        require(newOwner != address(0), "Cannot transfer ownership to zero address");
        require(newOwner != _msgSender(), "Cannot transfer ownership to self");
        
        _transferOwnership(newOwner);
    }

    // ==================== VERSION ====================

    /**
     * @dev Get contract version
     * @return string Contract version
     */
    function VERSION() external pure returns (string memory) {
        return "1.0.0";
    }
}