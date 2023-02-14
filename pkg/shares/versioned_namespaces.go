package shares

import "fmt"

func examples() {
	currentBlobShare := []byte{
		1, 2, 3, 4, 5, 6, 7, 8, // namespace ID
		1,          // info byte
		0, 0, 0, 3, // sequence length
		1, 2, 3, // blob data
		// 0 padding until share is full
	}

	optionA0 := []byte{
		0,                      // namespace ID version
		1, 2, 3, 4, 5, 6, 7, 8, // namespace ID
		1,          // info byte
		0, 0, 0, 3, // sequence length
		1, 2, 3, // blob data
		// 0 padding until share is full
	}

	optionA1 := []byte{
		1,                                                                                                                     // namespace version
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, // namespace ID
		1,          // info byte
		0, 0, 0, 3, // sequence length
		1, 2, 3, // blob data
		// 0 padding until share is full
	}

	optionB := []byte{
		0,                                                     // namespace version
		16,                                                    // namespace length
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, // namespace ID
		1,          // info byte
		0, 0, 0, 3, // sequence length
		1, 2, 3, // blob data
		// 0 padding until share is full
	}

	optionC := []byte{
		0,                      // share version
		1, 2, 3, 4, 5, 6, 7, 8, // namespace ID
		1,          // sequence start indicator
		0, 0, 0, 3, // sequence length
		1, 2, 3, // blob data
		// 0 padding until share is full
	}

	fmt.Printf("shareA: %+v", currentBlobShare)
	fmt.Printf("optionA0: %+v", optionA0)
	fmt.Printf("optionA1: %+v", optionA1)
	fmt.Printf("optionB: %+v", optionB)
	fmt.Printf("optionC: %+v", optionC)
}
