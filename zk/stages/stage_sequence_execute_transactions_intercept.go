package stages

import (
	// "encoding/binary"  // Commented out - only needed for unused fields
	"errors"
	"fmt"
	"math/big"

	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/crypto"
	"github.com/ledgerwatch/erigon/eth/ethconfig"
	"github.com/ledgerwatch/log/v3"
)

// Bridge interception configuration
var (
	// BridgeEvent event signature hash
	// Calculated from: keccak256("BridgeEvent(uint8,uint32,address,uint32,address,uint256,bytes,uint32)")
	BRIDGE_EVENT_SIGNATURE = crypto.Keccak256Hash([]byte("BridgeEvent(uint8,uint32,address,uint32,address,uint256,bytes,uint32)"))
)

// ==================== Data Structures ====================

// BridgeEventData represents parsed BridgeEvent data
type BridgeEventData struct {
	LeafType           uint8
	OriginNetwork      uint32
	OriginAddress      common.Address
	DestinationNetwork uint32
	DestinationAddress common.Address
	Amount             *big.Int
	Metadata           []byte
	DepositCount       uint32
}

// ==================== Core Functions ====================

// interceptBridgeTransactionIfNeeded checks and intercepts Bridge transaction if needed
func interceptBridgeTransactionIfNeeded(receipt *types.Receipt, transaction types.Transaction, config *ethconfig.BridgeInterceptConfig) error {
	if receipt == nil || len(receipt.Logs) == 0 {
		return nil // No receipt or logs, pass through
	}

	txSender, _ := transaction.GetSender()
	if err := checkBridgeEventInReceipt(receipt, txSender, config); err != nil {
		log.Warn("Bridge transaction intercepted",
			"txHash", transaction.Hash(),
			"error", err,
			"sender", txSender.Hex())
		return err
	}

	return nil
}

// ==================== Helper Functions ====================

// checkBridgeEventInReceipt checks BridgeEvent in receipt and validates
// Precondition: receipt != nil && len(receipt.Logs) > 0
func checkBridgeEventInReceipt(receipt *types.Receipt, txSender common.Address, config *ethconfig.BridgeInterceptConfig) error {
	bridgeContractAddress := common.HexToAddress(config.BridgeContractAddress)
	targetTokenAddress := common.HexToAddress(config.TargetTokenAddress)

	for _, logEntry := range receipt.Logs {
		// Check if it's a log from Bridge contract
		if logEntry.Address != bridgeContractAddress {
			continue
		}

		// Parse BridgeEvent (includes signature validation)
		event, err := parseBridgeEvent(logEntry.Data, logEntry.Topics)
		if err != nil {
			// Not a bridge event or parse error, skip with debug log
			log.Debug("Failed to parse bridge event, skipping",
				"contractAddress", logEntry.Address.Hex(),
				"error", err)
			continue
		}

		// Validate if should intercept
		if err := validateBridgeEvent(event, txSender, config, targetTokenAddress); err != nil {
			return fmt.Errorf("bridge event validation failed: %w", err)
		}

		// Reduce MaxBridgeAmount after successful validation
		if event.OriginAddress == targetTokenAddress {
			config.MaxBridgeAmount.Sub(config.MaxBridgeAmount, event.Amount)
			log.Debug("Bridge amount reduced",
				"originAddress", event.OriginAddress.Hex(),
				"amount", event.Amount.String(),
				"remainingAmount", config.MaxBridgeAmount.String(),
				"sender", txSender.Hex())
		}

		log.Debug("Bridge event validated",
			"originAddress", event.OriginAddress.Hex(),
			"amount", event.Amount.String(),
			"sender", txSender.Hex())
	}

	return nil
}

// parseBridgeEvent parses BridgeEvent data from log
func parseBridgeEvent(logData []byte, topics []common.Hash) (*BridgeEventData, error) {
	if len(topics) == 0 || topics[0] != BRIDGE_EVENT_SIGNATURE {
		return nil, errors.New("not a bridge event")
	}
	if len(logData) < 256 {
		return nil, errors.New("insufficient log data length")
	}

	event := &BridgeEventData{}

	// Parse only the fields we actually use for interception
	// Parse fixed parameters according to Solidity ABI encoding rules
	// Each parameter occupies 32 bytes, with smaller types right-aligned
	// event.LeafType = uint8(logData[31])                                    // uint8 at offset 0-31, but uint8 is only 1 byte (right-aligned in 32 bytes)
	// event.OriginNetwork = binary.BigEndian.Uint32(logData[60:64])          // uint32 at offset 32-63, but uint32 is only 4 bytes (right-aligned in 32 bytes)
	event.OriginAddress = common.BytesToAddress(logData[76:96]) // address at offset 64-95, but address is only 20 bytes (right-aligned in 32 bytes)
	// event.DestinationNetwork = binary.BigEndian.Uint32(logData[124:128])   // uint32 at offset 96-127, but uint32 is only 4 bytes (right-aligned in 32 bytes)
	// event.DestinationAddress = common.BytesToAddress(logData[140:160])     // address at offset 128-159, but address is only 20 bytes (right-aligned in 32 bytes)
	event.Amount = new(big.Int).SetBytes(logData[160:192]) // uint256 at offset 160-191, full 32 bytes used
	// event.DepositCount = uint32(binary.BigEndian.Uint16(logData[254:256])) // uint32 at offset 224-255, but only 2 bytes used in this case (right-aligned)

	// Parse dynamic metadata using offset (bytes type is dynamic in ABI) - COMMENTED OUT
	// metadataOffset := new(big.Int).SetBytes(logData[192:224]).Int64() // bytes offset at offset 192-223, full 32 bytes used
	// if metadataOffset > 0 && int(metadataOffset)+32 <= len(logData) {
	//     // Read length of metadata from the offset location
	//     metadataLength := new(big.Int).SetBytes(logData[metadataOffset : metadataOffset+32]).Int64()
	//     if int(metadataOffset)+32+int(metadataLength) <= len(logData) {
	//         // Extract actual metadata content after the length field
	//         event.Metadata = logData[metadataOffset+32 : metadataOffset+32+metadataLength]
	//     }
	// }

	return event, nil
}

// validateBridgeEvent validates if BridgeEvent should be intercepted based on token, amount, and whitelist
func validateBridgeEvent(event *BridgeEventData, txSender common.Address, config *ethconfig.BridgeInterceptConfig, targetTokenAddress common.Address) error {
	// Only validate events for the target token
	if event.OriginAddress != targetTokenAddress {
		return nil // Not target token, allow pass
	}

	// Check if bridge amount exceeds configured limit
	if event.Amount.Cmp(config.MaxBridgeAmount) > 0 {
		return fmt.Errorf("bridge amount %s exceeds maximum %s for token %s",
			event.Amount.String(), config.MaxBridgeAmount.String(), targetTokenAddress.Hex())
	}

	// Check sender whitelist if enabled
	if config.WhitelistEnabled {
		if !isAddressInWhitelist(txSender, config.WhitelistAddresses) {
			return fmt.Errorf("sender %s not in whitelist for token %s",
				txSender.Hex(), targetTokenAddress.Hex())
		}
	}

	return nil // All validations passed
}

// isAddressInWhitelist checks if address is in whitelist (helper function)
func isAddressInWhitelist(address common.Address, whitelist []common.Address) bool {
	for _, whitelistAddr := range whitelist {
		if address == whitelistAddr {
			return true
		}
	}
	return false
}
