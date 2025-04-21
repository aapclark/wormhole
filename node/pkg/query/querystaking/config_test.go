package querystaking

import (
	"testing"
)

// TestQueryTypeBits tests the bitset encoding for query types
func TestQueryTypeBits(t *testing.T) {
	tests := []struct {
		name       string
		queryTypes []QueryType
		want       [32]byte
	}{
		{
			name:       "single QueryType 1",
			queryTypes: []QueryType{1},
			want:       bytes32WithValue(31, 0x01), // bit 0 set
		},
		{
			name:       "single QueryType 2",
			queryTypes: []QueryType{2},
			want:       bytes32WithValue(31, 0x02), // bit 1 set
		},
		{
			name:       "single QueryType 3",
			queryTypes: []QueryType{3},
			want:       bytes32WithValue(31, 0x04), // bit 2 set
		},
		{
			name:       "single QueryType 4",
			queryTypes: []QueryType{4},
			want:       bytes32WithValue(31, 0x08), // bit 3 set
		},
		{
			name:       "single QueryType 5",
			queryTypes: []QueryType{5},
			want:       bytes32WithValue(31, 0x10), // bit 4 set
		},
		{
			name:       "EVM pool (types 1,2,3)",
			queryTypes: []QueryType{1, 2, 3},
			want:       bytes32WithValue(31, 0x07), // bits 0,1,2 set = 0x01 | 0x02 | 0x04 = 0x07
		},
		{
			name:       "Solana pool (types 4,5)",
			queryTypes: []QueryType{4, 5},
			want:       bytes32WithValue(31, 0x18), // bits 3,4 set = 0x08 | 0x10 = 0x18
		},
		{
			name:       "all five types",
			queryTypes: []QueryType{1, 2, 3, 4, 5},
			want:       bytes32WithValue(31, 0x1F), // bits 0-4 set = 0x1F
		},
		{
			name:       "zero QueryType is skipped",
			queryTypes: []QueryType{0, 1},
			want:       bytes32WithValue(31, 0x01), // only bit 0 set, 0 is skipped
		},
		// Note: QueryType is uint8, so values > 255 cannot be represented
		// The code checks for > 255 but it's unreachable due to type constraints
		{
			name:       "empty QueryTypes",
			queryTypes: []QueryType{},
			want:       [32]byte{}, // all zeros
		},
		{
			name:       "QueryType 8 (second byte)",
			queryTypes: []QueryType{8},
			want:       bytes32WithValue(31, 0x80), // bit 7 set
		},
		{
			name:       "QueryType 9 (crosses byte boundary)",
			queryTypes: []QueryType{9},
			want:       bytes32WithValue(30, 0x01), // bit 8 set (byte 30, bit 0)
		},
		{
			name:       "high QueryType 255",
			queryTypes: []QueryType{255},
			want:       bytes32WithValue(0, 0x40), // QueryType 255: bit 254 (255-1), byte index 31-31=0, bit offset 6
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := QueryTypePool{QueryTypes: tt.queryTypes}
			got := pool.QueryTypeBits()

			if got != tt.want {
				t.Errorf("QueryTypeBits() = %x, want %x", got, tt.want)
				// Print detailed diff for debugging
				for i := 0; i < 32; i++ {
					if got[i] != tt.want[i] {
						t.Errorf("  byte[%d]: got %02x, want %02x", i, got[i], tt.want[i])
					}
				}
			}
		})
	}
}

// TestQueryTypeBits_NoCollisions tests that different query type combinations produce different bitsets
func TestQueryTypeBits_NoCollisions(t *testing.T) {
	tests := []struct {
		name   string
		types1 []QueryType
		types2 []QueryType
	}{
		{
			name:   "single vs multiple",
			types1: []QueryType{7},
			types2: []QueryType{1, 2, 4},
		},
		{
			name:   "different singles",
			types1: []QueryType{1},
			types2: []QueryType{2},
		},
		{
			name:   "different multiples",
			types1: []QueryType{1, 2},
			types2: []QueryType{3, 4},
		},
		{
			name:   "subset",
			types1: []QueryType{1, 2, 3},
			types2: []QueryType{1, 2},
		},
		{
			name:   "overlapping but different",
			types1: []QueryType{1, 2, 3},
			types2: []QueryType{2, 3, 4},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool1 := QueryTypePool{QueryTypes: tt.types1}
			pool2 := QueryTypePool{QueryTypes: tt.types2}

			bits1 := pool1.QueryTypeBits()
			bits2 := pool2.QueryTypeBits()

			if bits1 == bits2 {
				t.Errorf("Collision detected: %v and %v both produce %x", tt.types1, tt.types2, bits1)
			}
		})
	}
}

// TestQueryTypeBits_BitPositions tests correct bit position calculation
func TestQueryTypeBits_BitPositions(t *testing.T) {
	tests := []struct {
		queryType QueryType
		wantByte  int
		wantBit   uint
	}{
		{queryType: 1, wantByte: 31, wantBit: 0},
		{queryType: 2, wantByte: 31, wantBit: 1},
		{queryType: 8, wantByte: 31, wantBit: 7},
		{queryType: 9, wantByte: 30, wantBit: 0},
		{queryType: 16, wantByte: 30, wantBit: 7},
		{queryType: 17, wantByte: 29, wantBit: 0},
		{queryType: 255, wantByte: 0, wantBit: 6}, // (255-1)/8 = 31.75, byte 31-31=0, bit (255-1)%8 = 6
	}

	for _, tt := range tests {
		t.Run(string(rune(tt.queryType)), func(t *testing.T) {
			pool := QueryTypePool{QueryTypes: []QueryType{tt.queryType}}
			bits := pool.QueryTypeBits()

			// Check that the expected bit is set
			expectedBit := uint(1 << tt.wantBit)
			if bits[tt.wantByte]&byte(expectedBit) == 0 {
				t.Errorf("QueryType %d: bit %d in byte %d not set. Got byte value: %08b", tt.queryType, tt.wantBit, tt.wantByte, bits[tt.wantByte])
			}

			// Check that only one byte is non-zero (for single QueryType)
			nonZeroCount := 0
			for _, b := range bits {
				if b != 0 {
					nonZeroCount++
				}
			}
			if nonZeroCount != 1 {
				t.Errorf("QueryType %d: expected 1 non-zero byte, got %d", tt.queryType, nonZeroCount)
			}
		})
	}
}

// TestSupportedQueryPools tests that the configured pools have valid bitsets
func TestSupportedQueryPools(t *testing.T) {
	// Test that EVM pool is configured correctly
	t.Run("EVM pool", func(t *testing.T) {
		pool, exists := SupportedQueryPools["evm"]
		if !exists {
			t.Fatal("EVM pool not found in SupportedQueryPools")
		}

		if len(pool.QueryTypes) == 0 {
			t.Error("EVM pool has no query types")
		}

		bits := pool.QueryTypeBits()
		// Verify it's not all zeros
		allZero := true
		for _, b := range bits {
			if b != 0 {
				allZero = false
				break
			}
		}
		if allZero {
			t.Error("EVM pool bitset is all zeros")
		}
	})

	// Test that Solana pool is configured correctly
	t.Run("Solana pool", func(t *testing.T) {
		pool, exists := SupportedQueryPools["solana"]
		if !exists {
			t.Fatal("Solana pool not found in SupportedQueryPools")
		}

		if len(pool.QueryTypes) == 0 {
			t.Error("Solana pool has no query types")
		}

		bits := pool.QueryTypeBits()
		// Verify it's not all zeros
		allZero := true
		for _, b := range bits {
			if b != 0 {
				allZero = false
				break
			}
		}
		if allZero {
			t.Error("Solana pool bitset is all zeros")
		}
	})

	// Test that EVM and Solana pools have different bitsets
	t.Run("pools have different bitsets", func(t *testing.T) {
		evmPool := SupportedQueryPools["evm"]
		solanaPool := SupportedQueryPools["solana"]

		evmBits := evmPool.QueryTypeBits()
		solanaBits := solanaPool.QueryTypeBits()

		if evmBits == solanaBits {
			t.Error("EVM and Solana pools have identical bitsets (collision)")
		}
	})
}

// Helper function to create a [32]byte with a single byte set
func bytes32WithValue(index int, value byte) [32]byte {
	var result [32]byte
	result[index] = value
	return result
}

// Helper function to create a [32]byte with high bits set
func bytes32WithHighBits(index int, value byte) [32]byte {
	var result [32]byte
	result[index] = value
	return result
}
