package testnet

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/celestiaorg/knuu/pkg/knuu"
)

type JSONRPCError struct {
	Code    int
	Message string
	Data    string
}

func (e *JSONRPCError) Error() string {
	return fmt.Sprintf("JSONRPC Error - Code: %d, Message: %s, Data: %s", e.Code, e.Message, e.Data)
}

func getStatus(executor *knuu.Executor, app *knuu.Instance) (string, error) {
	nodeIP, err := app.GetIP()
	if err != nil {
		return "", fmt.Errorf("error getting node ip: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	status, err := executor.ExecuteCommandWithContext(ctx, "wget", "-q", "-O", "-", fmt.Sprintf("%s:26657/status", nodeIP))
	if err != nil {
		return "", fmt.Errorf("error executing command: %w", err)
	}
	return status, nil
}

func latestBlockHeightFromStatus(status string) (int64, error) {
	var result map[string]interface{}
	err := json.Unmarshal([]byte(status), &result)
	if err != nil {
		return 0, fmt.Errorf("error unmarshalling status: %w", err)
	}

	if errorField, ok := result["error"]; ok {
		errorData, ok := errorField.(map[string]interface{})
		if !ok {
			return 0, fmt.Errorf("error field exists but is not a map[string]interface{}")
		}
		jsonError := &JSONRPCError{}
		if errorCode, ok := errorData["code"].(float64); ok {
			jsonError.Code = int(errorCode)
		}
		if errorMessage, ok := errorData["message"].(string); ok {
			jsonError.Message = errorMessage
		}
		if errorData, ok := errorData["data"].(string); ok {
			jsonError.Data = errorData
		}
		return 0, jsonError
	}

	resultData, ok := result["result"].(map[string]interface{})
	if !ok {
		return 0, fmt.Errorf("error getting result from status")
	}
	syncInfo, ok := resultData["sync_info"].(map[string]interface{})
	if !ok {
		return 0, fmt.Errorf("error getting sync info from status")
	}
	latestBlockHeight, ok := syncInfo["latest_block_height"].(string)
	if !ok {
		return 0, fmt.Errorf("error getting latest block height from sync info")
	}
	latestBlockHeightInt, err := strconv.ParseInt(latestBlockHeight, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("error converting latest block height to int: %w", err)
	}
	return latestBlockHeightInt, nil
}
