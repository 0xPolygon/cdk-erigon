// zk/utils/trace_logger.go
package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	log "github.com/ledgerwatch/erigon/zkevm/log"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	traceLogEnabled bool
	traceLogger     *zap.SugaredLogger
)

// Write a trace log line
func writeTraceLogInternal(v ...interface{}) {
	format := "%s,%s,%s,%s,%s,%s,%d,%d,%s,%s,%s,%d,%s,%s,%d,%s,%d,%s,%s,%s,%s,%d"
	message := fmt.Sprintf(format, v...)
	traceLogger.Info(message)
}

// Public logging function
func LogTrace(
	txhash string,
	serviceName string,
	processId uint64,
	processWord string,
	blockHeight uint64,
	blockHash string,
	blockTime uint64,
	transactionType int8,
) {
	if !traceLogEnabled || traceLogger == nil {
		return
	}
	allArgs := []interface{}{
		Chain,
		txhash,
		Status,
		serviceName,
		Business,
		Client,
		ChainID,
		processId,
		processWord,
		Index,
		InnerIndex,
		time.Now().UnixMilli(),
		ReferId,
		ContractAddress,
		blockHeight,
		blockHash,
		blockTime,
		DepositConfirmHeight,
		TokenID,
		MevSupplier,
		BusinessHash,
		transactionType,
	}
	writeTraceLogInternal(allArgs...)
}

func SetTraceLogConfig(enabled bool, path string) {
	if !enabled {
		traceLogEnabled = false
		log.Info("Trace logging is disabled.")
		return
	}

	if path == "" {
		traceLogEnabled = false
		log.Warn("Trace logging enabled in config, but no path provided. Logger will not be initialized.")
		return
	}

	logDir := filepath.Dir(path)
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		if mkErr := os.MkdirAll(logDir, 0755); mkErr != nil {
			log.Errorf("Failed to create trace log directory %s: %v. Trace logging will be off.", logDir, mkErr)
			traceLogEnabled = false
			return
		}
	}

	config := zap.NewProductionConfig()
	config.OutputPaths = []string{path}
	config.Encoding = "console"

	config.EncoderConfig = zapcore.EncoderConfig{
		MessageKey:    "msg",
		LevelKey:      "",
		TimeKey:       "",
		NameKey:       "",
		CallerKey:     "",
		FunctionKey:   "",
		StacktraceKey: "",
		LineEnding:    zapcore.DefaultLineEnding,
		EncodeLevel: func(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
		},
		EncodeTime: func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		},
		EncodeDuration: func(d time.Duration, enc zapcore.PrimitiveArrayEncoder) {
		},
		EncodeCaller: func(caller zapcore.EntryCaller, enc zapcore.PrimitiveArrayEncoder) {
		},
		EncodeName: func(loggerName string, enc zapcore.PrimitiveArrayEncoder) {
		},
	}

	logger, err := config.Build(zap.AddCallerSkip(1))
	if err != nil {
		log.Errorf("Failed to create trace logger: %v. Trace logging will be off.", err)
		traceLogEnabled = false
		return
	}

	traceLogger = logger.Sugar()
	traceLogEnabled = true
	log.Infof("Trace logging enabled. Path set to: %s", path)
}
