package main

import (
	"encoding/csv"
	"os"
	"strconv"
	"time"

	"github.com/tendermint/tendermint/types"
)

func extractHeaders(blockchain []*types.Block) []BlockHeader {
	blockData := make([]BlockHeader, 0, len(blockchain))
	for _, block := range blockchain {
		blockData = append(blockData, BlockHeader{
			Time:   block.Header.Time,
			Size:   float64(block.Size()),
			Height: int(block.Height),
		})
	}
	return blockData
}

type BlockHeader struct {
	Time   time.Time
	Size   float64 // in bytes
	Height int
}

// SaveToCSV saves slice of BlockHeader to a CSV file at the given path.
func SaveToCSV(blockData []BlockHeader, filePath string) error {
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	header := []string{"Time", "Size", "Height"}
	if err := writer.Write(header); err != nil {
		return err
	}

	// iterates over the slice and write each element as a row in the CSV
	for _, data := range blockData {
		row := []string{
			// format the time using RFC3339 standard
			data.Time.Format(time.RFC3339),
			strconv.FormatFloat(data.Size, 'f', -1, 64),
			strconv.Itoa(data.Height),
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	}

	return nil
}
