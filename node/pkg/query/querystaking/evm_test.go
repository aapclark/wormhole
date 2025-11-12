package querystaking

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// TestParseStakeInfo tests parsing of stake info from contract data
func TestParseStakeInfo(t *testing.T) {
	tests := []struct {
		name      string
		input     []byte
		wantError bool
		validate  func(*testing.T, *StakeInfo)
	}{
		{
			name: "valid stake info with all fields",
			input: buildStakeInfoBytes(
				1000,  // amount
				2,     // conversionTableIndex
				10000, // lockupEnd
				20000, // accessEnd
				5000,  // lastClaimed
				1000,  // capacity
			),
			wantError: false,
			validate: func(t *testing.T, si *StakeInfo) {
				if si.Amount.Uint64() != 1000 {
					t.Errorf("Amount = %d, want 1000", si.Amount.Uint64())
				}
				if si.ConversionTableIndex.Uint64() != 2 {
					t.Errorf("ConversionTableIndex = %d, want 2", si.ConversionTableIndex.Uint64())
				}
				if si.LockupEnd != 10000 {
					t.Errorf("LockupEnd = %d, want 10000", si.LockupEnd)
				}
				if si.AccessEnd != 20000 {
					t.Errorf("AccessEnd = %d, want 20000", si.AccessEnd)
				}
				if si.LastClaimed != 5000 {
					t.Errorf("LastClaimed = %d, want 5000", si.LastClaimed)
				}
				if si.Capacity.Uint64() != 1000 {
					t.Errorf("Capacity = %d, want 1000", si.Capacity.Uint64())
				}
			},
		},
		{
			name: "valid stake info with zero values",
			input: buildStakeInfoBytes(
				0, // amount
				0, // conversionTableIndex
				0, // lockupEnd
				0, // accessEnd
				0, // lastClaimed
				0, // capacity
			),
			wantError: false,
			validate: func(t *testing.T, si *StakeInfo) {
				if si.Amount.Uint64() != 0 {
					t.Errorf("Amount = %d, want 0", si.Amount.Uint64())
				}
				if si.ConversionTableIndex.Uint64() != 0 {
					t.Errorf("ConversionTableIndex = %d, want 0", si.ConversionTableIndex.Uint64())
				}
			},
		},
		{
			name: "valid stake info with max uint48 values",
			input: buildStakeInfoBytes(
				999999999,
				100,
				281474976710655,
				281474976710655,
				281474976710655,
				999999999,
			),
			wantError: false,
			validate: func(t *testing.T, si *StakeInfo) {
				if si.LockupEnd != 281474976710655 {
					t.Errorf("LockupEnd = %d, want max uint48", si.LockupEnd)
				}
			},
		},
		{
			name:      "invalid length - too short",
			input:     make([]byte, 191),
			wantError: true,
		},
		{
			name:      "invalid length - too long",
			input:     make([]byte, 193),
			wantError: true,
		},
		{
			name:      "empty input",
			input:     []byte{},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseStakeInfo(tt.input)

			if tt.wantError {
				if err == nil {
					t.Errorf("ParseStakeInfo() error = nil, wantError = true")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseStakeInfo() unexpected error = %v", err)
				return
			}

			if tt.validate != nil {
				tt.validate(t, got)
			}
		})
	}
}

// TestParseConversionTranches tests the conversion table parsing logic
func TestParseConversionTranches(t *testing.T) {
	tests := []struct {
		name      string
		input     [32]byte
		want      []ConversionTranche
		wantError bool
	}{
		{
			name:  "valid single tranche",
			input: toBytes32("rate:100,tranche:1000"),
			want: []ConversionTranche{
				{Rate: 100, Tranche: 1000},
			},
			wantError: false,
		},
		// Note: Multiple tranches in 32 bytes is challenging
		// "rate:10,tranche:5000,rate:100" = 31 chars, leaves only 1 char for second tranche value
		// In practice, conversion tables would be stored more efficiently or use multiple entries
		{
			name:  "valid with whitespace",
			input: toBytes32("rate: 100 , tranche: 1000 "),
			want: []ConversionTranche{
				{Rate: 100, Tranche: 1000},
			},
			wantError: false,
		},
		{
			name:      "empty entry",
			input:     [32]byte{},
			want:      nil,
			wantError: true,
		},
		{
			name:      "odd number of parts",
			input:     toBytes32("rate:100,tranche:1000,rate:200"),
			want:      nil,
			wantError: true,
		},
		{
			name:      "wrong key name - rate",
			input:     toBytes32("price:100,tranche:1000"),
			want:      nil,
			wantError: true,
		},
		{
			name:      "wrong key name - tranche",
			input:     toBytes32("rate:100,tier:1000"),
			want:      nil,
			wantError: true,
		},
		{
			name:      "missing colon",
			input:     toBytes32("rate100,tranche:1000"),
			want:      nil,
			wantError: true,
		},
		{
			name:      "non-numeric rate",
			input:     toBytes32("rate:abc,tranche:1000"),
			want:      nil,
			wantError: true,
		},
		{
			name:      "non-numeric tranche",
			input:     toBytes32("rate:100,tranche:xyz"),
			want:      nil,
			wantError: true,
		},
		{
			name:  "large values (truncated to fit 32 bytes)",
			input: toBytes32("rate:999999,tranche:999999"),
			want: []ConversionTranche{
				{Rate: 999999, Tranche: 999999},
			},
			wantError: false,
		},
		{
			name:  "zero values",
			input: toBytes32("rate:0,tranche:0"),
			want: []ConversionTranche{
				{Rate: 0, Tranche: 0},
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseConversionTranches(tt.input)

			if tt.wantError {
				if err == nil {
					t.Errorf("ParseConversionTranches() error = nil, wantError = true")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseConversionTranches() unexpected error = %v", err)
				return
			}

			if len(got) != len(tt.want) {
				t.Errorf("ParseConversionTranches() got %d tranches, want %d", len(got), len(tt.want))
				return
			}

			for i := range got {
				if got[i].Rate != tt.want[i].Rate {
					t.Errorf("ParseConversionTranches() tranche[%d].Rate = %d, want %d", i, got[i].Rate, tt.want[i].Rate)
				}
				if got[i].Tranche != tt.want[i].Tranche {
					t.Errorf("ParseConversionTranches() tranche[%d].Tranche = %d, want %d", i, got[i].Tranche, tt.want[i].Tranche)
				}
			}
		})
	}
}

// TestParseConversionTableEntry tests the bytes32 parsing
func TestParseConversionTableEntry(t *testing.T) {
	tests := []struct {
		name      string
		input     []byte
		wantError bool
	}{
		{
			name:      "valid 32 bytes",
			input:     make([]byte, 32),
			wantError: false,
		},
		{
			name:      "invalid length - too short",
			input:     make([]byte, 31),
			wantError: true,
		},
		{
			name:      "invalid length - too long",
			input:     make([]byte, 33),
			wantError: true,
		},
		{
			name:      "empty input",
			input:     []byte{},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseConversionTableEntry(tt.input)

			if tt.wantError {
				if err == nil {
					t.Errorf("ParseConversionTableEntry() error = nil, wantError = true")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseConversionTableEntry() unexpected error = %v", err)
				return
			}

			if len(got) != 32 {
				t.Errorf("ParseConversionTableEntry() returned array of length %d, want 32", len(got))
			}
		})
	}
}

// TestParseSignerAddress tests address parsing from contract data
func TestParseSignerAddress(t *testing.T) {
	tests := []struct {
		name      string
		input     []byte
		want      common.Address
		wantError bool
	}{
		{
			name: "valid address",
			input: append(
				make([]byte, 12), // 12 zero bytes padding
				[]byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 0x12, 0x34, 0x56, 0x78}...,
			),
			want:      common.HexToAddress("0x123456789abcdef0123456789abcdef012345678"),
			wantError: false,
		},
		{
			name:      "zero address",
			input:     make([]byte, 32),
			want:      common.Address{},
			wantError: false,
		},
		{
			name:      "invalid length - too short",
			input:     make([]byte, 31),
			want:      common.Address{},
			wantError: true,
		},
		{
			name:      "invalid length - too long",
			input:     make([]byte, 33),
			want:      common.Address{},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSignerAddress(tt.input)

			if tt.wantError {
				if err == nil {
					t.Errorf("ParseSignerAddress() error = nil, wantError = true")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseSignerAddress() unexpected error = %v", err)
				return
			}

			if got != tt.want {
				t.Errorf("ParseSignerAddress() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestParseBoolResult tests boolean parsing from contract data
func TestParseBoolResult(t *testing.T) {
	tests := []struct {
		name      string
		input     []byte
		want      bool
		wantError bool
	}{
		{
			name:      "true - last byte is 1",
			input:     append(make([]byte, 31), 0x01),
			want:      true,
			wantError: false,
		},
		{
			name:      "false - last byte is 0",
			input:     make([]byte, 32),
			want:      false,
			wantError: false,
		},
		{
			name:      "true - last byte is non-zero",
			input:     append(make([]byte, 31), 0xff),
			want:      true,
			wantError: false,
		},
		{
			name:      "invalid length - too short",
			input:     make([]byte, 31),
			want:      false,
			wantError: true,
		},
		{
			name:      "invalid length - too long",
			input:     make([]byte, 33),
			want:      false,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseBoolResult(tt.input)

			if tt.wantError {
				if err == nil {
					t.Errorf("ParseBoolResult() error = nil, wantError = true")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseBoolResult() unexpected error = %v", err)
				return
			}

			if got != tt.want {
				t.Errorf("ParseBoolResult() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestStakeInfoMethods tests StakeInfo helper methods
func TestStakeInfoMethods(t *testing.T) {
	t.Run("HasStake", func(t *testing.T) {
		tests := []struct {
			name  string
			stake *StakeInfo
			want  bool
		}{
			{
				name:  "nil amount",
				stake: &StakeInfo{Amount: nil},
				want:  false,
			},
			{
				name:  "zero amount",
				stake: &StakeInfo{Amount: uint256.NewInt(0)},
				want:  false,
			},
			{
				name:  "non-zero amount",
				stake: &StakeInfo{Amount: uint256.NewInt(1000)},
				want:  true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if got := tt.stake.HasStake(); got != tt.want {
					t.Errorf("HasStake() = %v, want %v", got, tt.want)
				}
			})
		}
	})

	t.Run("IsInLockup", func(t *testing.T) {
		tests := []struct {
			name      string
			lockupEnd uint64
			timestamp uint64
			want      bool
		}{
			{
				name:      "before lockup end",
				lockupEnd: 1000,
				timestamp: 500,
				want:      true,
			},
			{
				name:      "at lockup end",
				lockupEnd: 1000,
				timestamp: 1000,
				want:      false,
			},
			{
				name:      "after lockup end",
				lockupEnd: 1000,
				timestamp: 1500,
				want:      false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				stake := &StakeInfo{LockupEnd: tt.lockupEnd}
				if got := stake.IsInLockup(tt.timestamp); got != tt.want {
					t.Errorf("IsInLockup() = %v, want %v", got, tt.want)
				}
			})
		}
	})

	t.Run("IsInAccessPeriod", func(t *testing.T) {
		tests := []struct {
			name      string
			lockupEnd uint64
			accessEnd uint64
			timestamp uint64
			want      bool
		}{
			{
				name:      "before lockup end",
				lockupEnd: 1000,
				accessEnd: 2000,
				timestamp: 500,
				want:      false,
			},
			{
				name:      "at lockup end",
				lockupEnd: 1000,
				accessEnd: 2000,
				timestamp: 1000,
				want:      true,
			},
			{
				name:      "during access period",
				lockupEnd: 1000,
				accessEnd: 2000,
				timestamp: 1500,
				want:      true,
			},
			{
				name:      "at access end",
				lockupEnd: 1000,
				accessEnd: 2000,
				timestamp: 2000,
				want:      false,
			},
			{
				name:      "after access end",
				lockupEnd: 1000,
				accessEnd: 2000,
				timestamp: 2500,
				want:      false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				stake := &StakeInfo{
					LockupEnd: tt.lockupEnd,
					AccessEnd: tt.accessEnd,
				}
				if got := stake.IsInAccessPeriod(tt.timestamp); got != tt.want {
					t.Errorf("IsInAccessPeriod() = %v, want %v", got, tt.want)
				}
			})
		}
	})

	t.Run("HasExpired", func(t *testing.T) {
		tests := []struct {
			name      string
			accessEnd uint64
			timestamp uint64
			want      bool
		}{
			{
				name:      "before access end",
				accessEnd: 2000,
				timestamp: 1500,
				want:      false,
			},
			{
				name:      "at access end",
				accessEnd: 2000,
				timestamp: 2000,
				want:      true,
			},
			{
				name:      "after access end",
				accessEnd: 2000,
				timestamp: 2500,
				want:      true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				stake := &StakeInfo{AccessEnd: tt.accessEnd}
				if got := stake.HasExpired(tt.timestamp); got != tt.want {
					t.Errorf("HasExpired() = %v, want %v", got, tt.want)
				}
			})
		}
	})
}

// TestPackStakesCall tests ABI encoding for stakes(address) call
func TestPackStakesCall(t *testing.T) {
	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	result := PackStakesCall(addr)

	// Verify length: 4 bytes selector + 32 bytes address
	if len(result) != 36 {
		t.Errorf("PackStakesCall() length = %d, want 36", len(result))
	}

	// Verify function selector (first 4 bytes)
	// stakes(address) = keccak256("stakes(address)")[:4]
	expectedSelector := []byte{0x16, 0x93, 0x4f, 0xc4}
	if !bytesEqual(result[:4], expectedSelector) {
		t.Errorf("PackStakesCall() selector = %x, want %x", result[:4], expectedSelector)
	}

	// Verify address is properly padded (12 zero bytes + 20 address bytes)
	// Address should start at byte 16 (4 selector + 12 padding)
	if !bytesEqual(result[16:36], addr.Bytes()) {
		t.Errorf("PackStakesCall() address = %x, want %x", result[16:36], addr.Bytes())
	}

	// Verify padding is zeros
	for i := 4; i < 16; i++ {
		if result[i] != 0 {
			t.Errorf("PackStakesCall() byte[%d] = %x, want 0 (padding)", i, result[i])
		}
	}
}

// TestPackStakerSignersCall tests ABI encoding for stakerSigners(address) call
func TestPackStakerSignersCall(t *testing.T) {
	addr := common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")
	result := PackStakerSignersCall(addr)

	// Verify length
	if len(result) != 36 {
		t.Errorf("PackStakerSignersCall() length = %d, want 36", len(result))
	}

	// Verify function selector
	// stakerSigners(address) = keccak256("stakerSigners(address)")[:4]
	expectedSelector := []byte{0x9a, 0xe2, 0x56, 0xca}
	if !bytesEqual(result[:4], expectedSelector) {
		t.Errorf("PackStakerSignersCall() selector = %x, want %x", result[:4], expectedSelector)
	}

	// Verify address placement
	if !bytesEqual(result[16:36], addr.Bytes()) {
		t.Errorf("PackStakerSignersCall() address = %x, want %x", result[16:36], addr.Bytes())
	}
}

// TestPackIsBlocklistedCall tests ABI encoding for isBlocklisted(address) call
func TestPackIsBlocklistedCall(t *testing.T) {
	addr := common.HexToAddress("0x0000000000000000000000000000000000000001")
	result := PackIsBlocklistedCall(addr)

	// Verify length
	if len(result) != 36 {
		t.Errorf("PackIsBlocklistedCall() length = %d, want 36", len(result))
	}

	// Verify function selector
	// isBlocklisted(address) = keccak256("isBlocklisted(address)")[:4]
	expectedSelector := []byte{0x8e, 0x20, 0x4c, 0x43}
	if !bytesEqual(result[:4], expectedSelector) {
		t.Errorf("PackIsBlocklistedCall() selector = %x, want %x", result[:4], expectedSelector)
	}

	// Verify address placement (should be at bytes 35-36 for this short address)
	if result[35] != 0x01 {
		t.Errorf("PackIsBlocklistedCall() address last byte = %x, want 0x01", result[35])
	}
}

// TestPackQueryTypePoolsCall tests ABI encoding for queryTypePools(bytes32) call
func TestPackQueryTypePoolsCall(t *testing.T) {
	queryType := [32]byte{
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x07,
	} // EVM pool bits

	result := PackQueryTypePoolsCall(queryType)

	// Verify length: 4 bytes selector + 32 bytes queryType
	if len(result) != 36 {
		t.Errorf("PackQueryTypePoolsCall() length = %d, want 36", len(result))
	}

	// Verify function selector
	// queryTypePools(bytes32) = keccak256("queryTypePools(bytes32)")[:4]
	expectedSelector := []byte{0x4d, 0x76, 0xb7, 0x1d}
	if !bytesEqual(result[:4], expectedSelector) {
		t.Errorf("PackQueryTypePoolsCall() selector = %x, want %x", result[:4], expectedSelector)
	}

	// Verify bytes32 is directly appended
	if !bytesEqual(result[4:36], queryType[:]) {
		t.Errorf("PackQueryTypePoolsCall() queryType = %x, want %x", result[4:36], queryType[:])
	}

	// Verify last byte
	if result[35] != 0x07 {
		t.Errorf("PackQueryTypePoolsCall() last byte = %x, want 0x07", result[35])
	}
}

// TestPackConversionTableHistoryCall tests ABI encoding for conversionTableHistory(uint256) call
func TestPackConversionTableHistoryCall(t *testing.T) {
	tests := []struct {
		name  string
		index uint64
	}{
		{
			name:  "index 0",
			index: 0,
		},
		{
			name:  "index 1",
			index: 1,
		},
		{
			name:  "index 255",
			index: 255,
		},
		{
			name:  "large index",
			index: 999999,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			index := uint256.NewInt(tt.index)
			result := PackConversionTableHistoryCall(index)

			// Verify length: 4 bytes selector + 32 bytes uint256
			if len(result) != 36 {
				t.Errorf("PackConversionTableHistoryCall() length = %d, want 36", len(result))
			}

			// Verify function selector
			// conversionTableHistory(uint256) = keccak256("conversionTableHistory(uint256)")[:4]
			expectedSelector := []byte{0x4e, 0x80, 0x81, 0x2a}
			if !bytesEqual(result[:4], expectedSelector) {
				t.Errorf("PackConversionTableHistoryCall() selector = %x, want %x", result[:4], expectedSelector)
			}

			// Verify uint256 is properly encoded (big-endian, 32 bytes)
			// For small numbers, should be right-aligned with leading zeros
			indexBytes := index.Bytes32()
			if !bytesEqual(result[4:36], indexBytes[:]) {
				t.Errorf("PackConversionTableHistoryCall() index encoding = %x, want %x", result[4:36], indexBytes[:])
			}
		})
	}
}

// Helper function to convert string to [32]byte with null padding
func toBytes32(s string) [32]byte {
	var arr [32]byte
	copy(arr[:], s)
	return arr
}

// Helper function to build StakeInfo bytes for testing
func buildStakeInfoBytes(amount, conversionTableIndex, lockupEnd, accessEnd, lastClaimed, capacity uint64) []byte {
	result := make([]byte, 192)

	// amount (uint256) - bytes 0-31
	amountInt := uint256.NewInt(amount)
	amountBytes := amountInt.Bytes32()
	copy(result[0:32], amountBytes[:])

	// conversionTableIndex (uint256) - bytes 32-63
	indexInt := uint256.NewInt(conversionTableIndex)
	indexBytes := indexInt.Bytes32()
	copy(result[32:64], indexBytes[:])

	// lockupEnd (uint48) - bytes 64-69 (but stored in 32 bytes by contract)
	lockupBytes := uint256.NewInt(lockupEnd).Bytes32()
	copy(result[64:96], lockupBytes[:])

	// accessEnd (uint48) - bytes 96-101 (but stored in 32 bytes by contract)
	accessBytes := uint256.NewInt(accessEnd).Bytes32()
	copy(result[96:128], accessBytes[:])

	// lastClaimed (uint48) - bytes 128-133 (but stored in 32 bytes by contract)
	claimedBytes := uint256.NewInt(lastClaimed).Bytes32()
	copy(result[128:160], claimedBytes[:])

	// capacity (uint256) - bytes 160-191
	capacityInt := uint256.NewInt(capacity)
	capacityBytes := capacityInt.Bytes32()
	copy(result[160:192], capacityBytes[:])

	return result
}

// Helper function to compare byte slices
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestParseRateString tests rate string parsing
func TestParseRateString(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      uint64
		wantError bool
	}{
		{
			name:      "1 QPS",
			input:     "1 QPS",
			want:      60,
			wantError: false,
		},
		{
			name:      "10 QPS",
			input:     "10 QPS",
			want:      600,
			wantError: false,
		},
		{
			name:      "1 QPM",
			input:     "1 QPM",
			want:      1,
			wantError: false,
		},
		{
			name:      "100 QPM",
			input:     "100 QPM",
			want:      100,
			wantError: false,
		},
		{
			name:      "lowercase qps",
			input:     "5 qps",
			want:      300,
			wantError: false,
		},
		{
			name:      "lowercase qpm",
			input:     "50 qpm",
			want:      50,
			wantError: false,
		},
		{
			name:      "invalid format - no space",
			input:     "1QPS",
			wantError: true,
		},
		{
			name:      "invalid format - no unit",
			input:     "1",
			wantError: true,
		},
		{
			name:      "invalid unit",
			input:     "1 RPS",
			wantError: true,
		},
		{
			name:      "invalid number",
			input:     "abc QPS",
			wantError: true,
		},
		{
			name:      "negative number",
			input:     "-1 QPS",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRateString(tt.input)

			if tt.wantError {
				if err == nil {
					t.Errorf("parseRateString() error = nil, wantError = true")
				}
				return
			}

			if err != nil {
				t.Errorf("parseRateString() unexpected error = %v", err)
				return
			}

			if got != tt.want {
				t.Errorf("parseRateString() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestConversionTableGetTranchesByChain tests the GetTranchesByChain method
func TestConversionTableGetTranchesByChain(t *testing.T) {
	tests := []struct {
		name      string
		table     ConversionTable
		chainName string
		want      []ConversionTranche
		wantError bool
	}{
		{
			name: "EVM chain with single tranche",
			table: ConversionTable{
				EVM: map[string]string{
					"5000": "1 QPM",
				},
			},
			chainName: "EVM",
			want: []ConversionTranche{
				{Rate: 1, Tranche: 5000},
			},
			wantError: false,
		},
		{
			name: "EVM chain with multiple tranches",
			table: ConversionTable{
				EVM: map[string]string{
					"5000":   "1 QPM",
					"50000":  "1 QPS",
					"500000": "10 QPS",
				},
			},
			chainName: "EVM",
			want: []ConversionTranche{
				{Rate: 1, Tranche: 5000},
				{Rate: 60, Tranche: 50000},
				{Rate: 600, Tranche: 500000},
			},
			wantError: false,
		},
		{
			name: "Solana chain",
			table: ConversionTable{
				Solana: map[string]string{
					"12500":  "1 QPM",
					"125000": "1 QPS",
				},
			},
			chainName: "Solana",
			want: []ConversionTranche{
				{Rate: 1, Tranche: 12500},
				{Rate: 60, Tranche: 125000},
			},
			wantError: false,
		},
		{
			name: "unknown chain",
			table: ConversionTable{
				EVM: map[string]string{
					"5000": "1 QPM",
				},
			},
			chainName: "Bitcoin",
			wantError: true,
		},
		{
			name: "chain with no rates",
			table: ConversionTable{
				EVM: nil,
			},
			chainName: "EVM",
			wantError: true,
		},
		{
			name: "invalid tranche amount",
			table: ConversionTable{
				EVM: map[string]string{
					"abc": "1 QPM",
				},
			},
			chainName: "EVM",
			wantError: true,
		},
		{
			name: "invalid rate string",
			table: ConversionTable{
				EVM: map[string]string{
					"5000": "invalid",
				},
			},
			chainName: "EVM",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.table.GetTranchesByChain(tt.chainName)

			if tt.wantError {
				if err == nil {
					t.Errorf("GetTranchesByChain() error = nil, wantError = true")
				}
				return
			}

			if err != nil {
				t.Errorf("GetTranchesByChain() unexpected error = %v", err)
				return
			}

			if len(got) != len(tt.want) {
				t.Errorf("GetTranchesByChain() got %d tranches, want %d", len(got), len(tt.want))
				return
			}

			// Check that tranches are sorted by tranche amount
			for i := 1; i < len(got); i++ {
				if got[i].Tranche <= got[i-1].Tranche {
					t.Errorf("GetTranchesByChain() tranches not sorted: %v", got)
					break
				}
			}

			// Check each tranche
			for i := range got {
				if got[i].Rate != tt.want[i].Rate {
					t.Errorf("GetTranchesByChain() tranche[%d].Rate = %d, want %d", i, got[i].Rate, tt.want[i].Rate)
				}
				if got[i].Tranche != tt.want[i].Tranche {
					t.Errorf("GetTranchesByChain() tranche[%d].Tranche = %d, want %d", i, got[i].Tranche, tt.want[i].Tranche)
				}
			}
		})
	}
}
