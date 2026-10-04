package deepseek

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"sync"
)

// DeepSeekHashV1 is the proof-of-work hash used by chat.deepseek.com.
// It is a reduced Keccak-f[1600] sponge: rate 136 bytes, SHA-3 padding
// (0x06 ... 0x80), exactly 23 rounds using the standard round constants
// RC[1..23] (RC[0] is skipped), output = first 32 state bytes.
//
// Verified against live challenges captured from the web client; see
// docs/api-analysis.md.
func deepSeekHashV1(msg []byte) [32]byte {
	var st [25]uint64
	absorb(&st, msg)
	var out [32]byte
	for i := 0; i < 4; i++ {
		binary.LittleEndian.PutUint64(out[i*8:], st[i])
	}
	return out
}

func absorb(st *[25]uint64, msg []byte) {
	const rate = 136
	for len(msg) >= rate {
		for i := 0; i < rate/8; i++ {
			st[i] ^= leU64(msg[i*8:])
		}
		keccakF23(st)
		msg = msg[rate:]
	}
	var blk [rate]byte
	n := copy(blk[:], msg)
	blk[n] = 0x06
	blk[rate-1] |= 0x80
	for i := 0; i < rate/8; i++ {
		st[i] ^= leU64(blk[i*8:])
	}
	keccakF23(st)
}

func leU64(b []byte) uint64 {
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}

var keccakRC = [24]uint64{
	0x0000000000000001, 0x0000000000008082, 0x800000000000808A, 0x8000000080008000,
	0x000000000000808B, 0x0000000080000001, 0x8000000080008081, 0x8000000000008009,
	0x000000000000008A, 0x0000000000000088, 0x0000000080008009, 0x000000008000000A,
	0x000000008000808B, 0x800000000000008B, 0x8000000000008089, 0x8000000000008003,
	0x8000000000008002, 0x8000000000000080, 0x000000000000800A, 0x800000008000000A,
	0x8000000080008081, 0x8000000000008080, 0x0000000080000001, 0x8000000080008008,
}

var keccakRotc = [24]int{1, 3, 6, 10, 15, 21, 28, 36, 45, 55, 2, 14, 27, 41, 56, 8, 25, 43, 62, 18, 39, 61, 20, 44}
var keccakPiln = [24]int{10, 7, 11, 17, 18, 3, 5, 16, 8, 21, 24, 4, 15, 23, 19, 13, 12, 2, 20, 14, 22, 9, 6, 1}

func rotl64(x uint64, n int) uint64 {
	n &= 63
	if n == 0 {
		return x
	}
	return x<<uint(n) | x>>uint(64-n)
}

func keccakF23(a *[25]uint64) {
	var bc [5]uint64
	for round := 1; round <= 23; round++ {
		// theta
		for i := 0; i < 5; i++ {
			bc[i] = a[i] ^ a[i+5] ^ a[i+10] ^ a[i+15] ^ a[i+20]
		}
		for i := 0; i < 5; i++ {
			t := bc[(i+4)%5] ^ rotl64(bc[(i+1)%5], 1)
			for j := 0; j < 25; j += 5 {
				a[j+i] ^= t
			}
		}
		// rho and pi
		t := a[1]
		for i := 0; i < 24; i++ {
			j := keccakPiln[i]
			tmp := a[j]
			a[j] = rotl64(t, keccakRotc[i])
			t = tmp
		}
		// chi
		for j := 0; j < 25; j += 5 {
			for i := 0; i < 5; i++ {
				bc[i] = a[j+i]
			}
			for i := 0; i < 5; i++ {
				a[j+i] = bc[i] ^ (^bc[(i+1)%5] & bc[(i+2)%5])
			}
		}
		// iota
		a[0] ^= keccakRC[round]
	}
}

// PowChallenge mirrors data.biz_data.challenge of /chat/create_pow_challenge.
type PowChallenge struct {
	Algorithm   string `json:"algorithm"`
	Challenge   string `json:"challenge"`
	Salt        string `json:"salt"`
	Signature   string `json:"signature"`
	Difficulty  int64  `json:"difficulty"`
	ExpireAt    int64  `json:"expire_at"`
	ExpireAfter int64  `json:"expire_after"`
	TargetPath  string `json:"target_path"`
}

// Solve finds a nonce whose DeepSeekHashV1 hash equals the challenge.
// Any valid nonce is accepted by the server, so workers scan interleaved
// ranges and the first hit wins. Expected work is ~difficulty hashes.
func (c *PowChallenge) Solve() (uint64, error) {
	if c.Algorithm != "" && c.Algorithm != "DeepSeekHashV1" {
		return 0, fmt.Errorf("unsupported pow algorithm: %s", c.Algorithm)
	}
	target, err := hexDecodeString(c.Challenge)
	if err != nil {
		return 0, fmt.Errorf("bad challenge hex: %w", err)
	}
	var prefix [64]byte
	n := copy(prefix[:], c.Salt+"_"+strconv.FormatInt(c.ExpireAt, 10)+"_")
	if n >= len(prefix) {
		return 0, errors.New("pow: challenge prefix too long")
	}
	pb := prefix[:n]

	workers := runtime.NumCPU()
	if workers > 8 {
		workers = 8
	}
	if workers < 1 {
		workers = 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	found := make(chan uint64, 1)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(seed uint64) {
			defer wg.Done()
			msg := make([]byte, 0, 80)
			for nonce := uint64(w); ; nonce += uint64(workers) {
				select {
				case <-ctx.Done():
					return
				default:
				}
				msg = append(msg[:0], pb...)
				msg = strconv.AppendUint(msg, nonce, 10)
				h := deepSeekHashV1(msg)
				if bytes.Equal(h[:], target) {
					select {
					case found <- nonce:
					default:
					}
					cancel()
					return
				}
			}
		}(uint64(w))
	}
	wg.Wait()
	select {
	case nonce := <-found:
		return nonce, nil
	default:
		return 0, errors.New("pow: no answer found")
	}
}

func hexDecodeString(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, errors.New("odd-length hex string")
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		hi, ok1 := hexVal(s[i*2])
		lo, ok2 := hexVal(s[i*2+1])
		if !ok1 || !ok2 {
			return nil, errors.New("invalid hex character")
		}
		out[i] = hi<<4 | lo
	}
	return out, nil
}

func hexVal(c byte) (byte, bool) {
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
